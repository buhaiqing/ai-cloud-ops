package eval

import (
	"context"

	"github.com/buhaiqing/ai-cloud-ops/internal/agent"
)

// CIGateResult is the CI gate verdict for a baseline run.
type CIGateResult struct {
	Mean      float64     // mean of Total(scores)
	PerSample []ScoreCard // one scorecard per sample (input-sized; may be shorter on err)
	Pass      bool        // mean ≥ PassThreshold (18)
	Warn      bool        // 17 ≤ mean < 18
	Fail      bool        // mean < 17
	Threshold int         // mirrors PassThreshold for caller visibility
}

// WarnFloor separates warn (≥17) from fail (<17). PassThreshold (18)
// separates warn from pass.
const WarnFloor = 17

// EvaluateBaseline scores every sample with judge, then classifies pass/warn/fail.
// It calls agent.NewClient().Diagnose() per sample; tests inject behavior via
// either a Judge mock or by replacing the agent package. The first judge
// error short-circuits with (result, err).
//
// Ponytail: no configurability on the bucketing — the task locks 18/17 numbers.
func EvaluateBaseline(ctx context.Context, judge *Judge, samples []Sample) (CIGateResult, error) {
	res := CIGateResult{Threshold: PassThreshold, PerSample: make([]ScoreCard, 0, len(samples))}
	if len(samples) == 0 {
		res.Fail = true
		return res, nil
	}
	client := agent.NewClient("")
	var sum int
	for i, s := range samples {
		diagnosis := client.Diagnose(s.AlertPayload)
		card, err := judge.Score(ctx, s.AlertPayload, diagnosis)
		if err != nil {
			res.Fail = true
			res.Mean = avg(sum, i+1)
			return res, err
		}
		res.PerSample = append(res.PerSample, card)
		sum += Total(card)
	}
	res.Mean = avg(sum, len(samples))
	switch {
	case res.Mean >= float64(PassThreshold):
		res.Pass = true
	case res.Mean >= WarnFloor:
		res.Warn = true
	default:
		res.Fail = true
	}
	return res, nil
}

func avg(sum, n int) float64 { return float64(sum) / float64(n) }

// PlaceholderDiagnosis returns a minimal *agent.Diagnosis used by unit tests
// and CI gate smoke runs where the real agent is not wired in. Real CI will
// route through the production agent.
func PlaceholderDiagnosis(id string) *agent.Diagnosis {
	return &agent.Diagnosis{
		RootCause:      "synthetic",
		Recommendation: "synthetic",
		AlertID:        id,
	}
}
