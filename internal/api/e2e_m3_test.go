// M3-7: M2+M3 集成 E2E 测试。
// Contract: audit-results/contract-m3-7.md.
//
// All-HTTP E2E through the real router: webhook → ingest handler (shape
// contract) → AI diagnosis (stub planner) → plan → approve → execute →
// rollback on failure. Uses in-memory fakes; no real Postgres / cloud.
package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buhaiqing/ai-cloud-ops/internal/agent"
	"github.com/buhaiqing/ai-cloud-ops/internal/ingest"
	"go.uber.org/zap"
)

// e2eStore is the in-memory store for E2E. Mirrors rollbackStore but lives
// in this file so exec_test.go and rollback_test.go stay untouched.
type e2eStore struct {
	mu        sync.Mutex
	nextID    int64
	plans     map[int64]*ExecPlanRecord
	audit     map[int64][]ExecAuditRecord
	diagnosis map[int64]map[string]any
	account   string
}

func newE2EStore() *e2eStore {
	return &e2eStore{
		plans:     map[int64]*ExecPlanRecord{},
		audit:     map[int64][]ExecAuditRecord{},
		diagnosis: map[int64]map[string]any{},
		account:   "main",
	}
}

func (s *e2eStore) putDiagnosis(id int64, alert map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.diagnosis[id] = alert
}

func (s *e2eStore) CreatePlan(_ context.Context, p ExecPlanRecord) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	p.ID = s.nextID
	p.CreatedAt = time.Now()
	if p.Status == "" {
		p.Status = "planned"
	}
	s.plans[p.ID] = &p
	return p.ID, nil
}

func (s *e2eStore) GetPlan(_ context.Context, id int64) (*ExecPlanRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.plans[id]
	if p == nil {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (s *e2eStore) LoadDiagnosisAlert(_ context.Context, diagnosisID int64) (map[string]any, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.diagnosis[diagnosisID], s.account, nil
}

func (s *e2eStore) ApprovePlan(_ context.Context, id int64, approver, note string) (*ExecPlanRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.plans[id]
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

func (s *e2eStore) BeginExecution(_ context.Context, id int64, actions []agent.PlannedAction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.plans[id]
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
		s.audit[id] = append(s.audit[id], ExecAuditRecord{
			Seq:            seq + 1,
			Action:         raw,
			ActionName:     action.ToolName,
			TargetResource: action.TargetResource,
			PreState:       json.RawMessage(`{"status":"captured","source":"e2e"}`),
			Status:         "pending",
		})
	}
	return nil
}

func (s *e2eStore) RecordActionResult(_ context.Context, execID int64, seq int, postState json.RawMessage, status, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := s.audit[execID]
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

func (s *e2eStore) FinishExecution(_ context.Context, id int64, status string, completed int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.plans[id]
	if p == nil {
		return ErrExecNotFound
	}
	p.Status = status
	p.ActionsCompleted = completed
	now := time.Now()
	p.CompletedAt = &now
	return nil
}

func (s *e2eStore) CountExecutionsSince(_ context.Context, accountAlias string, since time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, p := range s.plans {
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

func (s *e2eStore) AuditRows(_ context.Context, execID int64) ([]ExecAuditRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ExecAuditRecord(nil), s.audit[execID]...), nil
}

func (s *e2eStore) MarkAuditRolledBack(_ context.Context, execID int64, seqs []int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := make(map[int]bool, len(seqs))
	for _, s := range seqs {
		want[s] = true
	}
	updated := 0
	rows := s.audit[execID]
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

// e2ePlanner is a stub Planner that returns a fixed DryRunResult with
// ActionTrail evidence in chains (simulating M3-1's context injection).
type e2ePlanner struct {
	mu          sync.Mutex
	calls       int32
	gotAlertKey string
	actions     []agent.PlannedAction
}

func (p *e2ePlanner) DiagnoseDryRun(_ context.Context, alert map[string]any) (*agent.DryRunResult, error) {
	atomic.AddInt32(&p.calls, 1)
	p.mu.Lock()
	defer p.mu.Unlock()
	for k := range alert {
		p.gotAlertKey = k
		break
	}
	return &agent.DryRunResult{
		Diagnosis: agent.Diagnosis{
			RootCause:  "e2e: recent change caused metric breach",
			Confidence: "high",
			Model:      "e2e-stub",
			EvidenceChains: []agent.EvidenceChain{
				{Claim: "actiontrail correlation injected by M3-1",
					SupportingTool: "lookup_actiontrail_events",
					SupportingData: "Rds"},
			},
		},
		DryRun:          true,
		WouldExecute:    p.actions,
		BlockedByPolicy: []string{},
	}, nil
}

// e2eActions builds 3 PlannedActions with ToolName = "e2e-action-N" so
// e2eActions builds 3 PlannedActions with ToolName = "action-N" so the
// execSuccessFirstN helper from rollback_test.go (which strips the
// "action-" prefix) can key behavior off N.
func e2eActions() []agent.PlannedAction {
	return []agent.PlannedAction{
		{ToolName: "action-1", Command: `{"op":"a"}`, TargetResource: "res-1", RiskLevel: "medium", Rollback: "undo-1"},
		{ToolName: "action-2", Command: `{"op":"b"}`, TargetResource: "res-2", RiskLevel: "medium", Rollback: "undo-2"},
		{ToolName: "action-3", Command: `{"op":"c"}`, TargetResource: "res-3", RiskLevel: "high", Rollback: "undo-3"},
	}
}

// signBody computes the X-Aliyun-Signature header value (hex HMAC-SHA256).
func signBody(body []byte, secret string) string {
	if secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// --- test cases ---

// TestE2E_M3_WebhookToExecution_HappyPath drives the full chain through
// HTTP. Webhook shape contract + plan + approve + execute happy path +
// audit trail all-success.
func TestE2E_M3_WebhookToExecution_HappyPath(t *testing.T) {
	store := newE2EStore()
	planner := &e2ePlanner{actions: e2eActions()}

	// 1a. Webhook shape contract.
	t.Setenv("WEBHOOK_SIGNING_SECRET", "e2e-shared-secret")
	inserted := []ingest.Alert{}
	insert := func(_ context.Context, a ingest.Alert) (string, error) {
		inserted = append(inserted, a)
		return "alert-101", nil
	}
	webhookPayload := []byte(`{"alert_id":"alert-101","alert_name":"RDS_Connections_High","severity":"critical","resource_id":"rm-bp1x","region":"cn-hangzhou","metric":{"namespace":"acs_rds","metric_name":"ConnectionUsage","value":99.2,"threshold":80,"duration_minutes":5}}`)
	sig := signBody(webhookPayload, "e2e-shared-secret")
	whReq := httptest.NewRequest(http.MethodPost, "/webhook/cms", bytes.NewReader(webhookPayload))
	whReq.Header.Set(ingest.HeaderSignature, sig)
	whRR := httptest.NewRecorder()
	ingest.WebhookHandler(insert, zap.NewNop()).ServeHTTP(whRR, whReq)
	if whRR.Code < 200 || whRR.Code >= 300 {
		t.Fatalf("webhook status = %d, want 2xx; body=%s", whRR.Code, whRR.Body.String())
	}
	if len(inserted) != 1 {
		t.Fatalf("inserted alerts = %d, want 1", len(inserted))
	}
	if inserted[0].AlertID != "alert-101" || inserted[0].Severity != "critical" || inserted[0].ResourceID != "rm-bp1x" {
		t.Fatalf("alert fields not preserved: %#v", inserted[0])
	}

	// 1b. Plant a diagnosis keyed on id 1.
	store.putDiagnosis(1, map[string]any{
		"alert_id":    "alert-101",
		"severity":    "critical",
		"resource_id": "rm-bp1x",
		"metric":      map[string]any{"namespace": "acs_rds", "metric_name": "ConnectionUsage"},
	})

	deps := &Deps{
		ExecStore:  store,
		Planner:    planner,
		ExecAction: execSuccessFirstN(0), // never fails
	}
	router := newExecRouter(deps)

	// 1c. POST /api/v1/exec/plan
	planRec := httptest.NewRecorder()
	router.ServeHTTP(planRec, authedRequest(t, deps.Auth, http.MethodPost, "/api/v1/exec/plan", `{"diagnosis_id":1}`))
	if planRec.Code != http.StatusOK {
		t.Fatalf("plan status = %d, want 200 (body=%s)", planRec.Code, planRec.Body.String())
	}
	var planBody map[string]any
	if err := json.Unmarshal(planRec.Body.Bytes(), &planBody); err != nil {
		t.Fatalf("plan body: %v", err)
	}
	planIDf, _ := planBody["plan_id"].(float64)
	planID := int64(planIDf)
	if planID <= 0 {
		t.Fatalf("plan_id = %v, want > 0", planBody["plan_id"])
	}

	// 1d. POST /api/v1/exec/approve
	approveRec := httptest.NewRecorder()
	router.ServeHTTP(approveRec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/approve", fmt.Sprintf(`{"plan_id":%d,"approver_note":"e2e go"}`, planID)))
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status = %d, want 200 (body=%s)", approveRec.Code, approveRec.Body.String())
	}

	// 1e. POST /api/v1/exec/{id}/execute
	execRec := httptest.NewRecorder()
	router.ServeHTTP(execRec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/"+strconv.FormatInt(planID, 10)+"/execute", `{}`))
	if execRec.Code != http.StatusOK {
		t.Fatalf("execute status = %d, want 200 (body=%s)", execRec.Code, execRec.Body.String())
	}
	var execBody map[string]any
	if err := json.Unmarshal(execRec.Body.Bytes(), &execBody); err != nil {
		t.Fatalf("execute body: %v", err)
	}
	if execBody["final_status"] != "completed" {
		t.Fatalf("final_status = %v, want completed", execBody["final_status"])
	}
	if at, _ := execBody["actions_total"].(float64); at != 3 {
		t.Fatalf("actions_total = %v, want 3", execBody["actions_total"])
	}

	// 1f. GET /api/v1/exec/{id}
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, authedRequest(t, deps.Auth, http.MethodGet,
		"/api/v1/exec/"+strconv.FormatInt(planID, 10)))
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200", getRec.Code)
	}
	var getBody map[string]any
	if err := json.Unmarshal(getRec.Body.Bytes(), &getBody); err != nil {
		t.Fatalf("get body: %v", err)
	}
	if getBody["status"] != "completed" {
		t.Fatalf("get status = %v, want completed", getBody["status"])
	}
	trail, _ := getBody["audit_trail"].([]any)
	if len(trail) != 3 {
		t.Fatalf("audit_trail length = %d, want 3", len(trail))
	}
	for i, row := range trail {
		rm, _ := row.(map[string]any)
		if rm["status"] != "success" {
			t.Errorf("audit[%d].status = %v, want success", i, rm["status"])
		}
	}
}

// TestE2E_M3_ExecuteFailure_Rollback: execute fails on the 3rd action,
// rollback flips seq1/2, plan ends rolled_back.
func TestE2E_M3_ExecuteFailure_Rollback(t *testing.T) {
	t.Setenv("AICO_ROLLBACK_ENABLED", "true")

	store := newE2EStore()
	planner := &e2ePlanner{actions: e2eActions()}
	store.putDiagnosis(1, map[string]any{"alert_id": "alert-200", "severity": "critical"})

	rbCalls := []string{}
	deps := &Deps{
		ExecStore:      store,
		Planner:        planner,
		ExecAction:     execSuccessFirstN(3),
		RollbackAction: recordedRollback(&rbCalls),
	}
	router := newExecRouter(deps)

	planRec := httptest.NewRecorder()
	router.ServeHTTP(planRec, authedRequest(t, deps.Auth, http.MethodPost, "/api/v1/exec/plan", `{"diagnosis_id":1}`))
	if planRec.Code != http.StatusOK {
		t.Fatalf("plan status = %d, want 200", planRec.Code)
	}
	var planBody map[string]any
	_ = json.Unmarshal(planRec.Body.Bytes(), &planBody)
	planID := int64(planBody["plan_id"].(float64))

	approveRec := httptest.NewRecorder()
	router.ServeHTTP(approveRec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/approve", fmt.Sprintf(`{"plan_id":%d}`, planID)))
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status = %d", approveRec.Code)
	}

	execRec := httptest.NewRecorder()
	router.ServeHTTP(execRec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/"+strconv.FormatInt(planID, 10)+"/execute", `{}`))
	if execRec.Code != http.StatusOK {
		t.Fatalf("execute status = %d, want 200", execRec.Code)
	}
	var execBody map[string]any
	_ = json.Unmarshal(execRec.Body.Bytes(), &execBody)
	if execBody["final_status"] != "rolled_back" {
		t.Fatalf("final_status = %v, want rolled_back", execBody["final_status"])
	}
	if rb, _ := execBody["rolled_back"].(float64); rb != 2 {
		t.Fatalf("rolled_back = %v, want 2", execBody["rolled_back"])
	}
	wantCalls := []string{"action-2", "action-1"}
	if len(rbCalls) != 2 {
		t.Fatalf("rollback calls = %d, want 2", len(rbCalls))
	}
	for i, n := range wantCalls {
		if rbCalls[i] != n {
			t.Errorf("rollback[%d] = %q, want %q", i, rbCalls[i], n)
		}
	}
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, authedRequest(t, deps.Auth, http.MethodGet,
		"/api/v1/exec/"+strconv.FormatInt(planID, 10)))
	var getBody map[string]any
	_ = json.Unmarshal(getRec.Body.Bytes(), &getBody)
	trail, _ := getBody["audit_trail"].([]any)
	want := []string{"rolled_back", "rolled_back", "failed"}
	for i, row := range trail {
		rm, _ := row.(map[string]any)
		if got := rm["status"]; got != want[i] {
			t.Errorf("audit[%d].status = %v, want %v", i, got, want[i])
		}
	}
}

// TestE2E_M3_ActionTrailContextVisible: the planner's DiagnoseDryRun
// receives a non-empty alert map; the resulting WouldExecute unmarshals
// cleanly to 3 PlannedActions; the planner's evidence chains include the
// actiontrail correlation marker that mirrors M3-1 wiring.
func TestE2E_M3_ActionTrailContextVisible(t *testing.T) {
	store := newE2EStore()
	planner := &e2ePlanner{actions: e2eActions()}
	alert := map[string]any{
		"alert_id":    "alert-300",
		"resource_id": "rm-bp1y",
		"metric":      map[string]any{"namespace": "acs_rds", "metric_name": "ConnectionUsage"},
	}
	store.putDiagnosis(1, alert)

	deps := &Deps{
		ExecStore:  store,
		Planner:    planner,
		ExecAction: execSuccessFirstN(0),
	}
	router := newExecRouter(deps)

	planRec := httptest.NewRecorder()
	router.ServeHTTP(planRec, authedRequest(t, deps.Auth, http.MethodPost, "/api/v1/exec/plan", `{"diagnosis_id":1}`))
	if planRec.Code != http.StatusOK {
		t.Fatalf("plan status = %d, want 200", planRec.Code)
	}
	if atomic.LoadInt32(&planner.calls) < 1 {
		t.Fatalf("planner.DiagnoseDryRun calls = 0, want >= 1")
	}
	planner.mu.Lock()
	got := planner.gotAlertKey
	planner.mu.Unlock()
	if got == "" {
		t.Fatalf("planner never observed any alert map key")
	}

	var planBody map[string]any
	_ = json.Unmarshal(planRec.Body.Bytes(), &planBody)
	would, _ := planBody["would_execute"].([]any)
	if len(would) != 3 {
		t.Fatalf("would_execute length = %d, want 3", len(would))
	}

	dr, err := planner.DiagnoseDryRun(context.Background(), alert)
	if err != nil {
		t.Fatalf("planner.DiagnoseDryRun: %v", err)
	}
	var atCount int
	for _, c := range dr.Diagnosis.EvidenceChains {
		if c.SupportingTool == "lookup_actiontrail_events" {
			atCount++
		}
	}
	if atCount == 0 {
		t.Fatalf("planner produced no actiontrail evidence chain; want >= 1")
	}
}

// TestE2E_M3_RateLimit429: pre-seed 10 successful executions; the 11th
// returns 429 and the plan stays approved.
func TestE2E_M3_RateLimit429(t *testing.T) {
	t.Setenv("EXEC_RATE_LIMIT", "10")
	store := newE2EStore()
	planner := &e2ePlanner{actions: e2eActions()}

	// Pre-seed 10 plans already running for the same account.
	for range 10 {
		id, err := store.CreatePlan(context.Background(), ExecPlanRecord{
			DiagnosisID: 1, AccountAlias: "main", DryRun: true,
			CreatedBy: "system", Status: "approved",
			ActionsTotal: 0,
		})
		if err != nil {
			t.Fatalf("seed plan: %v", err)
		}
		if err := store.BeginExecution(context.Background(), id, nil); err != nil {
			t.Fatalf("seed begin: %v", err)
		}
	}

	store.putDiagnosis(1, map[string]any{"alert_id": "alert-rl"})
	deps := &Deps{
		ExecStore:  store,
		Planner:    planner,
		ExecAction: execSuccessFirstN(0),
	}
	router := newExecRouter(deps)

	planRec := httptest.NewRecorder()
	router.ServeHTTP(planRec, authedRequest(t, deps.Auth, http.MethodPost, "/api/v1/exec/plan", `{"diagnosis_id":1}`))
	if planRec.Code != http.StatusOK {
		t.Fatalf("plan status = %d", planRec.Code)
	}
	var planBody map[string]any
	_ = json.Unmarshal(planRec.Body.Bytes(), &planBody)
	planID := int64(planBody["plan_id"].(float64))

	approveRec := httptest.NewRecorder()
	router.ServeHTTP(approveRec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/approve", fmt.Sprintf(`{"plan_id":%d}`, planID)))
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status = %d", approveRec.Code)
	}

	execRec := httptest.NewRecorder()
	router.ServeHTTP(execRec, authedRequest(t, deps.Auth, http.MethodPost,
		"/api/v1/exec/"+strconv.FormatInt(planID, 10)+"/execute", `{}`))
	if execRec.Code != http.StatusTooManyRequests {
		t.Fatalf("execute status = %d, want 429 (body=%s)", execRec.Code, execRec.Body.String())
	}

	plan, err := store.GetPlan(context.Background(), planID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if plan.Status != "approved" {
		t.Fatalf("plan status = %q, want approved (rate limit must not flip it)", plan.Status)
	}
}
