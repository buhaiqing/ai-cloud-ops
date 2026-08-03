// M2-6: Incident lifecycle state machine.
//
// States: open → acknowledged → (suppressed | maintenance | resolved) → open (via replay)
//
// Transitions (must validate before applying):
//   open          → acknowledged    via /ack
//   acknowledged  → suppressed      via /suppress
//   acknowledged  → maintenance     via /maintenance
//   acknowledged  → resolved        via /resolve
//   suppressed    → open            via /replay
//   maintenance   → open            via /replay
//   resolved      → open            via /replay
//
// Illegal transitions (e.g. open → resolved directly) return 409 Conflict.

package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// validTransitions encodes the legal state graph. Pure data, exported for tests.
var validTransitions = map[string]map[string]bool{
	"open": {
		"acknowledged": true,
	},
	"acknowledged": {
		"suppressed":  true,
		"maintenance": true,
		"resolved":    true,
	},
	"suppressed": {
		"open": true,
	},
	"maintenance": {
		"open": true,
	},
	"resolved": {
		"open": true,
	},
}

// ErrIllegalTransition is returned when a transition is not allowed.
var ErrIllegalTransition = errors.New("illegal state transition")

// canTransition reports whether from→to is allowed.
func canTransition(from, to string) bool {
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	return allowed[to]
}

// incidentTransitionHandler returns a handler that moves an alert to the
// given target state, validating the transition first.
func incidentTransitionHandler(deps *Deps, target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps == nil || deps.Pool == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "db unavailable"})
			return
		}
		id := chi.URLParam(r, "id")
		// Read current state.
		var current string
		err := deps.Pool.QueryRow(r.Context(),
			`SELECT status FROM alerts WHERE id=$1`, id).Scan(&current)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "alert not found"})
			return
		}
		if current == target {
			// Idempotent: already in target state, return 200.
			writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": target, "noop": true})
			return
		}
		if !canTransition(current, target) {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":  ErrIllegalTransition.Error(),
				"from":   current,
				"to":     target,
				"allowed": validTransitions[current],
			})
			return
		}
		_, err = deps.Pool.Exec(r.Context(),
			`UPDATE alerts SET status=$1, updated_at=now() WHERE id=$2`,
			target, id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": target})
	}
}

// incidentReplayHandler re-runs AI analysis on a previously processed alert.
// For now it just resets status to open; the actual re-analysis is queued by
// the worker (which is wired separately).
func incidentReplayHandler(deps *Deps) http.HandlerFunc {
	return incidentTransitionHandler(deps, "open")
}