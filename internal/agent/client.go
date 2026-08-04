package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
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
	// actionTrail supplies recent change events (M3-1); nil = disabled.
	actionTrail ActionTrailFetcher
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
			pool:  pool,
			model: model,
			stub:  true,
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

// WithActionTrail attaches a change-context fetcher (nil = disabled).
// Returns c for chaining.
func (c *Client) WithActionTrail(f ActionTrailFetcher) *Client {
	c.actionTrail = f
	return c
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
// M3-2 extension fields are optional in the LLM JSON (missing → zero values),
// so M1-shaped responses keep parsing (向后兼容).
type Recommendation struct {
	Action          string `json:"action"`
	Command         string `json:"command"`
	ExpectedOutcome string `json:"expected_outcome"`
	// M3-2: structured safety metadata for the HITL UI.
	Preconditions      []string `json:"preconditions"`
	RollbackCommand    string   `json:"rollback_command"`
	RiskLevel          string   `json:"risk_level"` // low|medium|high|irreversible; "" when absent
	EstimatedDowntimeS int      `json:"estimated_downtime_s"`
}

// PlannedAction is one intercepted write-tool call from a dry run.
type PlannedAction struct {
	ToolName         string   `json:"tool_name"`
	Command          string   `json:"command"`
	TargetResource   string   `json:"target_resource"`
	RiskLevel        string   `json:"risk_level"`
	Rollback         string   `json:"rollback"`
	PreconditionsMet []string `json:"preconditions_met"` // observed state vs required
}

// DryRunResult is the output of DiagnoseDryRun: the diagnosis plus what an
// approved execution would do, without touching any cloud resource.
type DryRunResult struct {
	Diagnosis               Diagnosis       `json:"diagnosis"`
	DryRun                  bool            `json:"dry_run"`
	WouldExecute            []PlannedAction `json:"would_execute"`
	EstimatedTotalDowntimeS int             `json:"estimated_total_downtime_s"`
	BlockedByPolicy         []string        `json:"blocked_by_policy"`
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
	diagnosis, _, _, err := c.runDiagnosis(ctx, alert, false)
	return diagnosis, err
}

// DiagnoseDryRun runs the same prompt/tool loop as Diagnose but intercepts
// WRITE_TOOLS calls into PlannedActions instead of executing them. Tools
// outside both whitelists are recorded in BlockedByPolicy. Stub mode returns
// an alert-derived diagnosis with empty plan slices (no API call).
func (c *Client) DiagnoseDryRun(ctx context.Context, alert map[string]any) (*DryRunResult, error) {
	diagnosis, planned, blocked, err := c.runDiagnosis(ctx, alert, true)
	result := &DryRunResult{
		Diagnosis:               *diagnosis,
		DryRun:                  true,
		WouldExecute:            planned,
		EstimatedTotalDowntimeS: totalEstimatedDowntime(diagnosis.Recommendations),
		BlockedByPolicy:         blocked,
	}
	if result.WouldExecute == nil {
		result.WouldExecute = []PlannedAction{}
	}
	if result.BlockedByPolicy == nil {
		result.BlockedByPolicy = []string{}
	}
	return result, err
}

func totalEstimatedDowntime(recs []Recommendation) int {
	total := 0
	for _, rec := range recs {
		total += rec.EstimatedDowntimeS
	}
	return total
}

// runDiagnosis is the shared prompt/tool loop behind Diagnose and
// DiagnoseDryRun. dryRun=true offers WRITE_TOOLS to the model and intercepts
// their calls instead of executing them.
func (c *Client) runDiagnosis(ctx context.Context, alert map[string]any, dryRun bool) (*Diagnosis, []PlannedAction, []string, error) {
	if c.stub {
		started := time.Now()
		d := stubDiagnosisFromAlert(alert, c.model)
		d.LatencyMs = int(time.Since(started) / time.Millisecond)
		c.attachActionTrail(ctx, d, alert)
		return d, nil, nil, nil
	}
	started := time.Now()
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(BuildUserPrompt(alert))),
	}

	specs := AllToolSpecsForLLM()
	if dryRun {
		specs = AllToolSpecsForLLMWithWrite()
	}
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

	// Dry-run bookkeeping, guarded because tool calls execute concurrently.
	var (
		stateMu       sync.Mutex
		planned       []PlannedAction
		blocked       []string
		observedReads []string
	)

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
			return c.lowConfidence(started, "inference failed"), planned, blocked, wrapped
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
				return c.lowConfidence(started, "inference failed"), planned, blocked, wrapped
			}
			diagnosis.LatencyMs = int(time.Since(started) / time.Millisecond)
			diagnosis.Model = c.model
			diagnosis.PromptVersion = PromptVersion
			c.attachActionTrail(ctx, diagnosis, alert)
			return diagnosis, planned, blocked, nil
		}

		toolResults := make([]anthropic.ContentBlockParamUnion, len(toolUses))
		group, groupCtx := errgroup.WithContext(ctx)
		for i, toolUse := range toolUses {
			group.Go(func() error {
				if err := groupCtx.Err(); err != nil {
					return fmt.Errorf("execute tool %s: %w", toolUse.Name, err)
				}
				readOnly := IsAllowed(toolUse.Name)
				writeTool, isWrite := GetWriteTool(toolUse.Name)
				switch {
				case readOnly:
					if dryRun {
						stateMu.Lock()
						observedReads = append(observedReads, toolUse.Name+" called")
						stateMu.Unlock()
					}
				case dryRun && isWrite:
					// M3-3: intercept — record what WOULD happen, execute nothing.
					stateMu.Lock()
					preconditions := make([]string, len(observedReads))
					copy(preconditions, observedReads)
					planned = append(planned, buildPlannedAction(writeTool, toolUse, preconditions))
					stateMu.Unlock()
					slog.Info("agent.dryrun.captured", "tool", toolUse.Name)
					captured, _ := json.Marshal(map[string]any{
						"status": "dry_run_captured",
						"tool":   toolUse.Name,
						"note":   "action recorded as PlannedAction, not executed",
					})
					toolResults[i] = anthropic.NewToolResultBlock(toolUse.ID, string(captured), false)
					return nil
				default:
					toolErr := ToolNotAllowedError{Name: toolUse.Name}
					slog.Warn("agent.tool.not_allowed", "tool", toolUse.Name)
					if dryRun {
						reason := fmt.Sprintf("tool %s not in WRITE_TOOLS whitelist", toolUse.Name)
						stateMu.Lock()
						if !contains(blocked, reason) {
							blocked = append(blocked, reason)
						}
						stateMu.Unlock()
					}
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
			return c.lowConfidence(started, "inference failed"), planned, blocked, wrapped
		}
		messages = append(messages, anthropic.NewUserMessage(toolResults...))
	}

	diagnosis := c.lowConfidence(started, "tool iteration limit reached")
	diagnosis.RootCause = "diagnosis_truncated"
	return diagnosis, planned, blocked, nil
}

// buildPlannedAction maps one intercepted write-tool call to the contract shape.
// Command carries the compact tool input; preconditions_met lists the
// read-only context gathered earlier in the same run (observed state).
func buildPlannedAction(tool Tool, toolUse anthropic.ToolUseBlock, preconditions []string) PlannedAction {
	var input map[string]any
	_ = json.Unmarshal(toolUse.Input, &input)
	target, _ := input["resource_id"].(string)
	command := strings.TrimSpace(string(toolUse.Input))
	if command == "" || command == "null" {
		command = "{}"
	}
	if preconditions == nil {
		preconditions = []string{}
	}
	return PlannedAction{
		ToolName:         toolUse.Name,
		Command:          command,
		TargetResource:   target,
		RiskLevel:        tool.Risk,
		Rollback:         tool.Rollback,
		PreconditionsMet: preconditions,
	}
}

func contains(list []string, s string) bool {
	return slices.Contains(list, s)
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
	for i := range diagnosis.Recommendations {
		rec := &diagnosis.Recommendations[i]
		switch rec.RiskLevel {
		case "", "low", "medium", "high", "irreversible":
		default:
			return nil, fmt.Errorf("decode diagnosis JSON: invalid risk_level %q", rec.RiskLevel)
		}
		if rec.Preconditions == nil {
			rec.Preconditions = []string{}
		}
	}
	if diagnosis.EvidenceChains == nil {
		diagnosis.EvidenceChains = []EvidenceChain{}
	}
	if diagnosis.Caveats == nil {
		diagnosis.Caveats = []string{}
	}
	return &diagnosis, nil
}
