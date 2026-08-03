package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

const maxToolIterations = 5

type messageCreator interface {
	New(context.Context, anthropic.MessageNewParams, ...option.RequestOption) (*anthropic.Message, error)
}

// Client runs the Claude diagnosis loop.
type Client struct {
	pool            *pgxpool.Pool
	anthropicClient *anthropic.Client
	model           string
	messageClient   messageCreator
	// stub=true means Diagnose returns alert-payload-derived stub (no API call).
	// Set by New() when apiKey is empty; tests use a mock messageClient.
	stub bool
}

// New creates an Anthropic-backed diagnosis client.
// When apiKey is empty, returns a stub-mode Client whose Diagnose falls
// back to the alert-payload-derived stub (no API call). This keeps unit
// tests and offline environments safe from inadvertent SDK usage.
func New(pool *pgxpool.Pool, apiKey, model string) *Client {
	if apiKey == "" {
		if model == "" {
			model = "claude-sonnet-4-5"
		}
		return &Client{
			pool:   pool,
			model:  model,
			stub:   true,
		}
	}
	var opts []option.RequestOption
	opts = append(opts, option.WithAPIKey(apiKey))
	sdkClient := anthropic.NewClient(opts...)
	return &Client{
		pool:            pool,
		anthropicClient: &sdkClient,
		model:           model,
		messageClient:   &sdkClient.Messages,
	}
}

// Diagnosis is the structured result returned by the AI Agent.
type Diagnosis struct {
	RootCause       string           `json:"root_cause"`
	Recommendations []Recommendation `json:"recommendations"`
	EvidenceChains  []EvidenceChain  `json:"evidence_chains"`
	Confidence      string           `json:"confidence"`
	Caveats         []string         `json:"caveats"`
	LatencyMs       int              `json:"latency_ms"`
	Model           string           `json:"model"`
	PromptVersion   string           `json:"prompt_version"`
}

// Recommendation describes one concrete remediation step.
type Recommendation struct {
	Action          string `json:"action"`
	Command         string `json:"command"`
	ExpectedOutcome string `json:"expected_outcome"`
}

// EvidenceChain links a diagnosis claim to one tool result.
type EvidenceChain struct {
	Claim          string `json:"claim"`
	SupportingTool string `json:"supporting_tool"`
	SupportingData string `json:"supporting_data"`
}

// Diagnose asks Claude to investigate one alert using only read-only tools.
// In stub mode (apiKey was empty at New), returns an alert-payload-derived
// Diagnosis without calling the API.
func (c *Client) Diagnose(ctx context.Context, alert map[string]any) (*Diagnosis, error) {
	if c.stub {
		started := time.Now()
		d := stubDiagnosisFromAlert(alert, c.model)
		d.LatencyMs = int(time.Since(started) / time.Millisecond)
		return d, nil
	}
	started := time.Now()
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(BuildUserPrompt(alert))),
	}

	specs := AllToolSpecsForLLM()
	tools := make([]anthropic.ToolUnionParam, 0, len(specs))
	for _, spec := range specs {
		inputSchema := spec["input_schema"].(map[string]any)
		tool := anthropic.ToolParam{
			Name:        spec["name"].(string),
			Description: anthropic.String(spec["description"].(string)),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: inputSchema["properties"],
				Required:   inputSchema["required"].([]string),
			},
		}
		tools = append(tools, anthropic.ToolUnionParam{OfTool: &tool})
	}

	for range maxToolIterations {
		response, err := c.messageClient.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(c.model),
			MaxTokens: 2048,
			System:    []anthropic.TextBlockParam{{Text: SystemPrompt}},
			Messages:  messages,
			Tools:     tools,
		})
		if err != nil {
			wrapped := fmt.Errorf("diagnose: create Anthropic message: %w", err)
			slog.Error("agent.inference.failed", "err", wrapped)
			return c.lowConfidence(started, "inference failed"), wrapped
		}

		messages = append(messages, response.ToParam())
		var text strings.Builder
		var toolUses []anthropic.ToolUseBlock
		for _, block := range response.Content {
			switch content := block.AsAny().(type) {
			case anthropic.TextBlock:
				if text.Len() > 0 {
					text.WriteByte('\n')
				}
				text.WriteString(content.Text)
			case anthropic.ToolUseBlock:
				toolUses = append(toolUses, content)
			}
		}

		if len(toolUses) == 0 {
			diagnosis, err := parseDiagnosis(text.String())
			if err != nil {
				wrapped := fmt.Errorf("diagnose: parse response: %w", err)
				slog.Error("agent.inference.parse_failed", "err", wrapped)
				return c.lowConfidence(started, "inference failed"), wrapped
			}
			diagnosis.LatencyMs = int(time.Since(started) / time.Millisecond)
			diagnosis.Model = c.model
			diagnosis.PromptVersion = PromptVersion
			return diagnosis, nil
		}

		toolResults := make([]anthropic.ContentBlockParamUnion, len(toolUses))
		group, groupCtx := errgroup.WithContext(ctx)
		for i, toolUse := range toolUses {
			i, toolUse := i, toolUse
			group.Go(func() error {
				if err := groupCtx.Err(); err != nil {
					return fmt.Errorf("execute tool %s: %w", toolUse.Name, err)
				}
				if !IsAllowed(toolUse.Name) {
					toolErr := ToolNotAllowedError{Name: toolUse.Name}
					slog.Warn("agent.tool.not_allowed", "tool", toolUse.Name)
					toolResults[i] = anthropic.NewToolResultBlock(toolUse.ID, toolErr.Error(), true)
					return nil
				}
				result, err := json.Marshal(map[string]any{
					"status": "tool_not_implemented_in_m1",
					"tool":   toolUse.Name,
				})
				if err != nil {
					return fmt.Errorf("marshal tool result for %s: %w", toolUse.Name, err)
				}
				toolResults[i] = anthropic.NewToolResultBlock(toolUse.ID, string(result), false)
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			wrapped := fmt.Errorf("diagnose: execute tools: %w", err)
			slog.Error("agent.tool.failed", "err", wrapped)
			return c.lowConfidence(started, "inference failed"), wrapped
		}
		messages = append(messages, anthropic.NewUserMessage(toolResults...))
	}

	diagnosis := c.lowConfidence(started, "tool iteration limit reached")
	diagnosis.RootCause = "diagnosis_truncated"
	return diagnosis, nil
}

func (c *Client) lowConfidence(started time.Time, caveat string) *Diagnosis {
	return &Diagnosis{
		Confidence:    "low",
		Caveats:       []string{caveat},
		LatencyMs:     int(time.Since(started) / time.Millisecond),
		Model:         c.model,
		PromptVersion: PromptVersion,
	}
}

// stubDiagnosisFromAlert builds a best-effort Diagnosis from the alert
// payload without an LLM call. Used when apiKey is empty (offline /
// unit tests). Surfaces alert_id, severity, metric_name, value.
func stubDiagnosisFromAlert(alert map[string]any, model string) *Diagnosis {
	alertID, _ := alert["alert_id"].(string)
	severity, _ := alert["severity"].(string)
	resourceType, _ := alert["resource_type"].(string)
	metricName, metricValue := "", ""
	if m, ok := alert["metric"].(map[string]any); ok {
		metricName, _ = m["metric_name"].(string)
		metricValue = fmt.Sprintf("%v", m["value"])
	}
	root := "unknown"
	if metricName != "" {
		root = fmt.Sprintf("alert on %s (%s) value=%v", metricName, severity, metricValue)
	}
	return &Diagnosis{
		RootCause: root,
		Recommendations: []Recommendation{
			{Action: fmt.Sprintf("investigate %s alert %s", resourceType, alertID)},
		},
		Confidence:    "low",
		Caveats:       []string{"M1 stub: no API key configured"},
		Model:         model,
		PromptVersion: PromptVersion,
	}
}

func parseDiagnosis(text string) (*Diagnosis, error) {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()

	var diagnosis Diagnosis
	if err := decoder.Decode(&diagnosis); err != nil {
		return nil, fmt.Errorf("decode diagnosis JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decode diagnosis JSON: unexpected trailing value")
		}
		return nil, fmt.Errorf("decode diagnosis JSON trailing content: %w", err)
	}
	if diagnosis.RootCause == "" {
		return nil, fmt.Errorf("decode diagnosis JSON: root_cause is required")
	}
	switch diagnosis.Confidence {
	case "high", "medium", "low":
	default:
		return nil, fmt.Errorf("decode diagnosis JSON: invalid confidence %q", diagnosis.Confidence)
	}
	if diagnosis.Recommendations == nil {
		diagnosis.Recommendations = []Recommendation{}
	}
	if diagnosis.EvidenceChains == nil {
		diagnosis.EvidenceChains = []EvidenceChain{}
	}
	if diagnosis.Caveats == nil {
		diagnosis.Caveats = []string{}
	}
	return &diagnosis, nil
}
