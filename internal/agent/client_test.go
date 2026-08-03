package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseDiagnosisValid(t *testing.T) {
	raw := `{
		"root_cause": "CPU sustained > 90% for 15 minutes",
		"recommendations": [
			{"action": "reboot instance", "command": "aliyun ecs RebootInstance", "expected_outcome": "CPU drops to <50%"}
		],
		"evidence_chains": [
			{"claim": "CPU high", "supporting_tool": "describe_ecs_monitor_data", "supporting_data": "max=96.5 avg=92.1"}
		],
		"confidence": "high",
		"caveats": []
	}`
	d, err := ParseDiagnosis(raw, "claude-sonnet-4-5", 1234)
	if err != nil {
		t.Fatalf("ParseDiagnosis: %v", err)
	}
	if d.RootCause == "" {
		t.Error("expected non-empty RootCause")
	}
	if d.Confidence != "high" {
		t.Errorf("got confidence %q, want high", d.Confidence)
	}
	if d.Model != "claude-sonnet-4-5" {
		t.Errorf("model not injected: %q", d.Model)
	}
	if d.PromptVersion != PromptVersion {
		t.Errorf("prompt_version not injected: got %q want %q", d.PromptVersion, PromptVersion)
	}
	if d.LatencyMs != 1234 {
		t.Errorf("latency_ms not injected: got %d", d.LatencyMs)
	}
}

func TestParseDiagnosisRejectsUnknownFields(t *testing.T) {
	raw := `{
		"root_cause": "x",
		"recommendations": [],
		"evidence_chains": [],
		"confidence": "low",
		"caveats": [],
		"surprise_field": "should fail"
	}`
	if _, err := ParseDiagnosis(raw, "m", 0); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestParseDiagnosisStripsCodeFences(t *testing.T) {
	raw := "```json\n{\"root_cause\":\"x\",\"recommendations\":[],\"evidence_chains\":[],\"confidence\":\"low\",\"caveats\":[]}\n```"
	d, err := ParseDiagnosis(raw, "m", 0)
	if err != nil {
		t.Fatalf("ParseDiagnosis: %v", err)
	}
	if d.RootCause != "x" {
		t.Errorf("got %q", d.RootCause)
	}
}

func TestStripCodeFences(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{`{"a":1}`, `{"a":1}`},
		{"```\n{\"a\":1}\n```", "{\"a\":1}"},
		{"```json\n{\"a\":1}\n```", "{\"a\":1}"},
	}
	for _, tc := range tests {
		if got := stripCodeFences(tc.in); got != tc.want {
			t.Errorf("stripCodeFences(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNewClientDefaults(t *testing.T) {
	c := NewClient("test-key", "", nil)
	if c.model != "claude-sonnet-4-5" {
		t.Errorf("default model should be claude-sonnet-4-5, got %q", c.model)
	}
	if c.executor == nil {
		t.Error("default executor should be NoopToolExecutor, got nil")
	}
}

func TestDiagnoseStubReturnsPopulatedDiagnosis(t *testing.T) {
	c := NewClient("test-key", "", nil)
	alert := map[string]any{
		"alert_id":      "alert-stub-001",
		"severity":      "critical",
		"resource_type": "ECS",
		"metric": map[string]any{
			"metric_name": "CPUUtilization",
			"value":       96.5,
		},
	}
	d, err := c.Diagnose(context.Background(), alert)
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if d.Confidence != "low" {
		t.Errorf("stub confidence should be low, got %q", d.Confidence)
	}
	if d.RootCause == "" {
		t.Error("stub should populate RootCause from alert payload")
	}
	if d.PromptVersion != PromptVersion {
		t.Error("stub should inject PromptVersion")
	}
	if len(d.Recommendations) == 0 {
		t.Error("stub should produce at least one recommendation")
	}
}

func TestDiagnoseReturnsErrorFlagForUnsetAPI(t *testing.T) {
	c := NewClient("", "", nil)
	_, err := c.Diagnose(context.Background(), map[string]any{"alert_id": "x"})
	if err != nil {
		t.Errorf("M1 stub should never error, got %v", err)
	}
}

func TestNoopExecutor(t *testing.T) {
	exec := NoopToolExecutor{}
	out, err := exec.Execute(context.Background(), "describe_ecs_instances", nil)
	if err != nil {
		t.Fatalf("noop should not error: %v", err)
	}
	if !contains(out, "tool_not_implemented_in_m1") {
		t.Errorf("noop output should mark as not implemented, got %q", out)
	}
}

// contains is a tiny strings.Contains helper to avoid the import.
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Sanity: the Diagnosis type is JSON-marshalable so MCP can return it.
func TestDiagnosisJSONRoundTrip(t *testing.T) {
	d := &Diagnosis{
		RootCause: "test",
		Recommendations: []Recommendation{
			{Action: "a", Command: "c", ExpectedOutcome: "e"},
		},
		Confidence: "high",
		Caveats:    []string{"x"},
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"root_cause":"test"`) {
		t.Errorf("unexpected JSON: %s", b)
	}
}