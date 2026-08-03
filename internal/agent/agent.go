// Package agent is the AI Cloud Ops diagnosis agent. Stub created so that
// the eval framework (internal/eval) compiles in isolation; the production
// implementation will replace this file.
package agent

// Diagnosis is the AI agent's output for a single alert.
type Diagnosis struct {
	RootCause       string   `json:"root_cause"`
	Recommendation  string   `json:"recommendation"`
	Evidence        []string `json:"evidence,omitempty"`
	ReasoningChain  []string `json:"reasoning_chain,omitempty"`
	ResponseTimeMS  int64    `json:"response_time_ms,omitempty"`
	Confidence      float64  `json:"confidence,omitempty"`
	AlertID         string   `json:"alert_id,omitempty"`
}

// Client diagnoses alerts. The real implementation uses the Anthropic SDK;
// this stub is sufficient for eval tests.
type Client struct{}

// NewClient returns a stub diagnosis client.
func NewClient(_ string) *Client { return &Client{} }

// Diagnose returns a stub Diagnosis. Replace with real impl.
func (c *Client) Diagnose(_ any) *Diagnosis {
	return &Diagnosis{
		RootCause:      "stub",
		Recommendation: "stub",
		AlertID:        "stub",
	}
}
