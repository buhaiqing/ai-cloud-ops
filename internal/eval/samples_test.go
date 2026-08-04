package eval

import (
	"context"
	"testing"
)

// baselineSamplesFile is resolved relative to this test file (package dir).
const baselineSamplesFile = "baseline_samples.json"

func loadSamplesOrFatal(t *testing.T) []Sample {
	t.Helper()
	samples, err := LoadBaselineSamples(baselineSamplesFile)
	if err != nil {
		t.Fatalf("LoadBaselineSamples(%s): %v", baselineSamplesFile, err)
	}
	return samples
}

// TestLoadBaselineSamples_AtLeast30 guards the M3-8 expansion: the baseline
// set must hold at least 30 scenarios.
func TestLoadBaselineSamples_AtLeast30(t *testing.T) {
	samples := loadSamplesOrFatal(t)
	if len(samples) < 30 {
		t.Fatalf("len(samples) = %d, want >= 30", len(samples))
	}
}

// TestLoadBaselineSamples_UniqueIDs ensures every sample id is unique and
// non-empty so scorecards can be attributed unambiguously.
func TestLoadBaselineSamples_UniqueIDs(t *testing.T) {
	samples := loadSamplesOrFatal(t)
	seen := make(map[string]bool, len(samples))
	for _, s := range samples {
		if s.ID == "" {
			t.Fatalf("sample %q has empty ID", s.ID)
		}
		if seen[s.ID] {
			t.Fatalf("duplicate sample ID %q", s.ID)
		}
		seen[s.ID] = true
	}
}

var (
	allowedCategories = map[string]bool{
		"ECS": true, "RDS": true, "SLB": true, "Redis": true, "MongoDB": true,
		"OSS": true, "VPC": true, "K8s": true, "Multi-Resource": true,
		"Network": true, "Edge Case": true, "Security": true,
	}
	allowedDifficulties = map[string]bool{"easy": true, "medium": true, "hard": true}
)

// TestLoadBaselineSamples_FieldIntegrity validates the schema contract on
// every sample: category/difficulty in allowed sets, keywords present, and
// alert_payload carries the routing fields the agent/judge depend on.
func TestLoadBaselineSamples_FieldIntegrity(t *testing.T) {
	samples := loadSamplesOrFatal(t)
	for i, s := range samples {
		if s.ID == "" {
			t.Errorf("samples[%d]: empty ID", i)
		}
		if !allowedCategories[s.Category] {
			t.Errorf("%s: category %q not in allowed set", s.ID, s.Category)
		}
		if !allowedDifficulties[s.Difficulty] {
			t.Errorf("%s: difficulty %q not in allowed set", s.ID, s.Difficulty)
		}
		if len(s.ExpectedKeywords) == 0 {
			t.Errorf("%s: expected_root_cause_keywords is empty", s.ID)
		}
		p := s.AlertPayload
		if p["alert_id"] == nil {
			t.Errorf("%s: alert_payload missing alert_id", s.ID)
		}
		// Fleet-wide alerts (multi-ecs-correlation-09) use resource_ids
		// instead of resource_id; accept either, but never both missing.
		if p["resource_id"] == nil && p["resource_ids"] == nil {
			t.Errorf("%s: alert_payload missing resource_id/resource_ids", s.ID)
		}
		if p["region"] == nil {
			t.Errorf("%s: alert_payload missing region", s.ID)
		}
		metric, ok := p["metric"].(map[string]any)
		if !ok || metric == nil {
			t.Errorf("%s: alert_payload missing metric object", s.ID)
			continue
		}
		if metric["metric_name"] == nil {
			t.Errorf("%s: metric missing metric_name", s.ID)
		}
	}
}

// TestEvaluateBaseline_StubSmoke_30Samples proves the full sample set runs
// through the stub agent + judge + gate pipeline: fake judge returns a fixed
// perfect score, so the gate must pass with one scorecard per sample.
func TestEvaluateBaseline_StubSmoke_30Samples(t *testing.T) {
	samples := loadSamplesOrFatal(t)
	j := newJudgeWith(&fakeClient{body: `{"root_cause":5,"recommendation":5,"evidence":5,"hallucination":5,"latency":5,"reasoning":"perfect"}`}, nil) // 25
	res, err := EvaluateBaseline(context.Background(), j, samples)
	if err != nil {
		t.Fatalf("EvaluateBaseline: %v", err)
	}
	if !res.Pass {
		t.Errorf("expected Pass=true, got %+v", res)
	}
	if len(res.PerSample) != len(samples) {
		t.Errorf("len(PerSample) = %d, want %d", len(res.PerSample), len(samples))
	}
}
