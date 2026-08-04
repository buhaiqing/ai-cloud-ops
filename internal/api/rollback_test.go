// M3-6 tests: automatic rollback of completed actions when execute fails.
// Contract: audit-results/contract-m3-6.md (v1.1, opt-in marker interface).
//
// Uses its own in-memory fake (rollbackStore), independent of exec_test.go's
// memExecStore, so that file stays untouched. rollbackStore implements both
// ExecStore and ExecRollbackMarker; plainExecStore hides the marker surface
// for the store-without-marker case.
//
// Reuses newExecRouter + authedRequest from exec_test.go (same package).
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buhaiqing/ai-cloud-ops/internal/agent"
)

// --- in-memory rollback-aware ExecStore fake ---

type rollbackStore struct {
	mu     sync.Mutex
	nextID int64
	plans  map[int64]*ExecPlanRecord
	audit  map[int64][]ExecAuditRecord
}

func newRollbackStore() *rollbackStore {
	return &rollbackStore{
		plans: map[int64]*ExecPlanRecord{},
		audit: map[int64][]ExecAuditRecord{},
	}
}

func (m *rollbackStore) CreatePlan(_ context.Context, p ExecPlanRecord) (int64, error) {
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

func (m *rollbackStore) GetPlan(_ context.Context, id int64) (*ExecPlanRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.plans[id]
	if p == nil {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (m *rollbackStore) LoadDiagnosisAlert(_ context.Context, _ int64) (map[string]any, string, error) {
	return nil, "", nil
}

func (m *rollbackStore) ApprovePlan(_ context.Context, id int64, approver, note string) (*ExecPlanRecord, error) {
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

func (m *rollbackStore) BeginExecution(_ context.Context, id int64, actions []agent.PlannedAction) error {
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

func (m *rollbackStore) RecordActionResult(_ context.Context, execID int64, seq int, postState json.RawMessage, status, errMsg string) error {
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

func (m *rollbackStore) FinishExecution(_ context.Context, id int64, status string, completed int) error {
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

func (m *rollbackStore) CountExecutionsSince(_ context.Context, accountAlias string, since time.Time) (int, error) {
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

func (m *rollbackStore) AuditRows(_ context.Context, execID int64) ([]ExecAuditRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ExecAuditRecord(nil), m.audit[execID]...), nil
}

// MarkAuditRolledBack mirrors pgxExecStore: only rows currently 'success'
// are flipped; failed rows (the one that broke the run) are left alone.
func (m *rollbackStore) MarkAuditRolledBack(_ context.Context, execID int64, seqs []int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	want := make(map[int]bool, len(seqs))
	for _, s := range seqs {
		want[s] = true
	}
	updated := 0
	rows := m.audit[execID]
	for i := range rows {
		if want[rows[i].Seq] && rows[i].Status == "success" {
			rows[i].Status = "rolled_back"
			now := time.Now()
			rows[i].CompletedAt = &now
			updated++
		}
	}
	return updated, nil
}

// plainExecStore re-exposes only the base ExecStore surface of an inner
// store, hiding optional interfaces: deps.ExecStore.(ExecRollbackMarker)
// must fail against it.
type plainExecStore struct{ inner ExecStore }

func (p *plainExecStore) CreatePlan(ctx context.Context, rec ExecPlanRecord) (int64, error) {
	return p.inner.CreatePlan(ctx, rec)
}
func (p *plainExecStore) GetPlan(ctx context.Context, id int64) (*ExecPlanRecord, error) {
	return p.inner.GetPlan(ctx, id)
}
func (p *plainExecStore) LoadDiagnosisAlert(ctx context.Context, diagnosisID int64) (map[string]any, string, error) {
	return p.inner.LoadDiagnosisAlert(ctx, diagnosisID)
}
func (p *plainExecStore) ApprovePlan(ctx context.Context, id int64, approver, note string) (*ExecPlanRecord, error) {
	return p.inner.ApprovePlan(ctx, id, approver, note)
}
func (p *plainExecStore) BeginExecution(ctx context.Context, id int64, actions []agent.PlannedAction) error {
	return p.inner.BeginExecution(ctx, id, actions)
}
func (p *plainExecStore) RecordActionResult(ctx context.Context, execID int64, seq int, postState json.RawMessage, status, errMsg string) error {
	return p.inner.RecordActionResult(ctx, execID, seq, postState, status, errMsg)
}
func (p *plainExecStore) FinishExecution(ctx context.Context, id int64, status string, completed int) error {
	return p.inner.FinishExecution(ctx, id, status, completed)
}
func (p *plainExecStore) CountExecutionsSince(ctx context.Context, accountAlias string, since time.Time) (int, error) {
	return p.inner.CountExecutionsSince(ctx, accountAlias, since)
}
func (p *plainExecStore) AuditRows(ctx context.Context, execID int64) ([]ExecAuditRecord, error) {
	return p.inner.AuditRows(ctx, execID)
}

// --- helpers ---

// rbActions builds n PlannedActions named action-1..action-n so ExecAction
// and RollbackAction can key behavior off the action identity (ExecAction
// is called twice per action: once for pre-state capture, once for execution).
func rbActions(n int) []agent.PlannedAction {
	actions := make([]agent.PlannedAction, n)
	for i := range actions {
		actions[i] = agent.PlannedAction{
			ToolName:       "action-" + strconv.Itoa(i+1),
			Command:        `{"op":"noop"}`,
			TargetResource: "res-" + strconv.Itoa(i+1),
			RiskLevel:      "medium",
			Rollback:       "undo",
		}
	}
	return actions
}

func seedRollbackPlan(t *testing.T, store *rollbackStore, actions []agent.PlannedAction) int64 {
	t.Helper()
	raw, err := json.Marshal(actions)
	if err != nil {
		t.Fatalf("marshal actions: %v", err)
	}
	id, err := store.CreatePlan(context.Background(), ExecPlanRecord{
		DiagnosisID:  1,
		AccountAlias: "main",
		DryRun:       true,
		WouldExecute: raw,
		CreatedBy:    "system",
		ActionsTotal: len(actions),
	})
	if err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	if _, err := store.ApprovePlan(context.Background(), id, "admin", "rollback seed"); err != nil {
		t.Fatalf("seed approve: %v", err)
	}
	return id
}

// execSuccessFirstN succeeds for the first n actions and fails on the
// (n+1)-th. Each invocation is thread-safe.
func execSuccessFirstN(failAt int) func(ctx context.Context, a agent.PlannedAction) (json.RawMessage, string, bool) {
	var mu sync.Mutex
	return func(_ context.Context, a agent.PlannedAction) (json.RawMessage, string, bool) {
		mu.Lock()
		defer mu.Unlock()
		seq, _ := strconv.Atoi(strings.TrimPrefix(a.ToolName, "action-"))
		if seq == failAt {
			return json.RawMessage(`{"status":"failed"}`), "simulated cloud error", false
		}
		body, _ := json.Marshal(map[string]any{"status": "ok", "tool": a.ToolName, "seq": seq})
		return body, "", true
	}
}

// recordedRollback returns a rollback closure that always succeeds and
// records the order of action names in *out (caller-owned).
func recordedRollback(out *[]string) func(ctx context.Context, a agent.PlannedAction, pre json.RawMessage) (json.RawMessage, string, bool) {
	return func(_ context.Context, a agent.PlannedAction, _ json.RawMessage) (json.RawMessage, string, bool) {
		*out = append(*out, a.ToolName)
		body, _ := json.Marshal(map[string]any{"rolled_back": a.ToolName})
		return body, "", true
	}
}

// failingRollback fails for any action whose tool name matches failFor.
func failingRollback(failFor string) func(ctx context.Context, a agent.PlannedAction, pre json.RawMessage) (json.RawMessage, string, bool) {
	return func(_ context.Context, a agent.PlannedAction, _ json.RawMessage) (json.RawMessage, string, bool) {
		if a.ToolName == failFor {
			return nil, "rollback failed for " + a.ToolName, false
		}
		body, _ := json.Marshal(map[string]any{"rolled_back": a.ToolName})
		return body, "", true
	}
}

// executeRollbackPlan drives the /api/v1/exec/{id}/execute endpoint with
// the given deps and returns the response. Uses the same package's
// newExecRouter + authedRequest (reuses the test auth setup from exec_test.go).
func executeRollbackPlan(t *testing.T, deps *Deps, id int64) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	router := newExecRouter(deps)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/"+strconv.FormatInt(id, 10)+"/execute", `{}`))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode execute body: %v (raw=%s)", err, rec.Body.String())
	}
	return rec, body
}

func getRollbackPlan(t *testing.T, deps *Deps, id int64) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	router := newExecRouter(deps)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, deps.Auth, http.MethodGet,
		"/api/v1/exec/"+strconv.FormatInt(id, 10)))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode get body: %v (raw=%s)", err, rec.Body.String())
	}
	return rec, body
}

// --- contract test cases ---

// TestExecExecute_RollbackOnFailure: 3 actions, 3rd fails, all rollbacks
// succeed → plan ends in 'rolled_back', audit seq1/2 = rolled_back,
// seq3 = failed, response rolled_back = 2.
func TestExecExecute_RollbackOnFailure(t *testing.T) {
	t.Setenv("AICO_ROLLBACK_ENABLED", "true")

	store := newRollbackStore()
	actions := rbActions(3)
	id := seedRollbackPlan(t, store, actions)

	exec := execSuccessFirstN(3)
	calls := []string{}
	rb := recordedRollback(&calls)
	deps := &Deps{ExecStore: store, ExecAction: exec, RollbackAction: rb}

	rr, body := executeRollbackPlan(t, deps, id)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	if body["final_status"] != "rolled_back" {
		t.Fatalf("final_status = %v, want rolled_back", body["final_status"])
	}
	if rbCount, _ := body["rolled_back"].(float64); rbCount != 2 {
		t.Fatalf("rolled_back = %v, want 2", body["rolled_back"])
	}

	_, getBody := getRollbackPlan(t, deps, id)
	trail, _ := getBody["audit_trail"].([]any)
	if len(trail) != 3 {
		t.Fatalf("audit_trail length = %d, want 3", len(trail))
	}
	want := []string{"rolled_back", "rolled_back", "failed"}
	for i, row := range trail {
		rowMap, _ := row.(map[string]any)
		if got := rowMap["status"]; got != want[i] {
			t.Errorf("audit[%d].status = %v, want %v", i, got, want[i])
		}
	}
}

// TestExecExecute_RollbackDisabledByDefault: rollback env unset, failure
// leaves plan in 'failed' and audit rows in 'success' (current behavior).
func TestExecExecute_RollbackDisabledByDefault(t *testing.T) {
	// explicitly unset the env even if a previous test set it
	t.Setenv("AICO_ROLLBACK_ENABLED", "")

	store := newRollbackStore()
	actions := rbActions(3)
	id := seedRollbackPlan(t, store, actions)

	exec := execSuccessFirstN(3)
	// A rollback fn is provided but env is off → must not be called.
	calls := []string{}
	rb := recordedRollback(&calls)
	deps := &Deps{ExecStore: store, ExecAction: exec, RollbackAction: rb}

	rr, body := executeRollbackPlan(t, deps, id)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if body["final_status"] != "failed" {
		t.Fatalf("final_status = %v, want failed (rollback disabled)", body["final_status"])
	}
	if len(calls) != 0 {
		t.Fatalf("rollback called %d times with env unset, want 0", len(calls))
	}
	_, getBody := getRollbackPlan(t, deps, id)
	trail, _ := getBody["audit_trail"].([]any)
	for i, row := range trail {
		rm, _ := row.(map[string]any)
		want := "success"
		if i == 2 {
			want = "failed"
		}
		if got := rm["status"]; got != want {
			t.Errorf("audit[%d].status = %v, want %v", i, got, want)
		}
	}
}

// TestExecExecute_RollbackPartialFailure: 2 success, 1 failure, the
// rollback for seq1 fails → final_status = 'failed', seq2 = rolled_back,
// seq1 = success (failed rollback is not retroactively flipped).
func TestExecExecute_RollbackPartialFailure(t *testing.T) {
	t.Setenv("AICO_ROLLBACK_ENABLED", "true")

	store := newRollbackStore()
	actions := rbActions(3)
	id := seedRollbackPlan(t, store, actions)

	exec := execSuccessFirstN(3)      // fails on seq3
	rb := failingRollback("action-1") // seq1 rollback fails, seq2 succeeds
	deps := &Deps{ExecStore: store, ExecAction: exec, RollbackAction: rb}

	rr, body := executeRollbackPlan(t, deps, id)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if body["final_status"] != "failed" {
		t.Fatalf("final_status = %v, want failed (partial rollback)", body["final_status"])
	}
	if rbCount, _ := body["rolled_back"].(float64); rbCount != 1 {
		t.Fatalf("rolled_back = %v, want 1", body["rolled_back"])
	}

	_, getBody := getRollbackPlan(t, deps, id)
	trail, _ := getBody["audit_trail"].([]any)
	want := []string{"success", "rolled_back", "failed"} // seq1 unchanged, seq2 flipped, seq3 stays failed
	for i, row := range trail {
		rm, _ := row.(map[string]any)
		if got := rm["status"]; got != want[i] {
			t.Errorf("audit[%d].status = %v, want %v", i, got, want[i])
		}
	}
}

// TestExecExecute_RollbackReverseOrder: rollback is called in reverse
// order of execution, i.e. [seq2, seq1].
func TestExecExecute_RollbackReverseOrder(t *testing.T) {
	t.Setenv("AICO_ROLLBACK_ENABLED", "true")

	store := newRollbackStore()
	actions := rbActions(3)
	id := seedRollbackPlan(t, store, actions)

	exec := execSuccessFirstN(3) // fails on seq3
	calls := []string{}
	rb := recordedRollback(&calls)
	deps := &Deps{ExecStore: store, ExecAction: exec, RollbackAction: rb}

	rr, _ := executeRollbackPlan(t, deps, id)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	want := []string{"action-2", "action-1"}
	if len(calls) != 2 {
		t.Fatalf("rollback calls = %d, want 2", len(calls))
	}
	for i, n := range want {
		if calls[i] != n {
			t.Errorf("rollback[%d] = %q, want %q", i, calls[i], n)
		}
	}
}

// TestExecExecute_NilPreStateCapture: deps.ExecAction is nil, so the
// handler takes the defaultStubExecutor path. Pre-state becomes nil
// (no panic) and the plan ends 'completed' (default stub never fails).
func TestExecExecute_NilPreStateCapture(t *testing.T) {
	t.Setenv("AICO_ROLLBACK_ENABLED", "true")

	store := newRollbackStore()
	actions := rbActions(3)
	id := seedRollbackPlan(t, store, actions)

	// rollback closure verifies pre-state is nil when ExecAction is nil
	preWas := []bool{}
	rb := func(_ context.Context, a agent.PlannedAction, pre json.RawMessage) (json.RawMessage, string, bool) {
		preWas = append(preWas, pre == nil)
		body, _ := json.Marshal(map[string]any{"rolled_back": a.ToolName})
		return body, "", true
	}

	deps := &Deps{
		ExecStore:      store,
		ExecAction:     nil, // forces defaultStubExecutor
		RollbackAction: rb,
	}
	rr, body := executeRollbackPlan(t, deps, id)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if body["final_status"] != "completed" {
		t.Fatalf("final_status = %v, want completed (default stub never fails)", body["final_status"])
	}
	if len(preWas) != 0 {
		// The default stub never fails, so rollback is not triggered;
		// this asserts the no-fail path with nil ExecAction doesn't crash.
		t.Fatalf("rollback called %d times, want 0 (no failure)", len(preWas))
	}
}

// TestExecExecute_StoreWithoutMarker: a store that does NOT implement
// ExecRollbackMarker → rollback still runs, plan ends in 'rolled_back',
// audit rows stay 'success' (only the marker can flip them).
func TestExecExecute_StoreWithoutMarker(t *testing.T) {
	t.Setenv("AICO_ROLLBACK_ENABLED", "true")

	raw := newRollbackStore() // implements the marker
	plain := &plainExecStore{inner: raw}

	actions := rbActions(3)
	rawActions, _ := json.Marshal(actions)
	id, err := raw.CreatePlan(context.Background(), ExecPlanRecord{
		DiagnosisID: 1, AccountAlias: "main", DryRun: true,
		WouldExecute: rawActions, CreatedBy: "system", ActionsTotal: len(actions),
	})
	if err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	if _, err := raw.ApprovePlan(context.Background(), id, "admin", ""); err != nil {
		t.Fatalf("seed approve: %v", err)
	}

	exec := execSuccessFirstN(3) // fails on seq3
	calls := []string{}
	rb := recordedRollback(&calls)
	deps := &Deps{ExecStore: plain, ExecAction: exec, RollbackAction: rb}

	rr, body := executeRollbackPlan(t, deps, id)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if body["final_status"] != "rolled_back" {
		t.Fatalf("final_status = %v, want rolled_back (rollback fn succeeded)", body["final_status"])
	}
	if len(calls) != 2 {
		t.Fatalf("rollback calls = %d, want 2", len(calls))
	}

	// Audit rows stay 'success' because plainExecStore hides the marker.
	rows, err := raw.AuditRows(context.Background(), id)
	if err != nil {
		t.Fatalf("audit rows: %v", err)
	}
	for _, r := range rows {
		switch r.Seq {
		case 1, 2:
			if r.Status != "success" {
				t.Errorf("audit seq%d status = %q, want success (no marker)", r.Seq, r.Status)
			}
		case 3:
			if r.Status != "failed" {
				t.Errorf("audit seq3 status = %q, want failed", r.Status)
			}
		}
	}
}
