// Concurrency tests for the incident lifecycle state machine (M2-6).
//
// Run with `go test -race` to catch data races on the global validTransitions map.
// These tests must remain stdlib-only and must not touch the real DB.

package api

import (
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
)

// allStates is derived from validTransitions so the random-walk tests stay in sync
// if the state graph changes.
var allStates = func() []string {
	keys := make([]string, 0, len(validTransitions))
	for k := range validTransitions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}()

// snapshotKeys returns a sorted copy of validTransitions's top-level keys.
func snapshotKeys() []string {
	keys := make([]string, 0, len(validTransitions))
	for k := range validTransitions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// snapshotFull returns a deep copy of validTransitions's (from,to)→bool entries.
func snapshotFull() map[string]map[string]bool {
	out := make(map[string]map[string]bool, len(validTransitions))
	for k, v := range validTransitions {
		inner := make(map[string]bool, len(v))
		for kk, vv := range v {
			inner[kk] = vv
		}
		out[k] = inner
	}
	return out
}

// TestCanTransition_ConcurrentReadsAreSafe — 16 goroutines each call canTransition
// 10_000 times with random from/to pairs. With -race the run must be clean and the
// validTransitions map shape (top-level key count) must be unchanged afterwards.
func TestCanTransition_ConcurrentReadsAreSafe(t *testing.T) {
	const goroutines = 16
	const iterations = 10_000

	// Pre-generate random (from, to) pairs deterministically.
	rng := rand.New(rand.NewSource(42))
	pairs := make([][2]string, iterations)
	for i := range pairs {
		pairs[i] = [2]string{
			allStates[rng.Intn(len(allStates))],
			allStates[rng.Intn(len(allStates))],
		}
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(start int) {
			defer wg.Done()
			for i := start; i < iterations; i += goroutines {
				_ = canTransition(pairs[i][0], pairs[i][1])
			}
		}(g)
	}
	wg.Wait()

	if got, want := len(validTransitions), len(allStates); got != want {
		t.Fatalf("validTransitions mutated: got %d top-level states, want %d", got, want)
	}
}

// TestCanTransition_ValidTransitionsMapImmutable — snapshot keys before, run concurrent
// reads, snapshot keys after. Both snapshots must be equal.
func TestCanTransition_ValidTransitionsMapImmutable(t *testing.T) {
	before := snapshotKeys()

	const goroutines = 16
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 10_000; i++ {
				for k, v := range validTransitions {
					_ = k
					_ = v
				}
			}
		}()
	}
	wg.Wait()

	after := snapshotKeys()
	if len(before) != len(after) {
		t.Fatalf("validTransitions key count changed: before=%d after=%d (before=%v after=%v)",
			len(before), len(after), before, after)
	}
	for i, k := range before {
		if after[i] != k {
			t.Fatalf("validTransitions keys differ at index %d: before=%q after=%q", i, k, after[i])
		}
	}
}

// TestIncidentTransition_NoRealDBNeeded_ButDocumentContract — when Pool is nil, the
// transition handler must not panic and must not return 500. With the current
// contract it returns 503 ("db unavailable"); a non-nil Pool that errors on
// QueryRow returns 404 ("alert not found"). Either is acceptable here — the
// regression guard is "not 500, not panic".
//
// Regression protection: if someone refactors and accidentally panics on nil pool,
// or removes the nil-check and falls through to QueryRow (which would nil-panic),
// this test catches it.
func TestIncidentTransition_NoRealDBNeeded_ButDocumentContract(t *testing.T) {
	deps := &Deps{Pool: nil}
	handler := incidentTransitionHandler(deps, "acknowledged")

	r := chi.NewRouter()
	r.Post("/incidents/{id}/ack", handler)

	req := httptest.NewRequest(http.MethodPost, "/incidents/123/ack", nil)
	rr := httptest.NewRecorder()

	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Fatalf("handler panicked with nil Pool: %v", rec)
			}
		}()
		r.ServeHTTP(rr, req)
	}()

	if rr.Code == http.StatusInternalServerError {
		t.Fatalf("handler returned 500 with nil Pool (should be a graceful 4xx/5xx): %s", rr.Body.String())
	}
	// Acceptable responses: 503 (db unavailable, nil Pool path) or 404 (alert not found).
	if rr.Code != http.StatusServiceUnavailable && rr.Code != http.StatusNotFound {
		t.Fatalf("expected 503 or 404 with nil Pool, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestStateMachine_RaceHeavyWalk — 8 goroutines each walk a 1000-step random path
// through the state graph. Each step is a valid transition by construction. With
// -race no data races must be detected. This stress-tests the global validTransitions
// map under heavy concurrent read access.
func TestStateMachine_RaceHeavyWalk(t *testing.T) {
	const goroutines = 8
	const steps = 1000

	rng := rand.New(rand.NewSource(42))

	// Pre-compute each path deterministically: at each state pick a random valid
	// next state. The path is guaranteed to be all-legal transitions by construction.
	paths := make([][]string, goroutines)
	for g := range paths {
		path := make([]string, steps+1)
		path[0] = "open"
		for i := 1; i <= steps; i++ {
			allowed := validTransitions[path[i-1]]
			if len(allowed) == 0 {
				t.Fatalf("no outgoing transitions from %q (dead-end in graph)", path[i-1])
			}
			nexts := make([]string, 0, len(allowed))
			for k := range allowed {
				nexts = append(nexts, k)
			}
			sort.Strings(nexts)
			path[i] = nexts[rng.Intn(len(nexts))]
		}
		paths[g] = path
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(idx int) {
			defer wg.Done()
			path := paths[idx]
			for i := 0; i < len(path)-1; i++ {
				if !canTransition(path[i], path[i+1]) {
					t.Errorf("goroutine %d: invalid transition %s→%s at step %d (path=%v)",
						idx, path[i], path[i+1], i, path)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

// TestValidTransitions_ConsistentUnderConcurrency — snapshot all (from,to)→bool entries
// before and after concurrent reads. Must be identical (same keys, same inner key count,
// same values).
func TestValidTransitions_ConsistentUnderConcurrency(t *testing.T) {
	before := snapshotFull()

	const goroutines = 16
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 5000; i++ {
				for from, allowed := range validTransitions {
					for to := range allowed {
						_ = canTransition(from, to)
					}
				}
			}
		}()
	}
	wg.Wait()

	after := snapshotFull()

	if len(before) != len(after) {
		t.Fatalf("top-level key count changed: before=%d after=%d", len(before), len(after))
	}
	for k, v := range before {
		afterInner, ok := after[k]
		if !ok {
			t.Fatalf("missing top-level key %q after concurrent reads", k)
		}
		if len(v) != len(afterInner) {
			t.Fatalf("inner key count changed for %q: before=%d after=%d (before=%v after=%v)",
				k, len(v), len(afterInner), v, afterInner)
		}
		for kk, vv := range v {
			if afterInner[kk] != vv {
				t.Fatalf("value changed for %s→%s: before=%v after=%v", k, kk, vv, afterInner[kk])
			}
		}
	}
}
