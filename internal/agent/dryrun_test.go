// M3-2 + M3-3: structured Recommendations + DiagnoseDryRun.
// Contract: audit-results/contract-m3-agent.md.

package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// toolUseNamed builds a tool_use response with an arbitrary tool name + input.
func toolUseNamed(id, name string, input map[string]any) *anthropic.Message {
	blockJSON, _ := json.Marshal(map[string]any{
		"type":  "tool_use",
		"id":    id,
		"name":  name,
		"input": input,
	})
	var block anthropic.ContentBlockUnion
	if err := json.Unmarshal(blockJSON, &block); err != nil {
		panic(err)
	}
	return &anthropic.Message{Content: []anthropic.ContentBlockUnion{block}}
}

// diagnosisJSONWithRecs builds a final diagnosis with the given recommendations.
func diagnosisJSONWithRecs(recs []map[string]any) string {
	payload, _ := json.Marshal(map[string]any{
		"root_cause":      "RDS connection pool exhausted",
		"recommendations": recs,
		"confidence":      "high",
		"caveats":         []string{},
	})
	return string(payload)
}

func writeRec(downtime int, risk, rollback string) map[string]any {
	return map[string]any{
		"action":               "restart rds",
		"command":              "aliyun rds RestartDBInstance",
		"expected_outcome":     "connections reset",
		"preconditions":        []string{"maintenance window open"},
		"rollback_command":     rollback,
		"risk_level":           risk,
		"estimated_downtime_s": downtime,
	}
}

// --- Contract tdd_target: empty Recommendations ---

func TestDiagnoseDryRun_EmptyRecommendations(t *testing.T) {
	mock := &mockMessageClient{responses: []*anthropic.Message{
		textResponse(diagnosisJSONWithRecs([]map[string]any{})),
	}}
	res, err := testClient(mock).DiagnoseDryRun(context.Background(), map[string]any{"alert_id": "d-1"})
	if err != nil {
		t.Fatalf("DiagnoseDryRun() error = %v", err)
	}
	if !res.DryRun {
		t.Fatal("DryRun flag must be true")
	}
	if len(res.WouldExecute) != 0 {
		t.Fatalf("WouldExecute = %+v, want empty", res.WouldExecute)
	}
	if len(res.BlockedByPolicy) != 0 {
		t.Fatalf("BlockedByPolicy = %+v, want empty", res.BlockedByPolicy)
	}
	if res.EstimatedTotalDowntimeS != 0 {
		t.Fatalf("EstimatedTotalDowntimeS = %d, want 0", res.EstimatedTotalDowntimeS)
	}
}

// --- Contract tdd_target: all-ReadOnly actions (no PlannedAction) ---

func TestDiagnoseDryRun_ReadOnlyToolsProduceNoPlannedAction(t *testing.T) {
	mock := &mockMessageClient{responses: []*anthropic.Message{
		toolUseNamed("toolu-r1", "describe_ecs_instances", map[string]any{"region": "cn-hangzhou"}),
		textResponse(diagnosisJSONWithRecs([]map[string]any{
			{"action": "watch metrics", "command": "", "expected_outcome": "cpu normalizes"},
		})),
	}}
	res, err := testClient(mock).DiagnoseDryRun(context.Background(), map[string]any{"alert_id": "d-2"})
	if err != nil {
		t.Fatalf("DiagnoseDryRun() error = %v", err)
	}
	if len(res.WouldExecute) != 0 {
		t.Fatalf("read-only tool calls must not produce PlannedActions, got %+v", res.WouldExecute)
	}
	if len(res.BlockedByPolicy) != 0 {
		t.Fatalf("read-only tools must not be blocked, got %+v", res.BlockedByPolicy)
	}
}

// --- Contract tdd_target: mixed Read/Write ---

func TestDiagnoseDryRun_MixedReadWriteInterceptsWriteOnly(t *testing.T) {
	mock := &mockMessageClient{responses: []*anthropic.Message{
		toolUseNamed("toolu-r1", "describe_rds_instances", map[string]any{"region": "cn-hangzhou"}),
		toolUseNamed("toolu-w1", "restart_rds_instance",
			map[string]any{"region": "cn-hangzhou", "resource_id": "rm-abc123"}),
		textResponse(diagnosisJSONWithRecs([]map[string]any{writeRec(45, "medium", "n/a")})),
	}}
	res, err := testClient(mock).DiagnoseDryRun(context.Background(), map[string]any{"alert_id": "d-3"})
	if err != nil {
		t.Fatalf("DiagnoseDryRun() error = %v", err)
	}
	if len(res.WouldExecute) != 1 {
		t.Fatalf("WouldExecute len = %d, want 1: %+v", len(res.WouldExecute), res.WouldExecute)
	}
	pa := res.WouldExecute[0]
	if pa.ToolName != "restart_rds_instance" {
		t.Fatalf("ToolName = %q", pa.ToolName)
	}
	if pa.TargetResource != "rm-abc123" {
		t.Fatalf("TargetResource = %q, want rm-abc123", pa.TargetResource)
	}
	if pa.RiskLevel != "medium" {
		t.Fatalf("RiskLevel = %q, want medium (from WRITE_TOOLS metadata)", pa.RiskLevel)
	}
	if !strings.Contains(pa.Command, "rm-abc123") {
		t.Fatalf("Command should carry the tool input, got %q", pa.Command)
	}
}

// --- Contract tdd_target: blocked tool ---

func TestDiagnoseDryRun_NonWhitelistedToolBlockedByPolicy(t *testing.T) {
	mock := &mockMessageClient{responses: []*anthropic.Message{
		toolUseNamed("toolu-b1", "delete_ecs_instance", map[string]any{"region": "cn-hangzhou"}),
		textResponse(diagnosisJSONWithRecs([]map[string]any{})),
	}}
	res, err := testClient(mock).DiagnoseDryRun(context.Background(), map[string]any{"alert_id": "d-4"})
	if err != nil {
		t.Fatalf("DiagnoseDryRun() error = %v", err)
	}
	if len(res.WouldExecute) != 0 {
		t.Fatalf("blocked tool must not become a PlannedAction: %+v", res.WouldExecute)
	}
	if len(res.BlockedByPolicy) != 1 {
		t.Fatalf("BlockedByPolicy = %+v, want 1 entry", res.BlockedByPolicy)
	}
	if !strings.Contains(res.BlockedByPolicy[0], "delete_ecs_instance") ||
		!strings.Contains(res.BlockedByPolicy[0], "WRITE_TOOLS") {
		t.Fatalf("blocked entry should name the tool + whitelist: %q", res.BlockedByPolicy[0])
	}
}

func TestDiagnoseDryRun_BlockedEntriesDeduplicated(t *testing.T) {
	mock := &mockMessageClient{responses: []*anthropic.Message{
		toolUseNamed("toolu-b1", "delete_ecs_instance", map[string]any{"region": "cn-hangzhou"}),
		toolUseNamed("toolu-b2", "delete_ecs_instance", map[string]any{"region": "cn-hangzhou"}),
		textResponse(diagnosisJSONWithRecs([]map[string]any{})),
	}}
	res, err := testClient(mock).DiagnoseDryRun(context.Background(), map[string]any{"alert_id": "d-5"})
	if err != nil {
		t.Fatalf("DiagnoseDryRun() error = %v", err)
	}
	if len(res.BlockedByPolicy) != 1 {
		t.Fatalf("duplicate blocked calls must dedupe, got %+v", res.BlockedByPolicy)
	}
}

// --- Contract tdd_target: preconditions computation ---

func TestDiagnoseDryRun_PreconditionsReflectObservedReads(t *testing.T) {
	mock := &mockMessageClient{responses: []*anthropic.Message{
		toolUseNamed("toolu-r1", "describe_rds_instances", map[string]any{"region": "cn-hangzhou"}),
		toolUseNamed("toolu-r2", "describe_rds_slow_logs", map[string]any{"region": "cn-hangzhou"}),
		toolUseNamed("toolu-w1", "scale_rds_instance",
			map[string]any{"region": "cn-hangzhou", "resource_id": "rm-x"}),
		textResponse(diagnosisJSONWithRecs([]map[string]any{})),
	}}
	res, err := testClient(mock).DiagnoseDryRun(context.Background(), map[string]any{"alert_id": "d-6"})
	if err != nil {
		t.Fatalf("DiagnoseDryRun() error = %v", err)
	}
	if len(res.WouldExecute) != 1 {
		t.Fatalf("WouldExecute len = %d, want 1", len(res.WouldExecute))
	}
	got := strings.Join(res.WouldExecute[0].PreconditionsMet, " ")
	for _, want := range []string{"describe_rds_instances", "describe_rds_slow_logs"} {
		if !strings.Contains(got, want) {
			t.Fatalf("PreconditionsMet missing observed read %q: %v", want, res.WouldExecute[0].PreconditionsMet)
		}
	}
}

func TestDiagnoseDryRun_PreconditionsEmptyWithoutReads(t *testing.T) {
	mock := &mockMessageClient{responses: []*anthropic.Message{
		toolUseNamed("toolu-w1", "restart_ecs_instance",
			map[string]any{"region": "cn-hangzhou", "resource_id": "i-1"}),
		textResponse(diagnosisJSONWithRecs([]map[string]any{})),
	}}
	res, err := testClient(mock).DiagnoseDryRun(context.Background(), map[string]any{"alert_id": "d-7"})
	if err != nil {
		t.Fatalf("DiagnoseDryRun() error = %v", err)
	}
	if len(res.WouldExecute) != 1 {
		t.Fatalf("WouldExecute len = %d, want 1", len(res.WouldExecute))
	}
	if len(res.WouldExecute[0].PreconditionsMet) != 0 {
		t.Fatalf("PreconditionsMet = %v, want empty (no reads observed)", res.WouldExecute[0].PreconditionsMet)
	}
}

// --- Contract tdd_target: rollback present / missing ---

func TestDiagnoseDryRun_RollbackFromWriteToolMetadata(t *testing.T) {
	tests := []struct {
		name         string
		tool         string
		wantRollback string
		wantRisk     string
	}{
		{name: "scale has rollback", tool: "scale_rds_instance",
			wantRollback: "ModifyDBInstanceSpec (downgrade)", wantRisk: "high"},
		{name: "reboot has no rollback", tool: "restart_ecs_instance",
			wantRollback: "n/a (transient reboot)", wantRisk: "medium"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockMessageClient{responses: []*anthropic.Message{
				toolUseNamed("toolu-w1", tc.tool, map[string]any{"region": "cn-hangzhou", "resource_id": "r-1"}),
				textResponse(diagnosisJSONWithRecs([]map[string]any{})),
			}}
			res, err := testClient(mock).DiagnoseDryRun(context.Background(), map[string]any{"alert_id": "d-8"})
			if err != nil {
				t.Fatalf("DiagnoseDryRun() error = %v", err)
			}
			if len(res.WouldExecute) != 1 {
				t.Fatalf("WouldExecute len = %d, want 1", len(res.WouldExecute))
			}
			pa := res.WouldExecute[0]
			if pa.Rollback != tc.wantRollback {
				t.Fatalf("Rollback = %q, want %q", pa.Rollback, tc.wantRollback)
			}
			if pa.RiskLevel != tc.wantRisk {
				t.Fatalf("RiskLevel = %q, want %q", pa.RiskLevel, tc.wantRisk)
			}
		})
	}
}

// --- estimated total downtime sums recommendation estimates ---

func TestDiagnoseDryRun_EstimatedTotalDowntimeSumsRecommendations(t *testing.T) {
	mock := &mockMessageClient{responses: []*anthropic.Message{
		textResponse(diagnosisJSONWithRecs([]map[string]any{
			writeRec(30, "medium", "n/a"),
			writeRec(45, "high", "downgrade"),
		})),
	}}
	res, err := testClient(mock).DiagnoseDryRun(context.Background(), map[string]any{"alert_id": "d-9"})
	if err != nil {
		t.Fatalf("DiagnoseDryRun() error = %v", err)
	}
	if res.EstimatedTotalDowntimeS != 75 {
		t.Fatalf("EstimatedTotalDowntimeS = %d, want 75", res.EstimatedTotalDowntimeS)
	}
}

// --- Diagnose itself stays backward compatible (M1 contract) ---

func TestDiagnose_UnchangedSignatureStillWorks(t *testing.T) {
	mock := &mockMessageClient{responses: []*anthropic.Message{
		toolUseNamed("toolu-w1", "restart_ecs_instance", map[string]any{"region": "cn-hangzhou"}),
		textResponse(validDiagnosisJSON()),
	}}
	diagnosis, err := testClient(mock).Diagnose(context.Background(), map[string]any{"alert_id": "d-10"})
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if diagnosis.Confidence != "high" {
		t.Fatalf("Diagnose() = %#v", diagnosis)
	}
}

// --- stub mode: no API call, safe dry-run ---

func TestDiagnoseDryRun_StubMode(t *testing.T) {
	c := New(nil, "", "")
	res, err := c.DiagnoseDryRun(context.Background(), map[string]any{
		"alert_id": "a-1", "severity": "critical",
	})
	if err != nil {
		t.Fatalf("DiagnoseDryRun() error = %v", err)
	}
	if !res.DryRun || len(res.WouldExecute) != 0 || len(res.BlockedByPolicy) != 0 {
		t.Fatalf("stub dry-run = %+v", res)
	}
	if res.Diagnosis.RootCause == "" {
		t.Fatal("stub diagnosis must carry a root cause")
	}
}

// --- Recommendation parsing: backward compat + risk_level validation ---

func TestParseDiagnosis_OldRecommendationShapeStillParses(t *testing.T) {
	// M1-era JSON without the M3 fields must still parse (缺省容错).
	d, err := parseDiagnosis(validDiagnosisJSON())
	if err != nil {
		t.Fatalf("parseDiagnosis() error = %v", err)
	}
	if len(d.Recommendations) != 1 {
		t.Fatalf("recommendations = %+v", d.Recommendations)
	}
	rec := d.Recommendations[0]
	if rec.RiskLevel != "" || rec.RollbackCommand != "" || rec.EstimatedDowntimeS != 0 {
		t.Fatalf("missing M3 fields must stay zero-valued: %+v", rec)
	}
	if rec.Preconditions == nil {
		t.Fatal("nil preconditions must normalize to empty slice")
	}
}

func TestParseDiagnosis_M3FieldsParsed(t *testing.T) {
	d, err := parseDiagnosis(diagnosisJSONWithRecs([]map[string]any{writeRec(120, "irreversible", "")}))
	if err != nil {
		t.Fatalf("parseDiagnosis() error = %v", err)
	}
	rec := d.Recommendations[0]
	if rec.RiskLevel != "irreversible" || rec.EstimatedDowntimeS != 120 {
		t.Fatalf("M3 fields lost: %+v", rec)
	}
	if len(rec.Preconditions) != 1 || rec.Preconditions[0] != "maintenance window open" {
		t.Fatalf("preconditions = %+v", rec.Preconditions)
	}
}

func TestParseDiagnosis_InvalidRiskLevelRejected(t *testing.T) {
	_, err := parseDiagnosis(diagnosisJSONWithRecs([]map[string]any{writeRec(0, "banana", "")}))
	if err == nil {
		t.Fatal("invalid risk_level must be rejected")
	}
}

// --- WRITE_TOOLS whitelist shape (contract-m3-5.md write_tools_whitelist) ---

func TestWriteTools_WhitelistShape(t *testing.T) {
	if got := len(WRITE_TOOLS); got != 4 {
		t.Fatalf("WRITE_TOOLS has %d tools, want 4", got)
	}
	seen := map[string]bool{}
	for _, tool := range WRITE_TOOLS {
		if tool.Category != Write {
			t.Errorf("%s category = %q, want %q", tool.Name, tool.Category, Write)
		}
		if tool.RateLimitPerHour <= 0 {
			t.Errorf("%s rate_limit_per_hour = %d, want > 0", tool.Name, tool.RateLimitPerHour)
		}
		if tool.Risk == "" || tool.Rollback == "" {
			t.Errorf("%s missing risk/rollback metadata", tool.Name)
		}
		if seen[tool.Name] {
			t.Errorf("duplicate tool name %s", tool.Name)
		}
		seen[tool.Name] = true
	}
	for _, name := range []string{
		"restart_ecs_instance", "scale_rds_instance", "restart_rds_instance", "remove_ecs_from_slb",
	} {
		if !seen[name] {
			t.Errorf("WRITE_TOOLS missing %s", name)
		}
	}
}

func TestIsWriteAllowed(t *testing.T) {
	if !IsWriteAllowed("restart_ecs_instance") {
		t.Fatal("restart_ecs_instance should be write-allowed")
	}
	if IsWriteAllowed("describe_ecs_instances") {
		t.Fatal("read-only tool must not be write-allowed")
	}
	if IsWriteAllowed("delete_ecs_instance") {
		t.Fatal("unknown tool must not be write-allowed")
	}
}

func TestAllToolSpecsForLLMWithWrite(t *testing.T) {
	specs := AllToolSpecsForLLMWithWrite()
	if got := len(specs); got != len(READ_ONLY_TOOLS)+len(WRITE_TOOLS) {
		t.Fatalf("specs = %d, want %d", got, len(READ_ONLY_TOOLS)+len(WRITE_TOOLS))
	}
	// Read-only surface for Diagnose must stay exactly the M1 ten.
	if got := len(AllToolSpecsForLLM()); got != 10 {
		t.Fatalf("AllToolSpecsForLLM() = %d, want 10 (unchanged)", got)
	}
}
