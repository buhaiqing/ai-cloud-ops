package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/buhaiqing/ai-cloud-ops/internal/agent"
)

// JudgeSystemPrompt mirrors the Python reference. JSON-only output with the
// five score keys plus a free-form reasoning field.
const JudgeSystemPrompt = `You are an expert SRE evaluator. You will receive an ` +
	`Alibaba Cloud alert and an AI-generated diagnosis. Score the diagnosis on ` +
	`5 dimensions (1-5 each):

1. 根因准确性 (root cause accuracy): does it correctly identify the cause?
2. 修复建议可执行性 (recommendation executability): are actions concrete + actionable?
3. 证据链完整性 (evidence chain completeness): does every claim trace to a tool call?
4. 幻觉率 (hallucination rate): 5=no invented IDs/timestamps; 1=many hallucinations
5. 响应时间 (response time): 5=<5s; 1=>30s

Output JSON only:
{"root_cause": 1-5, "recommendation": 1-5, "evidence": 1-5, "hallucination": 1-5, "latency": 1-5, "reasoning": "..."}
`

// anthropicClient is the subset of the Anthropic SDK used by Judge. Defined
// here so tests can inject a fake without a live network.
type anthropicClient interface {
	CreateMessage(ctx context.Context, body anthropic.MessageNewParams) (*anthropic.Message, error)
}

// realAnthropicClient wraps anthropic.Client (a value, not a pointer in v1.x)
// to satisfy the anthropicClient interface.
type realAnthropicClient struct{ c anthropic.Client }

// CreateMessage delegates to the SDK.
func (r *realAnthropicClient) CreateMessage(ctx context.Context, body anthropic.MessageNewParams) (*anthropic.Message, error) {
	return r.c.Messages.New(ctx, body)
}

// Judge asks Claude to score a diagnosis on the 5-dim rubric.
type Judge struct {
	anthropicClient anthropicClient
	model           string
	log             *slog.Logger
}

// NewJudge builds a Judge backed by the real Anthropic API.
func NewJudge(apiKey string) *Judge {
	c := anthropic.NewClient(option.WithAPIKey(apiKey))
	return newJudgeWith(&realAnthropicClient{c: c}, slog.Default())
}

// newJudgeWith is the internal constructor; tests use it to inject fakes.
func newJudgeWith(c anthropicClient, log *slog.Logger) *Judge {
	if log == nil {
		log = slog.Default()
	}
	return &Judge{anthropicClient: c, model: JudgeModel, log: log}
}

// rawScore is the JSON wire shape. Strict parsing (DisallowUnknownFields) makes
// upstream drift visible: a new dimension on the model side → explicit error.
type rawScore struct {
	RootCause      int    `json:"root_cause"`
	Recommendation int    `json:"recommendation"`
	Evidence       int    `json:"evidence"`
	Hallucination  int    `json:"hallucination"`
	Latency        int    `json:"latency"`
	Reasoning      string `json:"reasoning"`
}

func (r rawScore) toScoreCard() ScoreCard {
	return ScoreCard{
		DimRootCause:      r.RootCause,
		DimRecommendation: r.Recommendation,
		DimEvidence:       r.Evidence,
		DimHallucination:  r.Hallucination,
		DimLatency:        r.Latency,
	}
}

// Score queries the judge model and returns the parsed ScoreCard.
func (j *Judge) Score(ctx context.Context, alert map[string]any, diagnosis *agent.Diagnosis) (ScoreCard, error) {
	body := anthropic.MessageNewParams{
		Model:     j.model,
		MaxTokens: 1024,
		System: []anthropic.TextBlockParam{
			{Text: JudgeSystemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(buildUserPrompt(alert, diagnosis))),
		},
	}
	resp, err := j.anthropicClient.CreateMessage(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("judge: create message: %w", err)
	}
	raw := extractText(resp)
	if raw == "" {
		return nil, fmt.Errorf("judge: empty model response")
	}
	raw = stripCodeFences(raw)
	var parsed rawScore
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("judge: parse score json: %w (raw=%s)", err, raw)
	}
	card := parsed.toScoreCard()
	for _, d := range Dims() {
		v := card[d]
		if v < 1 || v > 5 {
			return nil, fmt.Errorf("judge: dim %s out of range 1-5: got %d", d, v)
		}
	}
	return card, nil
}

func buildUserPrompt(alert map[string]any, d *agent.Diagnosis) string {
	alertJSON, err := json.MarshalIndent(alert, "", "  ")
	if err != nil {
		alertJSON = fmt.Appendf(nil, "%v", alert)
	}
	diagJSON, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		diagJSON = fmt.Appendf(nil, "%v", d)
	}
	return fmt.Sprintf("Alert:\n%s\n\nAI Diagnosis:\n%s", alertJSON, diagJSON)
}

func extractText(msg *anthropic.Message) string {
	var b strings.Builder
	for _, block := range msg.Content {
		// Prefer explicit text-type; fall back to any non-empty Text payload so
		// downstream parsing is robust against partial/streaming responses.
		if block.Type == "text" || block.Text != "" {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

// stripCodeFences removes ```json ... ``` fences if present. Model output
// occasionally wraps JSON in a code block; we accept both forms.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	// optional language tag on first line
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if j := strings.LastIndex(s, "```"); j >= 0 {
		s = s[:j]
	}
	return strings.TrimSpace(s)
}
