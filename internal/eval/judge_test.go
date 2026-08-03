package eval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// fakeClient implements anthropicClient; `body` is returned as message text.
// `fail` triggers the error path.
type fakeClient struct {
	body string
	fail error
}

func (f *fakeClient) CreateMessage(_ context.Context, _ anthropic.MessageNewParams) (*anthropic.Message, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	return &anthropic.Message{Content: []anthropic.ContentBlockUnion{
		{Text: f.body},
	}}, nil
}

func goodScoreJSON() string {
	return `{"root_cause":4,"recommendation":4,"evidence":3,"hallucination":5,"latency":4,"reasoning":"looks ok"}`
}

func newFakeJudge(body string) *Judge {
	return newJudgeWith(&fakeClient{body: body}, nil)
}

func TestScoreParsesValidResponse(t *testing.T) {
	j := newFakeJudge(goodScoreJSON())
	card, err := j.Score(context.Background(), map[string]any{"alert": "x"}, PlaceholderDiagnosis("a1"))
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if Total(card) != 20 {
		t.Errorf("Total = %d, want 20", Total(card))
	}
	if card[DimHallucination] != 5 {
		t.Errorf("hallucination = %d, want 5", card[DimHallucination])
	}
}

func TestScoreRejectsMalformedJSON(t *testing.T) {
	j := newFakeJudge(`not json`)
	if _, err := j.Score(context.Background(), nil, PlaceholderDiagnosis("a1")); err == nil {
		t.Fatal("want error for malformed JSON, got nil")
	}
}

func TestScoreRejectsUnknownFields(t *testing.T) {
	j := newFakeJudge(`{"root_cause":4,"recommendation":4,"evidence":3,"hallucination":5,"latency":4,"extra_dim":99}`)
	_, err := j.Score(context.Background(), nil, PlaceholderDiagnosis("a1"))
	if err == nil {
		t.Fatal("want error for unknown field, got nil")
	}
	if !strings.Contains(err.Error(), "unknown field") &&
		!strings.Contains(err.Error(), "extra_dim") {
		t.Errorf("error should mention unknown field, got %v", err)
	}
}

func TestScoreRejectsOutOfRange(t *testing.T) {
	j := newFakeJudge(`{"root_cause":4,"recommendation":4,"evidence":3,"hallucination":5,"latency":7}`)
	if _, err := j.Score(context.Background(), nil, PlaceholderDiagnosis("a1")); err == nil {
		t.Fatal("want error for out-of-range dim, got nil")
	}
}

func TestScoreStripsCodeFences(t *testing.T) {
	j := newFakeJudge("```json\n" + goodScoreJSON() + "\n```")
	if _, err := j.Score(context.Background(), nil, PlaceholderDiagnosis("a1")); err != nil {
		t.Fatalf("Score with code fences: %v", err)
	}
}

func TestScorePropagatesAPIError(t *testing.T) {
	j := newJudgeWith(&fakeClient{fail: errors.New("api down")}, nil)
	if _, err := j.Score(context.Background(), nil, PlaceholderDiagnosis("a1")); err == nil {
		t.Fatal("want propagated api error, got nil")
	}
}

func TestEvaluateBaselinePasses(t *testing.T) {
	j := newFakeJudge(`{"root_cause":5,"recommendation":5,"evidence":4,"hallucination":5,"latency":5,"reasoning":"great"}`) // 24
	samples := []Sample{
		{ID: "s1", AlertPayload: map[string]any{}},
		{ID: "s2", AlertPayload: map[string]any{}},
	}
	res, err := EvaluateBaseline(context.Background(), j, samples)
	if err != nil {
		t.Fatalf("EvaluateBaseline: %v", err)
	}
	if !res.Pass || res.Warn || res.Fail {
		t.Errorf("expected Pass=true, got %+v", res)
	}
	if res.Mean != 24 {
		t.Errorf("Mean = %v, want 24", res.Mean)
	}
}

func TestEvaluateBaselineWarns(t *testing.T) {
	j := newFakeJudge(`{"root_cause":4,"recommendation":3,"evidence":3,"hallucination":4,"latency":3,"reasoning":"ok"}`) // 17
	samples := []Sample{{ID: "s1", AlertPayload: map[string]any{}}}
	res, err := EvaluateBaseline(context.Background(), j, samples)
	if err != nil {
		t.Fatalf("EvaluateBaseline: %v", err)
	}
	if !res.Warn || res.Pass || res.Fail {
		t.Errorf("expected Warn=true, got %+v", res)
	}
	if res.Mean != 17 {
		t.Errorf("Mean = %v, want 17", res.Mean)
	}
}

func TestEvaluateBaselineFails(t *testing.T) {
	j := newFakeJudge(`{"root_cause":3,"recommendation":3,"evidence":3,"hallucination":3,"latency":3,"reasoning":"meh"}`) // 15
	samples := []Sample{{ID: "s1", AlertPayload: map[string]any{}}}
	res, err := EvaluateBaseline(context.Background(), j, samples)
	if err != nil {
		t.Fatalf("EvaluateBaseline: %v", err)
	}
	if !res.Fail || res.Pass || res.Warn {
		t.Errorf("expected Fail=true, got %+v", res)
	}
}

func TestEvaluateBaselineEmptySamples(t *testing.T) {
	j := newFakeJudge(`{}`)
	res, err := EvaluateBaseline(context.Background(), j, nil)
	if err != nil {
		t.Fatalf("EvaluateBaseline: %v", err)
	}
	if !res.Fail || res.Mean != 0 {
		t.Errorf("expected Fail=true, Mean=0, got %+v", res)
	}
}

func TestEvaluateBaselineShortCircuitsOnError(t *testing.T) {
	j := newJudgeWith(&fakeClient{fail: errors.New("boom")}, nil)
	samples := []Sample{{ID: "s1", AlertPayload: map[string]any{}}}
	res, err := EvaluateBaseline(context.Background(), j, samples)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !res.Fail {
		t.Errorf("expected Fail=true on error, got %+v", res)
	}
}
