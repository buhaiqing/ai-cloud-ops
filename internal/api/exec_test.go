// M3-5 tests: exec plan/approve/execute/get endpoints.
// Contract: audit-results/contract-m3-5.md (+ contract-m3-4 state machine).
//
// DB layer is tested through the ExecStore interface stand-in
// (docs/backend-standards.md §2.3): a mutex-guarded in-memory fake below.
// No real Postgres is required.

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/buhaiqing/ai-cloud-ops/internal/agent"
	"github.com/buhaiqing/ai-cloud-ops/internal/auth"
)

// --- in-memory ExecStore fake (test-only) ---

type seededAlert struct {
	alert   map[string]any
	account string
}

type memExecStore struct {
	mu     sync.Mutex
	nextID int64
	plans  map[int64]*ExecPlanRecord
	audit  map[int64][]ExecAuditRecord
	alerts map[int64]seededAlert // diagnosis_id → alert context
}

func newMemExecStore() *memExecStore {
	return &memExecStore{
		plans:  map[int64]*ExecPlanRecord{},
		audit:  map[int64][]ExecAuditRecord{},
		alerts: map[int64]seededAlert{},
	}
}

func (m *memExecStore) CreatePlan(_ context.Context, p ExecPlanRecord) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	p.ID = m.nextID
	p.CreatedAt = time.Now()
	if p.Status == "" {
		p.Status = "planned"
	}
	m.plans[p.ID] = &p
	return p.ID, nil
}

func (m *memExecStore) GetPlan(_ context.Context, id int64) (*ExecPlanRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.plans[id]
	if p == nil {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (m *memExecStore) LoadDiagnosisAlert(_ context.Context, diagnosisID int64) (map[string]any, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.alerts[diagnosisID]
	if !ok {
		return nil, "", nil
	}
	return s.alert, s.account, nil
}

func (m *memExecStore) ApprovePlan(_ context.Context, id int64, approver, note string) (*ExecPlanRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.plans[id]
	if p == nil {
		return nil, ErrExecNotFound
	}
	if p.Status != "planned" {
		return nil, ErrExecConflict
	}
	now := time.Now()
	p.Status = "approved"
	p.ApprovedBy = &approver
	p.ApprovedAt = &now
	p.ApproverNote = &note
	cp := *p
	return &cp, nil
}

func (m *memExecStore) BeginExecution(_ context.Context, id int64, actions []agent.PlannedAction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.plans[id]
	if p == nil {
		return ErrExecNotFound
	}
	if p.Status != "approved" {
		return ErrExecConflict
	}
	now := time.Now()
	p.Status = "running"
	p.StartedAt = &now
	p.ActionsTotal = len(actions)
	for seq, action := range actions {
		raw, _ := json.Marshal(action)
		m.audit[id] = append(m.audit[id], ExecAuditRecord{
			Seq:            seq + 1,
			Action:         raw,
			ActionName:     action.ToolName,
			TargetResource: action.TargetResource,
			PreState:       json.RawMessage(`{"status":"captured","source":"stub"}`),
			Status:         "pending",
		})
	}
	return nil
}

func (m *memExecStore) RecordActionResult(_ context.Context, execID int64, seq int, postState json.RawMessage, status, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := m.audit[execID]
	for i := range rows {
		if rows[i].Seq == seq {
			rows[i].PostState = postState
			rows[i].Status = status
			if errMsg != "" {
				rows[i].Error = &errMsg
			}
			now := time.Now()
			rows[i].CompletedAt = &now
			return nil
		}
	}
	return ErrExecNotFound
}

func (m *memExecStore) FinishExecution(_ context.Context, id int64, status string, completed int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.plans[id]
	if p == nil {
		return ErrExecNotFound
	}
	p.Status = status
	p.ActionsCompleted = completed
	now := time.Now()
	p.CompletedAt = &now
	return nil
}

func (m *memExecStore) CountExecutionsSince(_ context.Context, accountAlias string, since time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, p := range m.plans {
		if p.AccountAlias != accountAlias || p.StartedAt == nil || p.StartedAt.Before(since) {
			continue
		}
		switch p.Status {
		case "running", "completed", "failed", "rolled_back":
			count++
		}
	}
	return count, nil
}

func (m *memExecStore) AuditRows(_ context.Context, execID int64) ([]ExecAuditRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ExecAuditRecord(nil), m.audit[execID]...), nil
}

// --- stub planner ---

type stubPlanner struct {
	result *agent.DryRunResult
	err    error
}

func (s *stubPlanner) DiagnoseDryRun(_ context.Context, _ map[string]any) (*agent.DryRunResult, error) {
	return s.result, s.err
}

func dryRunFixture() *agent.DryRunResult {
	return &agent.DryRunResult{
		Diagnosis: agent.Diagnosis{
			RootCause:  "RDS connection pool exhausted",
			Confidence: "high",
		},
		DryRun: true,
		WouldExecute: []agent.PlannedAction{{
			ToolName:       "restart_rds_instance",
			Command:        `{"region":"cn-hangzhou","resource_id":"rm-abc123"}`,
			TargetResource: "rm-abc123",
			RiskLevel:      "medium",
			Rollback:       "n/a",
		}},
		EstimatedTotalDowntimeS: 45,
		BlockedByPolicy:         []string{"tool delete_ecs_instance not in WRITE_TOOLS whitelist"},
	}
}

// newExecRouter mounts the four M3-5 routes behind the real auth middleware
// so 401/403/CSRF behavior is covered end to end.
func newExecRouter(deps *Deps) http.Handler {
	if deps.Auth == nil {
		deps.Auth = auth.NewStore(false)
	}
	r := chi.NewRouter()
	r.Use(auth.Middleware(deps.Auth, nil))
	sub := chi.NewRouter()
	sub.Post("/exec/plan", execPlanHandler(deps))
	sub.Post("/exec/approve", execApproveHandler(deps))
	sub.Post("/exec/{exec_id}/execute", execExecuteHandler(deps))
	sub.Get("/exec/{exec_id}", execGetHandler(deps))
	r.Mount("/api/v1", sub)
	return r
}

// authedRequest signs a request with a live session + CSRF header.
func authedRequest(t *testing.T, store *auth.Store, method, path string, body ...string) *http.Request {
	t.Helper()
	sess, err := store.Issue("admin")
	if err != nil {
		t.Fatalf("session issue: %v", err)
	}
	bodyStr := ""
	if len(body) > 0 {
		bodyStr = body[0]
	}
	req := httptest.NewRequest(method, path, strings.NewReader(bodyStr))
	req.AddCookie(&http.Cookie{Name: "aico_session", Value: sess.ID})
	req.Header.Set("Content-Type", "application/json")
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", sess.CSRF)
	}
	return req
}

func execDeps(t *testing.T, store *memExecStore, planner *stubPlanner) *Deps {
	t.Helper()
	return &Deps{
		Auth:      auth.NewStore(false),
		ExecStore: store,
		Planner:   planner,
	}
}

// seedPlannedPlan creates an approved-ready plan directly in the store.
func seedPlan(t *testing.T, store *memExecStore, status, account string) int64 {
	t.Helper()
	raw, _ := json.Marshal(dryRunFixture().WouldExecute)
	blocked, _ := json.Marshal(dryRunFixture().BlockedByPolicy)
	id, err := store.CreatePlan(context.Background(), ExecPlanRecord{
		DiagnosisID:     1,
		AccountAlias:    account,
		DryRun:          true,
		WouldExecute:    raw,
		BlockedByPolicy: blocked,
		CreatedBy:       "system",
		ActionsTotal:    1,
	})
	if err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	if status == "approved" {
		if _, err := store.ApprovePlan(context.Background(), id, "admin", "seed"); err != nil {
			t.Fatalf("seed approve: %v", err)
		}
	}
	return id
}

// --- state machine (contract-m3-4 state_machine_for_executions) ---

func TestCanExecTransition(t *testing.T) {
	legal := [][2]string{
		{"planned", "approved"}, {"planned", "rejected"},
		{"approved", "running"},
		{"running", "completed"}, {"running", "failed"},
		{"failed", "rolled_back"}, {"failed", "completed"},
	}
	for _, pair := range legal {
		if !canExecTransition(pair[0], pair[1]) {
			t.Errorf("canExecTransition(%s→%s) = false, want true", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]string{
		{"planned", "running"}, {"planned", "completed"},
		{"approved", "completed"}, {"approved", "approved"},
		{"running", "approved"}, {"completed", "running"},
	} {
		if canExecTransition(pair[0], pair[1]) {
			t.Errorf("canExecTransition(%s→%s) = true, want false", pair[0], pair[1])
		}
	}
}

// --- POST /exec/plan ---

func TestExecPlan_CreatesPlan(t *testing.T) {
	store := newMemExecStore()
	store.alerts[1] = seededAlert{alert: map[string]any{"alert_id": "a-1"}, account: "main"}
	deps := execDeps(t, store, &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/plan", `{"diagnosis_id":1}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	planID, _ := body["plan_id"].(float64)
	if planID < 1 {
		t.Fatalf("plan_id = %v, want >= 1", body["plan_id"])
	}
	if len(body["would_execute"].([]any)) != 1 {
		t.Fatalf("would_execute = %v", body["would_execute"])
	}
	if len(body["blocked_by_policy"].([]any)) != 1 {
		t.Fatalf("blocked_by_policy = %v", body["blocked_by_policy"])
	}
	plan, _ := store.GetPlan(context.Background(), int64(planID))
	if plan.Status != "planned" || plan.AccountAlias != "main" || !plan.DryRun {
		t.Fatalf("persisted plan = %+v", plan)
	}
}

func TestExecPlan_DryRunDefaultsTrue(t *testing.T) {
	store := newMemExecStore()
	store.alerts[2] = seededAlert{alert: map[string]any{}, account: "main"}
	deps := execDeps(t, store, &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/plan", `{"diagnosis_id":2}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	plan, _ := store.GetPlan(context.Background(), 1)
	if plan == nil || !plan.DryRun {
		t.Fatalf("dry_run must default to true, got %+v", plan)
	}
}

func TestExecPlan_MissingDiagnosisID_400(t *testing.T) {
	deps := execDeps(t, newMemExecStore(), &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/plan", `{}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestExecPlan_InvalidJSON_400(t *testing.T) {
	deps := execDeps(t, newMemExecStore(), &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/plan", `{not-json`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestExecPlan_DiagnosisNotFound_404(t *testing.T) {
	deps := execDeps(t, newMemExecStore(), &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/plan", `{"diagnosis_id":999}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestExecPlan_PlannerError_500(t *testing.T) {
	store := newMemExecStore()
	store.alerts[1] = seededAlert{alert: map[string]any{}, account: "main"}
	deps := execDeps(t, store, &stubPlanner{err: context.DeadlineExceeded})
	router := newExecRouter(deps)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/plan", `{"diagnosis_id":1}`))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// --- POST /exec/approve ---

func TestExecApprove_HappyPath(t *testing.T) {
	store := newMemExecStore()
	id := seedPlan(t, store, "planned", "main")
	deps := execDeps(t, store, &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/approve", `{"plan_id":1,"approver_note":"approved for maintenance window"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["status"] != "approved" || body["exec_id"].(float64) != float64(id) {
		t.Fatalf("body = %v", body)
	}
	plan, _ := store.GetPlan(context.Background(), id)
	if plan.Status != "approved" || plan.ApprovedBy == nil || *plan.ApprovedBy != "admin" {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.ApproverNote == nil || *plan.ApproverNote != "approved for maintenance window" {
		t.Fatalf("approver_note = %v", plan.ApproverNote)
	}
}

func TestExecApprove_NotFound_404(t *testing.T) {
	deps := execDeps(t, newMemExecStore(), &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/approve", `{"plan_id":42}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestExecApprove_IllegalTransition_409(t *testing.T) {
	store := newMemExecStore()
	seedPlan(t, store, "approved", "main")
	deps := execDeps(t, store, &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/approve", `{"plan_id":1}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	for _, key := range []string{"error", "from", "to", "allowed"} {
		if body[key] == nil {
			t.Fatalf("409 body missing %q: %v", key, body)
		}
	}
}

// --- POST /exec/{exec_id}/execute ---

func TestExecExecute_HappyPath(t *testing.T) {
	store := newMemExecStore()
	id := seedPlan(t, store, "approved", "main")
	deps := execDeps(t, store, &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/1/execute", `{}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["status"] != "running" || body["actions_total"].(float64) != 1 {
		t.Fatalf("body = %v", body)
	}
	plan, _ := store.GetPlan(context.Background(), id)
	if plan.Status != "completed" || plan.ActionsCompleted != 1 {
		t.Fatalf("plan after sync execution = %+v", plan)
	}
	rows, _ := store.AuditRows(context.Background(), id)
	if len(rows) != 1 || rows[0].Status != "success" || rows[0].PostState == nil {
		t.Fatalf("audit rows = %+v", rows)
	}
}

func TestExecExecute_FromPlanned_409(t *testing.T) {
	store := newMemExecStore()
	seedPlan(t, store, "planned", "main")
	deps := execDeps(t, store, &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/1/execute", `{}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestExecExecute_NotFound_404(t *testing.T) {
	deps := execDeps(t, newMemExecStore(), &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/77/execute", `{}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestExecExecute_FailedActionMarksFailed(t *testing.T) {
	store := newMemExecStore()
	id := seedPlan(t, store, "approved", "main")
	deps := execDeps(t, store, &stubPlanner{result: dryRunFixture()})
	deps.ExecAction = func(_ context.Context, action agent.PlannedAction) (json.RawMessage, string, bool) {
		return nil, "simulated cloud error", false
	}
	router := newExecRouter(deps)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/1/execute", `{}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	plan, _ := store.GetPlan(context.Background(), id)
	if plan.Status != "failed" || plan.ActionsCompleted != 0 {
		t.Fatalf("plan = %+v", plan)
	}
	rows, _ := store.AuditRows(context.Background(), id)
	if len(rows) != 1 || rows[0].Status != "failed" || rows[0].Error == nil {
		t.Fatalf("audit rows = %+v", rows)
	}
}

func TestExecExecute_RateLimit_429(t *testing.T) {
	t.Setenv("EXEC_RATE_LIMIT", "10")
	store := newMemExecStore()
	for i := 0; i < 10; i++ {
		seedPlan(t, store, "approved", "main")
		_ = store.BeginExecution(context.Background(), int64(i+1), nil)
	}
	id := seedPlan(t, store, "approved", "main")
	deps := execDeps(t, store, &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/11/execute", `{}`))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429: %s", rec.Code, rec.Body.String())
	}
	plan, _ := store.GetPlan(context.Background(), id)
	if plan.Status != "approved" {
		t.Fatalf("rate-limited plan must stay approved, got %s", plan.Status)
	}
}

func TestExecExecute_RateLimitEnvOverride(t *testing.T) {
	t.Setenv("EXEC_RATE_LIMIT", "1")
	store := newMemExecStore()
	seedPlan(t, store, "approved", "main")
	_ = store.BeginExecution(context.Background(), 1, nil)
	seedPlan(t, store, "approved", "main")
	deps := execDeps(t, store, &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/2/execute", `{}`))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
}

// --- GET /exec/{exec_id} ---

func TestExecGet_ReturnsAuditTrail(t *testing.T) {
	store := newMemExecStore()
	seedPlan(t, store, "approved", "main")
	deps := execDeps(t, store, &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)

	// execute first so the audit trail exists
	exec := httptest.NewRecorder()
	router.ServeHTTP(exec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/1/execute", `{}`))
	if exec.Code != http.StatusOK {
		t.Fatalf("execute status = %d", exec.Code)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodGet,
		"/api/v1/exec/1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"exec_id", "plan_id", "status", "actions_total",
		"actions_completed", "started_at", "completed_at", "audit_trail"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("GET body missing %q: %v", key, body)
		}
	}
	if body["exec_id"] != body["plan_id"] {
		t.Fatalf("exec_id/plan_id must match: %v", body)
	}
	if body["status"] != "completed" {
		t.Fatalf("status = %v", body["status"])
	}
	trail := body["audit_trail"].([]any)
	if len(trail) != 1 {
		t.Fatalf("audit_trail = %v", trail)
	}
	row := trail[0].(map[string]any)
	for _, key := range []string{"seq", "action", "action_name", "target_resource",
		"pre_state", "post_state", "status", "error", "started_at", "completed_at"} {
		if _, ok := row[key]; !ok {
			t.Fatalf("audit row missing %q: %v", key, row)
		}
	}
}

func TestExecGet_NotFound_404(t *testing.T) {
	deps := execDeps(t, newMemExecStore(), &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodGet,
		"/api/v1/exec/5"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// --- full flow: planned → approved → running → completed ---

func TestExec_FullFlowAcrossEndpoints(t *testing.T) {
	store := newMemExecStore()
	store.alerts[7] = seededAlert{alert: map[string]any{"alert_id": "a-7"}, account: "main"}
	deps := execDeps(t, store, &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)
	authStore := deps.Auth

	plan := httptest.NewRecorder()
	router.ServeHTTP(plan, authedRequest(t, authStore, http.MethodPost,
		"/api/v1/exec/plan", `{"diagnosis_id":7}`))
	if plan.Code != http.StatusOK {
		t.Fatalf("plan status = %d: %s", plan.Code, plan.Body.String())
	}

	approve := httptest.NewRecorder()
	router.ServeHTTP(approve, authedRequest(t, authStore, http.MethodPost,
		"/api/v1/exec/approve", `{"plan_id":1,"approver_note":"go"}`))
	if approve.Code != http.StatusOK {
		t.Fatalf("approve status = %d: %s", approve.Code, approve.Body.String())
	}

	execute := httptest.NewRecorder()
	router.ServeHTTP(execute, authedRequest(t, authStore, http.MethodPost,
		"/api/v1/exec/1/execute", `{}`))
	if execute.Code != http.StatusOK {
		t.Fatalf("execute status = %d: %s", execute.Code, execute.Body.String())
	}

	get := httptest.NewRecorder()
	router.ServeHTTP(get, authedRequest(t, authStore, http.MethodGet, "/api/v1/exec/1"))
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d", get.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(get.Body.Bytes(), &body)
	if body["status"] != "completed" || body["actions_completed"].(float64) != 1 {
		t.Fatalf("final state = %v", body)
	}
}

// --- auth / CSRF (contract: session-required / csrf-required) ---

func TestExecRoutes_Unauthenticated_401(t *testing.T) {
	deps := execDeps(t, newMemExecStore(), &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)
	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/exec/plan"},
		{http.MethodPost, "/api/v1/exec/approve"},
		{http.MethodPost, "/api/v1/exec/1/execute"},
		{http.MethodGet, "/api/v1/exec/1"},
	} {
		req := httptest.NewRequest(route.method, route.path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want 401", route.method, route.path, rec.Code)
		}
	}
}

func TestExecRoutes_MissingCSRF_403(t *testing.T) {
	deps := execDeps(t, newMemExecStore(), &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)
	authStore := deps.Auth
	sess, _ := authStore.Issue("admin")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/exec/approve", strings.NewReader(`{"plan_id":1}`))
	req.AddCookie(&http.Cookie{Name: "aico_session", Value: sess.ID})
	req.Header.Set("Content-Type", "application/json") // no X-CSRF-Token
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// --- concurrency: double approve, only one wins (gotchas A6) ---

func TestExecApprove_ConcurrentDoubleApprove(t *testing.T) {
	store := newMemExecStore()
	seedPlan(t, store, "planned", "main")
	deps := execDeps(t, store, &stubPlanner{result: dryRunFixture()})
	router := newExecRouter(deps)
	authStore := deps.Auth

	const goroutines = 8
	var wg sync.WaitGroup
	codes := make(chan int, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, authedRequest(t, authStore, http.MethodPost,
				"/api/v1/exec/approve", `{"plan_id":1}`))
			codes <- rec.Code
		}()
	}
	wg.Wait()
	close(codes)

	var winners, conflicts int
	for code := range codes {
		switch code {
		case http.StatusOK:
			winners++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("unexpected status %d", code)
		}
	}
	if winners != 1 || conflicts != goroutines-1 {
		t.Fatalf("winners = %d, conflicts = %d; want 1 / %d", winners, conflicts, goroutines-1)
	}
	plan, _ := store.GetPlan(context.Background(), 1)
	if plan.Status != "approved" || plan.ApprovedBy == nil || *plan.ApprovedBy != "admin" {
		t.Fatalf("plan = %+v", plan)
	}
}

// --- nil wiring guard (regression: incidents_nil_pool_503 pattern) ---

func TestExecHandlers_NilStore_503(t *testing.T) {
	deps := &Deps{} // no ExecStore, no Planner
	r := chi.NewRouter()
	r.Post("/api/v1/exec/plan", execPlanHandler(deps))
	r.Post("/api/v1/exec/approve", execApproveHandler(deps))
	r.Post("/api/v1/exec/{exec_id}/execute", execExecuteHandler(deps))
	r.Get("/api/v1/exec/{exec_id}", execGetHandler(deps))

	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/exec/plan"},
		{http.MethodPost, "/api/v1/exec/approve"},
		{http.MethodPost, "/api/v1/exec/1/execute"},
		{http.MethodGet, "/api/v1/exec/1"},
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(route.method, route.path, strings.NewReader("{}")))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s status = %d, want 503", route.method, route.path, rec.Code)
		}
	}
}
