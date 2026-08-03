package api

import (
	"encoding/json"
	"testing"
)

// --- validateRuleReq pure-function tests ---

func TestValidateRuleReq_RejectsMissingAccount(t *testing.T) {
	err := validateRuleReq(CreateRuleReq{Name: "x", Severity: "critical", Metric: "cpu"})
	if err == nil {
		t.Fatal("expected error for missing account_alias")
	}
}

func TestValidateRuleReq_RejectsBadSeverity(t *testing.T) {
	err := validateRuleReq(CreateRuleReq{AccountAlias: "a", Name: "n", Severity: "fatal", Metric: "cpu"})
	if err == nil {
		t.Fatal("expected error for invalid severity")
	}
}

func TestValidateRuleReq_AcceptsValid(t *testing.T) {
	err := validateRuleReq(CreateRuleReq{AccountAlias: "a", Name: "n", Severity: "warning", Metric: "cpu"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRuleReq_AllSeverities(t *testing.T) {
	for _, s := range []string{"critical", "warning", "info"} {
		err := validateRuleReq(CreateRuleReq{AccountAlias: "a", Name: "n", Severity: s, Metric: "m"})
		if err != nil {
			t.Fatalf("severity %q rejected: %v", s, err)
		}
	}
}

// --- JSON encoding sanity for the Rule contract ---

func TestRule_JSONRoundTrip(t *testing.T) {
	threshold := 80.0
	rl := Rule{
		ID:           42,
		AccountAlias: "main",
		Name:         "high-cpu",
		Severity:     "warning",
		Metric:       "cpu_util",
		Threshold:    &threshold,
		Channel:      json.RawMessage(`{"type":"webhook","url":"http://x"}`),
		Enabled:      true,
	}
	b, err := json.Marshal(rl)
	if err != nil {
		t.Fatal(err)
	}
	var back Rule
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Name != "high-cpu" || back.Severity != "warning" || !back.Enabled {
		t.Fatalf("roundtrip mismatch: %+v", back)
	}
	if back.Threshold == nil || *back.Threshold != 80.0 {
		t.Fatalf("threshold lost: %v", back.Threshold)
	}
}