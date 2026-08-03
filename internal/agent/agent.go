// Package agent provides the AI diagnosis client and its read-only tool surface.
package agent

// NewClient is a legacy shim kept for internal/eval stubs that previously
// invoked `agent.NewClient("")` with a single string. New callers should use
// New(*pgxpool.Pool, apiKey, model).
func NewClient(_ string) *Client {
	return New(nil, "", "")
}
