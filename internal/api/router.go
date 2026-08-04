// Package api: REST + WebSocket surface for the Web Dashboard (M2-7).
//
// See router_test.go for the contract.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/buhaiqing/ai-cloud-ops/internal/agent"
	"github.com/buhaiqing/ai-cloud-ops/internal/auth"
)

// Deps wires DB + future dependencies (auth, WS hub, ...) into the router.
// Passing nil for pool is allowed in tests that only probe routing.
type Deps struct {
	Pool *pgxpool.Pool
	Hub  *Hub // M2-8
	// M2-5: nil-safe. When nil, auth middleware is skipped and the auth
	// handlers are not mounted (existing routing tests keep working).
	Auth         *auth.Store
	AuthHandlers *auth.Handlers
	// M3-5: HITL execution. ExecStore is the persistence layer; Planner
	// produces the dry-run plan handed to the human; ExecAction is the
	// per-action executor (nil → defaultStubExecutor).
	ExecStore  ExecStore
	Planner    Planner
	ExecAction func(ctx context.Context, action agent.PlannedAction) (json.RawMessage, string, bool)
}

// Public paths bypass the auth middleware. Stats stays public so dashboards
// can render summary widgets before login; login/logout are public by
// definition; WS is authed inside wsHandler (cookie + origin check) rather
// than via the middleware to keep the upgrade request uninterrupted.
var publicPaths = map[string]bool{
	"/api/v1/ping":        true,
	"/api/v1/stats":       true,
	"/api/v1/auth/login":  true,
	"/api/v1/auth/logout": true,
	"/api/v1/ws":          true,
}

// mountRoutes installs all /api/v1 routes on r. Safe to call with nil deps
// for tests that only check routing shape; handlers that touch the DB will
// return 503 if Deps.Pool is nil.
func mountRoutes(r chi.Router, deps *Deps) {
	r.Route("/api/v1", func(sub chi.Router) {
		// Auth + CSRF middleware (skipped when deps.Auth is nil so existing
		// routing tests keep working without a store).
		if deps != nil && deps.Auth != nil {
			sub.Use(auth.Middleware(deps.Auth, publicPaths))
		}

		// Auth endpoints (M2-5). Built from env in cmd/.
		if deps != nil && deps.Auth != nil && deps.AuthHandlers != nil {
			sub.Post("/auth/login", deps.AuthHandlers.Login)
			sub.Post("/auth/logout", deps.AuthHandlers.Logout)
			sub.Get("/auth/me", deps.AuthHandlers.Me)
		}

		sub.Get("/ping", pingHandler)
		sub.Get("/stats", statsHandler(deps))

		// Read endpoints — M2-2, M2-3
		sub.Get("/accounts", listAccountsHandler(deps))
		sub.Get("/resources", listResourcesHandler(deps))
		sub.Get("/alerts", listAlertsHandler(deps))
		sub.Get("/alerts/{id}", getAlertHandler(deps))
		sub.Get("/analyses/{id}", getAnalysisHandler(deps))

		// Rules CRUD — M2-4
		sub.Get("/rules", listRulesHandler(deps))
		sub.Post("/rules", createRuleHandler(deps))
		sub.Delete("/rules/{id}", deleteRuleHandler(deps))
		sub.Post("/incidents/{id}/ack", incidentTransitionHandler(deps, "acknowledged"))
		sub.Post("/incidents/{id}/suppress", incidentTransitionHandler(deps, "suppressed"))
		sub.Post("/incidents/{id}/maintenance", incidentTransitionHandler(deps, "maintenance"))
		sub.Post("/incidents/{id}/replay", incidentReplayHandler(deps))
		sub.Post("/incidents/{id}/resolve", incidentTransitionHandler(deps, "resolved"))

		// Real-time — M2-8
		sub.Get("/ws", wsHandler(deps))

		// HITL execution — M3-5
		sub.Post("/exec/plan", execPlanHandler(deps))
		sub.Post("/exec/approve", execApproveHandler(deps))
		sub.Post("/exec/{exec_id}/execute", execExecuteHandler(deps))
		sub.Get("/exec/{exec_id}", execGetHandler(deps))
		sub.Get("/executions", listExecutionsHandler(deps))
	})
}

// Mount installs routes onto r using the given deps. Exported for the
// backend's serve command to wire up the HTTP server.
func Mount(r chi.Router, deps *Deps) {
	mountRoutes(r, deps)
}

// --- handlers ---

func pingHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ts": time.Now().UTC()})
}

// statsHandler returns aggregate counts. With a nil pool it returns zeros
// (the contract test only requires the keys to exist).
func statsHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out := map[string]any{
			"total_alerts":      0,
			"open_alerts":       0,
			"ai_success_rate":   0.0,
			"avg_latency_ms":    0,
			"resources_covered": 0,
			"generated_at":      time.Now().UTC(),
		}
		if deps == nil || deps.Pool == nil {
			writeJSON(w, http.StatusOK, out)
			return
		}
		// Real implementation deferred to M2-9 (stats dashboard) — live in db.
		// For now the contract is satisfied with zeros.
		writeJSON(w, http.StatusOK, out)
	}
}

func listAccountsHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps == nil || deps.Pool == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "db unavailable"})
			return
		}
		rows, err := deps.Pool.Query(r.Context(),
			`SELECT alias, regions FROM accounts ORDER BY alias`)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		type acc struct {
			Alias   string   `json:"alias"`
			Regions []string `json:"regions"`
		}
		out := []acc{}
		for rows.Next() {
			var a acc
			var regionsJSON []byte
			if err := rows.Scan(&a.Alias, &regionsJSON); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			_ = json.Unmarshal(regionsJSON, &a.Regions)
			out = append(out, a)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func listResourcesHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps == nil || deps.Pool == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "db unavailable"})
			return
		}
		q := r.URL.Query()
		account := q.Get("account")
		region := q.Get("region")
		rtype := q.Get("type")
		sql := `SELECT account_alias, region, resource_type, resource_id, name, fetched_at
			FROM resources WHERE 1=1`
		args := []any{}
		if account != "" {
			args = append(args, account)
			sql += " AND account_alias=$" + itoa(len(args))
		}
		if region != "" {
			args = append(args, region)
			sql += " AND region=$" + itoa(len(args))
		}
		if rtype != "" {
			args = append(args, rtype)
			sql += " AND resource_type=$" + itoa(len(args))
		}
		sql += " ORDER BY fetched_at DESC LIMIT 500"
		rows, err := deps.Pool.Query(r.Context(), sql, args...)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		type res struct {
			Account   string `json:"account"`
			Region    string `json:"region"`
			Type      string `json:"type"`
			ID        string `json:"id"`
			Name      string `json:"name"`
			FetchedAt string `json:"fetched_at"`
		}
		out := []res{}
		for rows.Next() {
			var x res
			var ts string
			if err := rows.Scan(&x.Account, &x.Region, &x.Type, &x.ID, &x.Name, &ts); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			x.FetchedAt = ts
			out = append(out, x)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func listAlertsHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps == nil || deps.Pool == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "db unavailable"})
			return
		}
		q := r.URL.Query()
		account := q.Get("account")
		status := q.Get("status")
		limit := atoiDefault(q.Get("limit"), 50)
		sql := `SELECT id, alert_id, account_alias, region, severity, status,
			COALESCE(name,''), created_at
			FROM alerts WHERE 1=1`
		args := []any{}
		if account != "" {
			args = append(args, account)
			sql += " AND account_alias=$" + itoa(len(args))
		}
		if status != "" {
			args = append(args, status)
			sql += " AND status=$" + itoa(len(args))
		}
		args = append(args, limit)
		sql += " ORDER BY created_at DESC LIMIT $" + itoa(len(args))
		rows, err := deps.Pool.Query(r.Context(), sql, args...)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		type alert struct {
			ID        int64  `json:"id"`
			AlertID   string `json:"alert_id"`
			Account   string `json:"account"`
			Region    string `json:"region"`
			Severity  string `json:"severity"`
			Status    string `json:"status"`
			Name      string `json:"name"`
			CreatedAt string `json:"created_at"`
		}
		out := []alert{}
		for rows.Next() {
			var a alert
			var ts string
			if err := rows.Scan(&a.ID, &a.AlertID, &a.Account, &a.Region, &a.Severity, &a.Status, &a.Name, &ts); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			a.CreatedAt = ts
			out = append(out, a)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func getAlertHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps == nil || deps.Pool == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "db unavailable"})
			return
		}
		id := chi.URLParam(r, "id")
		var (
			alertID, account, region, severity, status, name string
			createdAt                                        string
		)
		err := deps.Pool.QueryRow(r.Context(),
			`SELECT alert_id, account_alias, region, severity, status,
				COALESCE(name,''), created_at::text
				FROM alerts WHERE id=$1`, id).
			Scan(&alertID, &account, &region, &severity, &status, &name, &createdAt)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		// Linked analyses (most recent 5).
		aRows, _ := deps.Pool.Query(r.Context(),
			`SELECT id, model, root_cause, latency_ms, created_at::text
				FROM analyses WHERE alert_id=$1 ORDER BY created_at DESC LIMIT 5`, id)
		type ana struct {
			ID        int64  `json:"id"`
			Model     string `json:"model"`
			RootCause string `json:"root_cause"`
			LatencyMS int    `json:"latency_ms"`
			CreatedAt string `json:"created_at"`
		}
		linked := []ana{}
		if aRows != nil {
			defer aRows.Close()
			for aRows.Next() {
				var a ana
				if err := aRows.Scan(&a.ID, &a.Model, &a.RootCause, &a.LatencyMS, &a.CreatedAt); err == nil {
					linked = append(linked, a)
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":         id,
			"alert_id":   alertID,
			"account":    account,
			"region":     region,
			"severity":   severity,
			"status":     status,
			"name":       name,
			"created_at": createdAt,
			"analyses":   linked,
		})
	}
}

func getAnalysisHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps == nil || deps.Pool == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "db unavailable"})
			return
		}
		id := chi.URLParam(r, "id")
		var (
			model, promptVersion, rootCause string
			recommendations, evidence       []byte
			latency                         int
			createdAt                       string
		)
		err := deps.Pool.QueryRow(r.Context(),
			`SELECT model, prompt_version, root_cause, recommendations, evidence_chains,
				latency_ms, created_at::text
				FROM analyses WHERE id=$1`, id).
			Scan(&model, &promptVersion, &rootCause, &recommendations, &evidence,
				&latency, &createdAt)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":              id,
			"model":           model,
			"prompt_version":  promptVersion,
			"root_cause":      rootCause,
			"recommendations": json.RawMessage(recommendations),
			"evidence":        json.RawMessage(evidence),
			"latency_ms":      latency,
			"created_at":      createdAt,
		})
	}
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func atoiDefault(s string, def int) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	if n == 0 {
		return def
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
