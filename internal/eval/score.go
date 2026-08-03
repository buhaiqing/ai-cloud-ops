// Package eval is the LLM-as-judge scoring framework for the AI agent.
// Mirrors tests/eval/judge.py — 5-dim scorecard, 72 % pass threshold.
package eval

// Pass threshold and judge model — mirrored from tests/eval/judge.py.
const (
	PassThreshold = 18 // of 25
	JudgeModel    = "claude-sonnet-4-5"
)

// Dimension keys. Exposed so callers and tests can iterate/validate without
// duplicating literals. JSON wire format uses snake_case; see rawScore in judge.go.
const (
	DimRootCause      = "RootCause"
	DimRecommendation = "Recommendation"
	DimEvidence       = "Evidence"
	DimHallucination  = "Hallucination"
	DimLatency        = "Latency"
)

// Dims returns the dimension keys in canonical order.
func Dims() []string {
	return []string{DimRootCause, DimRecommendation, DimEvidence, DimHallucination, DimLatency}
}

// ScoreCard is a 5-dimension score on the (1..5) scale. 25 = perfect.
type ScoreCard map[string]int

// Total sums the 5 dims.
func Total(s ScoreCard) int {
	sum := 0
	for _, d := range Dims() {
		sum += s[d]
	}
	return sum
}

// Aggregate returns per-dimension mean across scores. Empty input → zero map.
func Aggregate(scores []ScoreCard) map[string]float64 {
	out := make(map[string]float64, 5)
	for _, d := range Dims() {
		out[d] = 0
	}
	if len(scores) == 0 {
		return out
	}
	n := float64(len(scores))
	for _, d := range Dims() {
		var sum int
		for _, s := range scores {
			sum += s[d]
		}
		out[d] = float64(sum) / n
	}
	return out
}
