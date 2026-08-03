package api

import "testing"

// --- canTransition unit tests (pure state machine) ---

func TestCanTransition_LegalPaths(t *testing.T) {
	legal := []struct{ from, to string }{
		{"open", "acknowledged"},
		{"acknowledged", "suppressed"},
		{"acknowledged", "maintenance"},
		{"acknowledged", "resolved"},
		{"suppressed", "open"},
		{"maintenance", "open"},
		{"resolved", "open"},
	}
	for _, c := range legal {
		if !canTransition(c.from, c.to) {
			t.Errorf("expected %s→%s to be allowed", c.from, c.to)
		}
	}
}

func TestCanTransition_IllegalSkips(t *testing.T) {
	illegal := []struct{ from, to string }{
		{"open", "suppressed"},      // must acknowledge first
		{"open", "maintenance"},
		{"open", "resolved"},
		{"acknowledged", "open"},    // no direct un-ack
		{"suppressed", "acknowledged"}, // must replay to open first
		{"resolved", "suppressed"},
		{"unknown", "open"},
	}
	for _, c := range illegal {
		if canTransition(c.from, c.to) {
			t.Errorf("expected %s→%s to be illegal", c.from, c.to)
		}
	}
}

func TestCanTransition_AllStatesHaveEntry(t *testing.T) {
	// Every state in the graph must have at least one outgoing edge except terminal-only states.
	// Our graph is fully cyclic (everything can return to open), so every state must have
	// at least one transition.
	for state, allowed := range validTransitions {
		if len(allowed) == 0 {
			t.Errorf("state %q has no outgoing transitions — dead-end", state)
		}
	}
}

func TestCanTransition_RoundTripReplay(t *testing.T) {
	// The most important user flow: ack → resolve → replay → ack.
	// After replay the state is "open" so we can ack again.
	for _, path := range [][]string{
		{"open", "acknowledged", "resolved", "open", "acknowledged"},
		{"open", "acknowledged", "suppressed", "open", "acknowledged", "maintenance", "open"},
	} {
		for i := 0; i < len(path)-1; i++ {
			if !canTransition(path[i], path[i+1]) {
				t.Errorf("path broken at %s→%s in %v", path[i], path[i+1], path)
				break
			}
		}
	}
}