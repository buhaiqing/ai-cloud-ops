// Package ingest — webhook.go: CloudMonitor EventSubscription webhook receiver (T4).
//
// POST /webhook/cms receives CMS alert pushes, verifies HMAC-SHA256 signature,
// then hands the parsed alert to a pluggable AlertInserter (production wires
// this to internal/db.InsertAlert). Idempotency is enforced inside the inserter
// via UNIQUE (alert_id, created_at) — see db/migrations/0001_init.sql.
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// HeaderSignature is the Aliyun CMS signature header.
const HeaderSignature = "X-Aliyun-Signature"

// EnvSigningSecret is the env var holding the shared HMAC secret.
const EnvSigningSecret = "WEBHOOK_SIGNING_SECRET"

// maxBodyBytes caps inbound webhook bodies (1 MiB) — alerts are small JSON.
const maxBodyBytes = 1 << 20

// Alert is the parsed CMS alert payload (subset of the schema in
// db/migrations/0001_init.sql — only fields the handler needs to extract).
type Alert struct {
	AlertID      string          `json:"alert_id"`
	AccountAlias string          `json:"account_alias,omitempty"`
	Region       string          `json:"region,omitempty"`
	Severity     string          `json:"severity,omitempty"`
	ResourceType string          `json:"resource_type,omitempty"`
	ResourceID   string          `json:"resource_id,omitempty"`
	Name         string          `json:"name,omitempty"` // some CMS payloads use alertName
	Tags         map[string]any  `json:"tags,omitempty"`
	Metric       map[string]any  `json:"metric,omitempty"`
	Payload      json.RawMessage `json:"-"` // full raw body for the JSONB column
	CreatedAt    time.Time       `json:"-"` // parsed from payload or set to now
}

// AlertInserter persists an Alert and returns its DB id.
// Production wires this to db.InsertAlert; tests pass a stub closure.
type AlertInserter func(ctx context.Context, alert Alert) (string, error)

// WebhookHandler returns an http.Handler for POST /webhook/cms.
// `secret` may be empty at construction; it is read from the env on each request
// so rotating WEBHOOK_SIGNING_SECRET doesn't require a restart.
func WebhookHandler(insert AlertInserter, logger *zap.Logger) http.Handler {
	if logger == nil {
		logger = zap.NewNop()
	}
	if insert == nil {
		insert = func(context.Context, Alert) (string, error) {
			return "", errors.New("alert inserter not configured")
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveWebhook(w, r, insert, logger)
	})
}

// Mount registers POST /webhook/cms on the given chi router.
func Mount(r chi.Router, insert AlertInserter, logger *zap.Logger) {
	r.Post("/webhook/cms", WebhookHandler(insert, logger).ServeHTTP)
}

func serveWebhook(w http.ResponseWriter, r *http.Request, insert AlertInserter, logger *zap.Logger) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read body", "detail": err.Error()})
		return
	}
	secret := os.Getenv(EnvSigningSecret)
	sig := r.Header.Get(HeaderSignature)
	if !VerifySignature(body, sig, secret) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid signature"})
		return
	}
	alert, err := parseAlert(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	id, err := insert(r.Context(), alert)
	if err != nil {
		logger.Error("webhook insert failed",
			zap.String("alert_id", alert.AlertID),
			zap.Error(err))
		writeJSON(w, http.StatusInternalServerError,
			map[string]any{"error": "persist failed; retry with backoff", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "persisted", "alert_id": id})
}

// parseAlert decodes the CMS payload and fills derived fields.
// CMS sometimes keys the alert under "alertName" instead of "alert_id" —
// accept either (matches Python webhook.py behavior).
func parseAlert(body []byte) (Alert, error) {
	var a Alert
	if err := json.Unmarshal(body, &a); err != nil {
		return a, errors.New("invalid json")
	}
	if a.AlertID == "" {
		// Fallback: try the legacy CMS key.
		var legacy struct {
			AlertName string `json:"alertName"`
		}
		_ = json.Unmarshal(body, &legacy) // best-effort, first unmarshal already populated fields
		a.AlertID = legacy.AlertName
	}
	if a.AlertID == "" {
		return a, errors.New("alert_id missing")
	}
	a.Payload = body
	if a.CreatedAt.IsZero() {
		if ts := extractCreatedAt(body); !ts.IsZero() {
			a.CreatedAt = ts
		} else {
			a.CreatedAt = time.Now().UTC()
		}
	}
	return a, nil
}

// extractCreatedAt pulls created_at if present (RFC3339 string).
func extractCreatedAt(body []byte) time.Time {
	var p struct {
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(body, &p); err != nil || p.CreatedAt == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, p.CreatedAt); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func writeJSON(w http.ResponseWriter, status int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}