package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ActionTrailEvent is one recent change/API call from Aliyun ActionTrail.
type ActionTrailEvent struct {
	EventName   string `json:"event_name"`
	ResourceID  string `json:"resource_id"`
	Username    string `json:"username"`
	EventTime   string `json:"event_time"` // RFC3339
	ServiceName string `json:"service_name"`
}

// ActionTrailFetcher returns recent change events near an alert window.
// The production driver (real AK) is deferred; nil fetcher = no context.
type ActionTrailFetcher interface {
	RecentEvents(ctx context.Context, resourceID string, window time.Duration) ([]ActionTrailEvent, error)
}

// DefaultActionTrailWindow is the T16 sliding window.
const DefaultActionTrailWindow = 10 * time.Minute

// attachActionTrail appends recent ActionTrail change events to the diagnosis
// as evidence chains. Best-effort by design: nil fetcher, missing resource_id,
// fetch error, or empty events all leave the diagnosis untouched, so context
// failures never fail the diagnosis itself.
func (c *Client) attachActionTrail(ctx context.Context, d *Diagnosis, alert map[string]any) {
	if c.actionTrail == nil {
		return
	}
	resourceID, _ := alert["resource_id"].(string)
	if resourceID == "" {
		return
	}
	events, err := c.actionTrail.RecentEvents(ctx, resourceID, DefaultActionTrailWindow)
	if err != nil {
		slog.Warn("agent.actiontrail.fetch_failed", "resource_id", resourceID, "err", err)
		return
	}
	if len(events) == 0 {
		return
	}
	for _, ev := range events {
		d.EvidenceChains = append(d.EvidenceChains, EvidenceChain{
			Claim:          fmt.Sprintf("recent change: %s on %s by %s at %s", ev.EventName, ev.ResourceID, ev.Username, ev.EventTime),
			SupportingTool: "lookup_actiontrail_events",
			SupportingData: ev.ServiceName,
		})
	}
	d.Caveats = append(d.Caveats, "actiontrail_context_attached")
}
