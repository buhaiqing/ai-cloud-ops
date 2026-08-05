// M2-4: Alert rules CRUD.
//
// Schema (new table — migration 0002_rules.sql adds this):
//   alert_rules(
//     id BIGSERIAL PK,
//     account_alias TEXT NOT NULL,
//     name TEXT NOT NULL,
//     severity TEXT NOT NULL,
//     metric TEXT NOT NULL,
//     threshold NUMERIC,
//     channel JSONB,           -- e.g. {"type":"webhook","url":"..."} or {"type":"email","to":""}
//     enabled BOOLEAN NOT NULL DEFAULT TRUE,
//     created_at TIMESTAMPTZ DEFAULT now(),
//     updated_at TIMESTAMPTZ DEFAULT now()
//   )
//
// State machine: enabled/disabled toggle is a soft switch. Deletes are hard.

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Rule is the JSON contract for /rules.
type Rule struct {
	ID           int64           `json:"id"`
	AccountAlias string          `json:"account_alias"`
	Name         string          `json:"name"`
	Severity     string          `json:"severity"`
	Metric       string          `json:"metric"`
	Threshold    *float64        `json:"threshold,omitempty"`
	Channel      json.RawMessage `json:"channel"`
	Enabled      bool            `json:"enabled"`
	CreatedAt    string          `json:"created_at,omitempty"`
	UpdatedAt    string          `json:"updated_at,omitempty"`
}

// CreateRuleReq is the POST body.
type CreateRuleReq struct {
	AccountAlias string          `json:"account_alias"`
	Name         string          `json:"name"`
	Severity     string          `json:"severity"`
	Metric       string          `json:"metric"`
	Threshold    *float64        `json:"threshold,omitempty"`
	Channel      json.RawMessage `json:"channel"`
	Enabled      *bool           `json:"enabled,omitempty"`
}

// UpdateRuleReq allows partial update.
type UpdateRuleReq struct {
	Name      *string          `json:"name,omitempty"`
	Severity  *string          `json:"severity,omitempty"`
	Metric    *string          `json:"metric,omitempty"`
	Threshold *float64         `json:"threshold,omitempty"`
	Channel   *json.RawMessage `json:"channel,omitempty"`
	Enabled   *bool            `json:"enabled,omitempty"`
}

func listRulesHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps == nil || deps.Pool == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "db unavailable"})
			return
		}
		account := r.URL.Query().Get("account")
		sql := `SELECT id, account_alias, name, severity, metric, threshold, channel, enabled,
			created_at::text, updated_at::text FROM alert_rules WHERE 1=1`
		args := []any{}
		if account != "" {
			args = append(args, account)
			sql += " AND account_alias=$" + itoa(len(args))
		}
		sql += " ORDER BY id DESC LIMIT 500"
		rows, err := deps.Pool.Query(r.Context(), sql, args...)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		out := []Rule{}
		for rows.Next() {
			var rl Rule
			var threshold *float64
			if err := rows.Scan(&rl.ID, &rl.AccountAlias, &rl.Name, &rl.Severity,
				&rl.Metric, &threshold, &rl.Channel, &rl.Enabled,
				&rl.CreatedAt, &rl.UpdatedAt); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			rl.Threshold = threshold
			out = append(out, rl)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func createRuleHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps == nil || deps.Pool == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "db unavailable"})
			return
		}
		var req CreateRuleReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		if err := validateRuleReq(req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		channel := req.Channel
		if len(channel) == 0 {
			channel = json.RawMessage(`{}`)
		}
		var id int64
		err := deps.Pool.QueryRow(r.Context(),
			`INSERT INTO alert_rules (account_alias, name, severity, metric, threshold, channel, enabled)
			 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
			req.AccountAlias, req.Name, req.Severity, req.Metric,
			req.Threshold, channel, enabled).Scan(&id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})
	}
}

func updateRuleHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps == nil || deps.Pool == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "db unavailable"})
			return
		}
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		var req UpdateRuleReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		// Build dynamic SET clause.
		sets := []string{}
		args := []any{}
		if req.Name != nil {
			args = append(args, *req.Name)
			sets = append(sets, "name=$"+itoa(len(args)))
		}
		if req.Severity != nil {
			args = append(args, *req.Severity)
			sets = append(sets, "severity=$"+itoa(len(args)))
		}
		if req.Metric != nil {
			args = append(args, *req.Metric)
			sets = append(sets, "metric=$"+itoa(len(args)))
		}
		if req.Threshold != nil {
			args = append(args, *req.Threshold)
			sets = append(sets, "threshold=$"+itoa(len(args)))
		}
		if req.Channel != nil {
			args = append(args, *req.Channel)
			sets = append(sets, "channel=$"+itoa(len(args)))
		}
		if req.Enabled != nil {
			args = append(args, *req.Enabled)
			sets = append(sets, "enabled=$"+itoa(len(args)))
		}
		if len(sets) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no fields to update"})
			return
		}
		args = append(args, id)
		sql := "UPDATE alert_rules SET " + joinComma(sets) + ", updated_at=now() WHERE id=$" + itoa(len(args))
		tag, err := deps.Pool.Exec(r.Context(), sql, args...)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if tag.RowsAffected() == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "updated": true})
	}
}

func deleteRuleHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps == nil || deps.Pool == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "db unavailable"})
			return
		}
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		tag, err := deps.Pool.Exec(r.Context(), `DELETE FROM alert_rules WHERE id=$1`, id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if tag.RowsAffected() == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// validateRuleReq enforces minimum invariants. Pure function for unit tests.
func validateRuleReq(req CreateRuleReq) error {
	if req.AccountAlias == "" {
		return errors.New("account_alias is required")
	}
	if req.Name == "" {
		return errors.New("name is required")
	}
	if req.Severity == "" {
		return errors.New("severity is required")
	}
	if req.Severity != "critical" && req.Severity != "warning" && req.Severity != "info" {
		return errors.New("severity must be one of: critical, warning, info")
	}
	if req.Metric == "" {
		return errors.New("metric is required")
	}
	return nil
}

func joinComma(parts []string) string {
	var out strings.Builder
	for i, p := range parts {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(p)
	}
	return out.String()
}
