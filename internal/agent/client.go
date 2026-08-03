// Package agent implements the AI diagnosis agent for ai-cloud-ops.
//
// Client is the entry point. M1 wiring exposes the right types (Diagnosis,
// Recommendation, EvidenceChain, Tool whitelist, Prompt) so eval and MCP
// can build against the same contract as production.
//
// M1 NOTE: the real Anthropic SDK tool-use loop is wired in scaffold but
// not exercised (see TODO below). M1 Client.Diagnose returns a structured
// diagnosis populated from the alert payload so the eval framework and
// MCP server can run end-to-end. The full Claude API integration lands in
// the next Go Phase — see docs/go-migration.md § Phase 3.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Diagnosis is the structured output of the AI Agent for a single alert.
// Mirrors src/ai_cloud_ops/agent/client.py's pydantic Diagnosis model.
type Diagnosis struct {
	RootCause       string           `json:"root_cause"`
	Recommendations []Recommendation `json:"recommendations"`
	EvidenceChains  []EvidenceChain  `json:"evidence_chains"`
	Confidence      string           `json:"confidence"` // "high" | "medium" | "low"
	Caveats         []string         `json:"caveats"`
	LatencyMs       int              `json:"latency_ms"`
	Model           string           `json:"model"`
	PromptVersion   string           `json:"prompt_version"`
}

type Recommendation struct {
	Action          string `json:"action"`
	Command         string `json:"command"`
	ExpectedOutcome string `json:"expected_outcome"`
}

type EvidenceChain struct {
	Claim          string `json:"claim"`
	SupportingTool string `json:"supporting_tool"`
	SupportingData string `json:"supporting_data"`
}

// ToolExecutor runs a whitelisted tool and returns its result as a string.
// M1 default is a noop; production wiring will use internal/ingest/fetcher.
type ToolExecutor interface {
	Execute(ctx context.Context, toolName string, args map[string]any) (string, error)
}

// NoopToolExecutor returns a "tool not implemented in M1" stub for every call.
type NoopToolExecutor struct{}

func (NoopToolExecutor) Execute(_ context.Context, toolName string, _ map[string]any) (string, error) {
	return fmt.Sprintf(`{"status":"tool_not_implemented_in_m1","tool":%q}`, toolName), nil
}

// Client is the AI Agent. M1 returns a structured diagnosis from the alert
// payload without a live Claude call; future wiring uses the Anthropic SDK
// (TODO: implement in next Go phase).
type Client struct {
	apiKey string
	model  string
	// executor may be nil (uses NoopToolExecutor default)
	executor ToolExecutor
}

// NewClient builds a Client. apiKey/model are retained for future SDK use;
// executor may be nil (uses NoopToolExecutor).
func NewClient(apiKey, model string, executor ToolExecutor) *Client {
	if model == "" {
		model = "claude-sonnet-4-5"
	}
	if executor == nil {
		executor = NoopToolExecutor{}
	}
	return &Client{
		apiKey:   apiKey,
		model:    model,
		executor: executor,
	}
}

// ErrAnthropicNotImplemented signals that the real SDK loop hasn't been wired
// in this build. Callers fall back to the alert-payload stub.
var ErrAnthropicNotImplemented = errors.New("agent: Anthropic SDK integration deferred to next Go phase")

// Diagnose runs the AI Agent on one alert and returns a structured Diagnosis.
//
// M1 behavior: returns a populated Diagnosis derived from the alert payload
// (no live Claude call). M1 is wired so the eval framework + MCP server can
// run end-to-end against the contract; real SDK tool-use loop is TODO.
func (c *Client) Diagnose(ctx context.Context, alert map[string]any) (*Diagnosis, error) {
	_ = ctx // reserved for future SDK call
	start := time.Now()
	d := m1StubDiagnosis(alert, c.model, start)
	d.LatencyMs = int(time.Since(start).Milliseconds())
	return d, nil
}

// m1StubDiagnosis builds a best-effort Diagnosis from the alert payload
// without an LLM call. It surfaces whatever structured information is
// present (alert_id, severity, metric) so callers can render the UI.
func m1StubDiagnosis(alert map[string]any, model string, start time.Time) *Diagnosis {
	alertID, _ := alert["alert_id"].(string)
	severity, _ := alert["severity"].(string)

	metric, _ := alert["metric"].(map[string]any)
	metricName, _ := metric["metric_name"].(string)
	metricValue, _ := metric["value"]
	resourceType, _ := alert["resource_type"].(string)

	root := "unknown"
	if metricName != "" {
		root = fmt.Sprintf("alert on %s (%s) — value %v", metricName, severity, metricValue)
	}

	recs := []Recommendation{
		{
			Action:          fmt.Sprintf("investigate %s alert %s", resourceType, alertID),
			ExpectedOutcome: "human triage confirms root cause",
		},
	}

	caveats := []string{
		"M1 stub: real Anthropic Diagnose lands in next Go phase",
	}

	return &Diagnosis{
		RootCause:       root,
		Recommendations: recs,
		EvidenceChains:  []EvidenceChain{},
		Confidence:      "low",
		Caveats:         caveats,
		Model:           model,
		PromptVersion:   PromptVersion,
	}
}

// ParseDiagnosis is exported for the eval suite. Mirrors the Python helper.
// Uses DisallowUnknownFields to surface upstream schema drift as a hard error.
func ParseDiagnosis(raw string, model string, latencyMs int) (*Diagnosis, error) {
	stripped := stripCodeFences(raw)
	var d Diagnosis
	dec := json.NewDecoder(strings.NewReader(stripped))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	d.Model = model
	d.LatencyMs = latencyMs
	d.PromptVersion = PromptVersion
	if d.Confidence == "" {
		d.Confidence = "low"
	}
	return &d, nil
}

func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if j := strings.LastIndex(s, "```"); j >= 0 {
		s = s[:j]
	}
	return strings.TrimSpace(s)
}