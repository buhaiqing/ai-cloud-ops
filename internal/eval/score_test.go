package eval

import "testing"

func TestTotalSumsFiveDims(t *testing.T) {
	card := ScoreCard{
		DimRootCause:      4,
		DimRecommendation: 4,
		DimEvidence:       3,
		DimHallucination:  5,
		DimLatency:        4,
	}
	if got := Total(card); got != 20 {
		t.Fatalf("Total() = %d, want 20", got)
	}
}

func TestTotalMaxIs25(t *testing.T) {
	card := ScoreCard{DimRootCause: 5, DimRecommendation: 5, DimEvidence: 5, DimHallucination: 5, DimLatency: 5}
	if got := Total(card); got != 25 {
		t.Fatalf("Total() at all-5s = %d, want 25", got)
	}
}

func TestAggregateReturnsMean(t *testing.T) {
	scores := []ScoreCard{
		{DimRootCause: 4, DimRecommendation: 4, DimEvidence: 3, DimHallucination: 5, DimLatency: 4}, // 20
		{DimRootCause: 3, DimRecommendation: 3, DimEvidence: 3, DimHallucination: 4, DimLatency: 4}, // 17
	}
	got := Aggregate(scores)
	if got[DimRootCause] != 3.5 {
		t.Errorf("mean(root_cause) = %v, want 3.5", got[DimRootCause])
	}
	if got[DimLatency] != 4.0 {
		t.Errorf("mean(latency) = %v, want 4.0", got[DimLatency])
	}
}

func TestAggregateEmptyInputReturnsZeros(t *testing.T) {
	got := Aggregate(nil)
	for _, d := range Dims() {
		if got[d] != 0.0 {
			t.Errorf("dim %s = %v, want 0.0", d, got[d])
		}
	}
}

func TestConstants(t *testing.T) {
	if PassThreshold != 18 {
		t.Errorf("PassThreshold = %d, want 18", PassThreshold)
	}
	if JudgeModel != "claude-sonnet-4-5" {
		t.Errorf("JudgeModel = %q, want claude-sonnet-4-5", JudgeModel)
	}
	if len(Dims()) != 5 {
		t.Errorf("Dims() count = %d, want 5", len(Dims()))
	}
}
