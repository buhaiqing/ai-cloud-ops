// M3-5: HITL execution endpoints.
//
// Lifecycle (contract-m3-4 state_machine_for_executions):
//
//	planned → approved | rejected
//	approved → running
//	running → completed | failed
//	failed → rolled_back | completed
//
// Endpoints (all behind the standard auth + CSRF middleware):
//
//	POST /api/v1/exec/plan                       — create a dry-run plan from a diagnosis
//	POST /api/v1/exec/approve                    — approve a planned plan
//	POST /api/v1/exec/{exec_id}/execute          — execute an approved plan (sync MVP)
//	GET  /api/v1/exec/{exec_id}                  — fetch plan + audit trail
//	GET  /api/v1/executions[?account=&limit=]    — list recent plans
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/buhaiqing/ai-cloud-ops/internal/agent"
	"github.com/buhaiqing/ai-cloud-ops/internal/auth"
)

// Sentinel errors surfaced by ExecStore implementations so handlers can map
// them to 404 / 409 without leaking store-specific error types.
var (
	ErrExecNotFound = errors.New("exec plan not found")
	ErrExecConflict = errors.New("exec state conflict")
)

// ExecPlanRecord mirrors a row in exec_plans. JSON tags drive both the
// pgxExecStore Scan and the GET response shape.
type ExecPlanRecord struct {
	ID               int64           `json:"id"`
	DiagnosisID      int64           `json:"diagnosis_id"`
	AccountAlias     string          `json:"account_alias"`
	DryRun           bool            `json:"dry_run"`
	WouldExecute     json.RawMessage `json:"would_execute"`
	BlockedByPolicy  json.RawMessage `json:"blocked_by_policy"`
	Status           string          `json:"status"`
	ApproverNote     *string         `json:"approver_note,omitempty"`
	CreatedBy        string          `json:"created_by"`
	ApprovedBy       *string         `json:"approved_by,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	ApprovedAt       *time.Time      `json:"approved_at,omitempty"`
	StartedAt        *time.Time      `json:"started_at,omitempty"`
	CompletedAt      *time.Time      `json:"completed_at,omitempty"`
	ActionsTotal     int             `json:"actions_total"`
	ActionsCompleted int             `json:"actions_completed"`
}

// ExecAuditRecord mirrors a row in exec_audit.
type ExecAuditRecord struct {
	ID             int64           `json:"id"`
	ExecID         int64           `json:"exec_id"`
	Seq            int             `json:"seq"`
	Action         json.RawMessage `json:"action"`
	ActionName     string          `json:"action_name"`
	TargetResource string          `json:"target_resource"`
	PreState       json.RawMessage `json:"pre_state"`
	PostState      json.RawMessage `json:"post_state"`
	Status         string          `json:"status"`
	Error          *string         `json:"error"`
	StartedAt      *time.Time      `json:"started_at"`
	CompletedAt    *time.Time      `json:"completed_at"`
}

// ExecStore is the contract the HTTP handlers depend on; the in-memory fake
// in exec_test.go and the pgxExecStore below both implement it.
type ExecStore interface {
	CreatePlan(ctx context.Context, p ExecPlanRecord) (int64, error)
	GetPlan(ctx context.Context, id int64) (*ExecPlanRecord, error) // nil,nil when missing
	LoadDiagnosisAlert(ctx context.Context, diagnosisID int64) (map[string]any, string, error)
	ApprovePlan(ctx context.Context, id int64, approver, note string) (*ExecPlanRecord, error)
	BeginExecution(ctx context.Context, id int64, actions []agent.PlannedAction) error
	RecordActionResult(ctx context.Context, execID int64, seq int, postState json.RawMessage, status, errMsg string) error
	FinishExecution(ctx context.Context, id int64, status string, completed int) error
	CountExecutionsSince(ctx context.Context, accountAlias string, since time.Time) (int, error)
	AuditRows(ctx context.Context, execID int64) ([]ExecAuditRecord, error)
}

// ExecLister is opt-in: stores that can list historical executions satisfy
// it. Kept separate from ExecStore so the test fake (which doesn't need
// listing) doesn't have to grow a no-op implementation.
type ExecLister interface {
	ListExecutions(ctx context.Context, account string, limit int) ([]ExecPlanRecord, error)
}

// ExecRollbackMarker is opt-in: stores that can flip successful audit rows
// to 'rolled_back' satisfy it. Kept separate from ExecStore (like ExecLister)
// so existing test fakes don't have to grow a no-op implementation.
type ExecRollbackMarker interface {
	// MarkAuditRolledBack flips audit rows with status='success' for the
	// given exec and seqs to 'rolled_back'. Returns count updated.
	MarkAuditRolledBack(ctx context.Context, execID int64, seqs []int) (int, error)
}

// Planner produces the dry-run plan handed to the human. *agent.Client
// already satisfies this signature.
type Planner interface {
	DiagnoseDryRun(ctx context.Context, alert map[string]any) (*agent.DryRunResult, error)
}

// validExecTransitions is the exec state machine (contract-m3-4).
var validExecTransitions = map[string]map[string]bool{
	"planned": {
		"approved": true,
		"rejected": true,
	},
	"approved": {
		"running": true,
	},
	"running": {
		"completed": true,
		"failed":    true,
	},
	"failed": {
		"rolled_back": true,
		"completed":   true,
	},
}

func canExecTransition(from, to string) bool {
	allowed, ok := validExecTransitions[from]
	if !ok {
		return false
	}
	return allowed[to]
}

// defaultExecRateLimit is used when EXEC_RATE_LIMIT is unset or unparseable.
const defaultExecRateLimit = 10

// execRateLimit reads EXEC_RATE_LIMIT on every call (tests use t.Setenv).
func execRateLimit() int {
	v := os.Getenv("EXEC_RATE_LIMIT")
	if v == "" {
		return defaultExecRateLimit
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultExecRateLimit
	}
	return n
}

// sessionUser extracts the login name from the auth session, defaulting to
// "system" for the few code paths that run without one (e.g. bootstrap).
func sessionUser(ctx context.Context) string {
	if sess, ok := auth.FromContext(ctx); ok {
		return sess.User
	}
	return "system"
}

// --- handlers ---

func execPlanHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps == nil || deps.ExecStore == nil || deps.Planner == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "exec store unavailable"})
			return
		}
		var req struct {
			DiagnosisID int64  `json:"diagnosis_id"`
			DryRun      *bool  `json:"dry_run,omitempty"`
			AccountHint string `json:"account,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
			return
		}
		if req.DiagnosisID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "diagnosis_id is required"})
			return
		}
		alert, account, err := deps.ExecStore.LoadDiagnosisAlert(r.Context(), req.DiagnosisID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if alert == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "diagnosis not found"})
			return
		}
		result, err := deps.Planner.DiagnoseDryRun(r.Context(), alert)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		dryRun := true
		if req.DryRun != nil {
			dryRun = *req.DryRun
		}
		wouldExec, err := json.Marshal(result.WouldExecute)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if len(wouldExec) == 0 || string(wouldExec) == "null" {
			wouldExec = json.RawMessage(`[]`)
		}
		blocked, err := json.Marshal(result.BlockedByPolicy)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if len(blocked) == 0 || string(blocked) == "null" {
			blocked = json.RawMessage(`[]`)
		}

		plan := ExecPlanRecord{
			DiagnosisID:     req.DiagnosisID,
			AccountAlias:    account,
			DryRun:          dryRun,
			WouldExecute:    wouldExec,
			BlockedByPolicy: blocked,
			CreatedBy:       sessionUser(r.Context()),
			Status:          "planned",
			ActionsTotal:    len(result.WouldExecute),
		}
		id, err := deps.ExecStore.CreatePlan(r.Context(), plan)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"plan_id":           id,
			"would_execute":     result.WouldExecute,
			"blocked_by_policy": result.BlockedByPolicy,
		})
	}
}

func execApproveHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps == nil || deps.ExecStore == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "exec store unavailable"})
			return
		}
		var req struct {
			PlanID       int64  `json:"plan_id"`
			ApproverNote string `json:"approver_note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
			return
		}
		if req.PlanID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "plan_id is required"})
			return
		}
		_, err := deps.ExecStore.ApprovePlan(r.Context(), req.PlanID, sessionUser(r.Context()), req.ApproverNote)
		switch {
		case errors.Is(err, ErrExecNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "plan not found"})
			return
		case errors.Is(err, ErrExecConflict):
			current, _ := deps.ExecStore.GetPlan(r.Context(), req.PlanID)
			from := ""
			if current != nil {
				from = current.Status
			}
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":   ErrExecConflict.Error(),
				"from":    from,
				"to":      "approved",
				"allowed": execAllowedFrom(from),
			})
			return
		case err != nil:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"exec_id": req.PlanID,
			"status":  "approved",
		})
	}
}

func execExecuteHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps == nil || deps.ExecStore == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "exec store unavailable"})
			return
		}
		idStr := chi.URLParam(r, "exec_id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid exec_id"})
			return
		}
		plan, err := deps.ExecStore.GetPlan(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if plan == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "plan not found"})
			return
		}
		if !canExecTransition(plan.Status, "running") {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":   ErrExecConflict.Error(),
				"from":    plan.Status,
				"to":      "running",
				"allowed": execAllowedFrom(plan.Status),
			})
			return
		}
		// Rate limit: count executions already started for this account in
		// the last hour. Plan stays "approved" on 429 — check happens before
		// BeginExecution.
		since := time.Now().UTC().Add(-1 * time.Hour)
		count, err := deps.ExecStore.CountExecutionsSince(r.Context(), plan.AccountAlias, since)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if count >= execRateLimit() {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
			return
		}

		var actions []agent.PlannedAction
		if len(plan.WouldExecute) > 0 {
			if err := json.Unmarshal(plan.WouldExecute, &actions); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
		if err := deps.ExecStore.BeginExecution(r.Context(), id, actions); err != nil {
			switch {
			case errors.Is(err, ErrExecNotFound):
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "plan not found"})
			case errors.Is(err, ErrExecConflict):
				writeJSON(w, http.StatusConflict, map[string]any{
					"error":   ErrExecConflict.Error(),
					"from":    plan.Status,
					"to":      "running",
					"allowed": execAllowedFrom(plan.Status),
				})
			default:
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			}
		}

		exec := deps.ExecAction
		if exec == nil {
			exec = defaultStubExecutor
		}
		completed := 0
		failed := false
		preStates := make([]json.RawMessage, 0, len(actions))
		for i, a := range actions {
			var preState json.RawMessage
			if deps.ExecAction != nil {
				preState, _, _ = deps.ExecAction(r.Context(), a)
			}
			preStates = append(preStates, preState)
			postState, errMsg, ok := exec(r.Context(), a)
			status := "success"
			if !ok {
				status = "failed"
			}
			if recErr := deps.ExecStore.RecordActionResult(r.Context(), id, i+1, postState, status, errMsg); recErr != nil {
				if errors.Is(recErr, ErrExecNotFound) {
					break
				}
			}
			if !ok {
				failed = true
				break
			}
			completed++
		}
		finalStatus := "completed"
		rolledBack := 0
		if failed && completed > 0 && deps.RollbackAction != nil && os.Getenv("AICO_ROLLBACK_ENABLED") == "true" {
			rolledSeqs := make([]int, 0, completed)
			rollbackErrors := []string{}
			for i := completed - 1; i >= 0; i-- {
				_, rbErr, rbOK := deps.RollbackAction(r.Context(), actions[i], preStates[i])
				if rbOK {
					rolledSeqs = append(rolledSeqs, i+1)
				} else {
					rollbackErrors = append(rollbackErrors, fmt.Sprintf("action %d: %s", i+1, rbErr))
				}
			}
			if len(rolledSeqs) > 0 {
				if marker, ok := deps.ExecStore.(ExecRollbackMarker); ok {
					if _, rbStoreErr := marker.MarkAuditRolledBack(r.Context(), id, rolledSeqs); rbStoreErr != nil {
						rollbackErrors = append(rollbackErrors, rbStoreErr.Error())
					}
				} else {
					slog.Warn("exec.rollback.store_marker_unavailable", "exec_id", id)
				}
			}
			if len(rollbackErrors) == 0 {
				finalStatus = "rolled_back"
			} else {
				slog.Warn("exec.rollback.partial_failure", "exec_id", id, "errors", rollbackErrors)
			}
			rolledBack = len(rolledSeqs)
		}
		if failed && finalStatus == "completed" {
			finalStatus = "failed"
		}
		if err := deps.ExecStore.FinishExecution(r.Context(), id, finalStatus, completed); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"exec_id":       id,
			"status":        "running",
			"actions_total": len(actions),
			"completed":     completed,
			"final_status":  finalStatus,
			"rolled_back":   rolledBack,
		})
	}
}

func execGetHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps == nil || deps.ExecStore == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "exec store unavailable"})
			return
		}
		idStr := chi.URLParam(r, "exec_id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid exec_id"})
			return
		}
		plan, err := deps.ExecStore.GetPlan(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if plan == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "plan not found"})
			return
		}
		rows, err := deps.ExecStore.AuditRows(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []ExecAuditRecord{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"exec_id":           plan.ID,
			"plan_id":           plan.ID,
			"diagnosis_id":      plan.DiagnosisID,
			"account_alias":     plan.AccountAlias,
			"status":            plan.Status,
			"actions_total":     plan.ActionsTotal,
			"actions_completed": plan.ActionsCompleted,
			"started_at":        plan.StartedAt,
			"completed_at":      plan.CompletedAt,
			"created_at":        plan.CreatedAt,
			"approved_at":       plan.ApprovedAt,
			"approved_by":       plan.ApprovedBy,
			"audit_trail":       rows,
		})
	}
}

func listExecutionsHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps == nil || deps.ExecStore == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "exec store unavailable"})
			return
		}
		lister, ok := deps.ExecStore.(ExecLister)
		if !ok {
			writeJSON(w, http.StatusOK, []ExecPlanRecord{})
			return
		}
		account := r.URL.Query().Get("account")
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
				limit = n
			}
		}
		rows, err := lister.ListExecutions(r.Context(), account, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []ExecPlanRecord{}
		}
		writeJSON(w, http.StatusOK, rows)
	}
}

// execAllowedFrom returns the list of valid `to` states for `from` so
// handlers can emit it in 409 bodies.
func execAllowedFrom(from string) []string {
	allowed := validExecTransitions[from]
	if len(allowed) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(allowed))
	for k := range allowed {
		out = append(out, k)
	}
	return out
}

// defaultStubExecutor lets the MVP run end-to-end without a real cloud
// driver. Returns a deterministic post-state and reports success.
func defaultStubExecutor(_ context.Context, a agent.PlannedAction) (json.RawMessage, string, bool) {
	body := map[string]any{
		"status": "executed",
		"source": "stub",
		"tool":   a.ToolName,
		"target": a.TargetResource,
	}
	raw, _ := json.Marshal(body)
	return raw, "", true
}
