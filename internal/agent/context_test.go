package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// fakeActionTrailFetcher is a same-package test double for ActionTrailFetcher.
type fakeActionTrailFetcher struct {
	mu     sync.Mutex
	calls  int
	gotID  string
	window time.Duration
	events []ActionTrailEvent
	err    error
}

func (f *fakeActionTrailFetcher) RecentEvents(_ context.Context, resourceID string, window time.Duration) ([]ActionTrailEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.gotID = resourceID
	f.window = window
	return f.events, f.err
}

// snapshot returns the recorded call state under the lock.
func (f *fakeActionTrailFetcher) snapshot() (calls int, id string, window time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.gotID, f.window
}

func twoActionTrailEvents() []ActionTrailEvent {
	return []ActionTrailEvent{
		{EventName: "ModifySecurityIps", ResourceID: "rm-abc123", Username: "ops-alice", EventTime: "2026-08-04T09:55:00Z", ServiceName: "Rds"},
		{EventName: "RestartDBInstance", ResourceID: "rm-abc123", Username: "ops-bob", EventTime: "2026-08-04T09:58:00Z", ServiceName: "Rds"},
	}
}

func actionTrailAlert() map[string]any {
	return map[string]any{
		"alert_id":    "alert-at-1",
		"severity":    "critical",
		"resource_id": "rm-abc123",
	}
}

func actionTrailChains(chains []EvidenceChain) []EvidenceChain {
	var out []EvidenceChain
	for _, chain := range chains {
		if chain.SupportingTool == "lookup_actiontrail_events" {
			out = append(out, chain)
		}
	}
	return out
}

func TestClient_WithActionTrail_StubMode(t *testing.T) {
	fake := &fakeActionTrailFetcher{events: twoActionTrailEvents()}
	diagnosis, err := New(nil, "", "claude-test").
		WithActionTrail(fake).
		Diagnose(context.Background(), actionTrailAlert())
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	chains := actionTrailChains(diagnosis.EvidenceChains)
	if len(chains) != 2 {
		t.Fatalf("actiontrail evidence chains = %d, want 2: %#v", len(chains), diagnosis.EvidenceChains)
	}
	wantClaims := []string{
		"recent change: ModifySecurityIps on rm-abc123 by ops-alice at 2026-08-04T09:55:00Z",
		"recent change: RestartDBInstance on rm-abc123 by ops-bob at 2026-08-04T09:58:00Z",
	}
	for i, want := range wantClaims {
		if chains[i].Claim != want {
			t.Fatalf("chain[%d].Claim = %q, want %q", i, chains[i].Claim, want)
		}
		if chains[i].SupportingData != "Rds" {
			t.Fatalf("chain[%d].SupportingData = %q, want Rds", i, chains[i].SupportingData)
		}
	}
	if !contains(diagnosis.Caveats, "actiontrail_context_attached") {
		t.Fatalf("Caveats missing actiontrail marker: %#v", diagnosis.Caveats)
	}
	if calls, id, window := fake.snapshot(); calls != 1 || id != "rm-abc123" || window != DefaultActionTrailWindow {
		t.Fatalf("fetcher snapshot = (%d, %q, %v), want (1, rm-abc123, %v)", calls, id, window, DefaultActionTrailWindow)
	}
}

func TestClient_WithActionTrail_NilFetcher(t *testing.T) {
	cases := []struct {
		name   string
		client *Client
	}{
		{"no WithActionTrail call", New(nil, "", "claude-test")},
		{"WithActionTrail(nil)", New(nil, "", "claude-test").WithActionTrail(nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diagnosis, err := tc.client.Diagnose(context.Background(), actionTrailAlert())
			if err != nil {
				t.Fatalf("Diagnose() error = %v", err)
			}
			if len(diagnosis.EvidenceChains) != 0 {
				t.Fatalf("EvidenceChains = %#v, want none", diagnosis.EvidenceChains)
			}
			if len(diagnosis.Caveats) != 1 || diagnosis.Caveats[0] != "M1 stub: no API key configured" {
				t.Fatalf("Caveats = %#v, want unchanged stub caveat", diagnosis.Caveats)
			}
		})
	}
}

func TestClient_WithActionTrail_FetchError(t *testing.T) {
	fetchErr := errors.New("actiontrail api unavailable")
	cases := []struct {
		name         string
		client       *Client
		baseEvidence int // evidence chains owned by the diagnosis itself
	}{
		{
			name:   "stub mode",
			client: New(nil, "", "claude-test").WithActionTrail(&fakeActionTrailFetcher{err: fetchErr}),
		},
		{
			name: "mock inference success branch",
			client: testClient(&mockMessageClient{responses: []*anthropic.Message{textResponse(validDiagnosisJSON())}}).
				WithActionTrail(&fakeActionTrailFetcher{err: fetchErr}),
			baseEvidence: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diagnosis, err := tc.client.Diagnose(context.Background(), actionTrailAlert())
			if err != nil {
				t.Fatalf("Diagnose() error = %v, want success despite fetch error", err)
			}
			if len(diagnosis.EvidenceChains) != tc.baseEvidence {
				t.Fatalf("EvidenceChains = %d, want %d (no actiontrail append): %#v",
					len(diagnosis.EvidenceChains), tc.baseEvidence, diagnosis.EvidenceChains)
			}
			if contains(diagnosis.Caveats, "actiontrail_context_attached") {
				t.Fatalf("Caveats must not carry marker on fetch error: %#v", diagnosis.Caveats)
			}
		})
	}
}

func TestClient_WithActionTrail_NoResourceID(t *testing.T) {
	cases := []struct {
		name  string
		alert map[string]any
	}{
		{"missing resource_id", map[string]any{"alert_id": "alert-at-2", "severity": "critical"}},
		{"empty resource_id", map[string]any{"alert_id": "alert-at-3", "resource_id": ""}},
		{"non-string resource_id", map[string]any{"alert_id": "alert-at-4", "resource_id": 42}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeActionTrailFetcher{events: twoActionTrailEvents()}
			diagnosis, err := New(nil, "", "claude-test").
				WithActionTrail(fake).
				Diagnose(context.Background(), tc.alert)
			if err != nil {
				t.Fatalf("Diagnose() error = %v", err)
			}
			if calls, _, _ := fake.snapshot(); calls != 0 {
				t.Fatalf("fetcher calls = %d, want 0", calls)
			}
			if len(diagnosis.EvidenceChains) != 0 {
				t.Fatalf("EvidenceChains = %#v, want none", diagnosis.EvidenceChains)
			}
			if contains(diagnosis.Caveats, "actiontrail_context_attached") {
				t.Fatalf("Caveats must not carry marker without resource_id: %#v", diagnosis.Caveats)
			}
		})
	}
}

func TestClient_WithActionTrail_EmptyEvents(t *testing.T) {
	fake := &fakeActionTrailFetcher{events: nil}
	diagnosis, err := New(nil, "", "claude-test").
		WithActionTrail(fake).
		Diagnose(context.Background(), actionTrailAlert())
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if calls, id, window := fake.snapshot(); calls != 1 || id != "rm-abc123" || window != DefaultActionTrailWindow {
		t.Fatalf("fetcher snapshot = (%d, %q, %v), want (1, rm-abc123, %v)", calls, id, window, DefaultActionTrailWindow)
	}
	if len(diagnosis.EvidenceChains) != 0 {
		t.Fatalf("EvidenceChains = %#v, want none", diagnosis.EvidenceChains)
	}
	if contains(diagnosis.Caveats, "actiontrail_context_attached") {
		t.Fatalf("Caveats must not carry marker for empty events: %#v", diagnosis.Caveats)
	}
}
