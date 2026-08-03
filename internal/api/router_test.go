// Package api provides the HTTP REST surface for the Web Dashboard (M2-7).
//
// Endpoints (all under /api/v1):
//   GET    /accounts             list account aliases
//   GET    /resources            list resources, filter by ?account&region&type
//   GET    /alerts               list alerts, filter by ?account&status&limit
//   GET    /alerts/{id}          alert detail + linked analyses
//   GET    /analyses/{id}        single AI analysis detail
//   GET    /rules                list alert rules
//   POST   /rules                create rule
//   PUT    /rules/{id}           update rule
//   DELETE /rules/{id}           delete rule
//   POST   /incidents/{id}/ack   acknowledge (state machine — M2-6)
//   POST   /incidents/{id}/suppress
//   POST   /incidents/{id}/maintenance
//   POST   /incidents/{id}/replay
//   GET    /stats                aggregate counts (M2-9)
//   GET    /ws                   WebSocket upgrade (M2-8)
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// --- M2-7 tests: routes & JSON contract ---

func TestRoutes_RootPathsReturn405ForPostWithoutRoute(t *testing.T) {
	// Sanity: a router with no handlers mounted should return 404 for /api/v1/accounts.
	r := chi.NewRouter()
	mountRoutes(r, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts", strings.NewReader("{}"))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound && rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 404 or 405, got %d", rr.Code)
	}
}

func TestRoutes_PingReturns200(t *testing.T) {
	// Health-style smoke: /api/v1/ping is mounted and returns JSON.
	r := chi.NewRouter()
	mountRoutes(r, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["ok"] != true {
		t.Fatalf("expected ok=true, got %v", body)
	}
}

func TestRoutes_StatsReturnsJSONShape(t *testing.T) {
	// Stats endpoint must return at least the documented keys.
	r := chi.NewRouter()
	mountRoutes(r, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	for _, key := range []string{"total_alerts", "open_alerts", "ai_success_rate", "avg_latency_ms", "resources_covered"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("missing key %q in stats response: %v", key, body)
		}
	}
}