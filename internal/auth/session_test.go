// Tests for auth/session.go — Store + Middleware.
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- Store unit tests ---

func TestStore_IssueCreatesUniqueIDs(t *testing.T) {
	s := NewStore(false)
	a, err := s.Issue("alice")
	if err != nil {
		t.Fatalf("issue failed: %v", err)
	}
	b, err := s.Issue("alice")
	if err != nil {
		t.Fatalf("issue failed: %v", err)
	}
	if a.ID == b.ID {
		t.Fatalf("expected unique IDs, got %q twice", a.ID)
	}
	if a.CSRF == b.CSRF {
		t.Fatalf("expected unique CSRF tokens, got %q twice", a.CSRF)
	}
	if len(a.ID) == 0 || len(a.CSRF) == 0 {
		t.Fatalf("ID and CSRF must be non-empty, got ID=%q CSRF=%q", a.ID, a.CSRF)
	}
}

func TestStore_GetReturnsIssuedSession(t *testing.T) {
	s := NewStore(false)
	issued, _ := s.Issue("alice")
	got, ok := s.Get(issued.ID)
	if !ok {
		t.Fatalf("Get(%q) not found", issued.ID)
	}
	if got.User != "alice" {
		t.Errorf("User = %q, want alice", got.User)
	}
	if got.CSRF != issued.CSRF {
		t.Errorf("CSRF = %q, want %q", got.CSRF, issued.CSRF)
	}
}

func TestStore_GetMissingReturnsFalse(t *testing.T) {
	s := NewStore(false)
	if _, ok := s.Get("nope"); ok {
		t.Fatalf("expected Get(missing) to return false")
	}
}

func TestStore_RevokeRemoves(t *testing.T) {
	s := NewStore(false)
	issued, _ := s.Issue("alice")
	s.Revoke(issued.ID)
	if _, ok := s.Get(issued.ID); ok {
		t.Fatalf("expected session to be revoked")
	}
}

// --- Middleware tests ---

func newRouter(store *Store, public map[string]bool, protected http.HandlerFunc) http.Handler {
	mux := http.NewServeMux()
	for p := range public {
		p := p
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			protected(w, r)
		})
	}
	mux.HandleFunc("/", protected)
	return Middleware(store, public)(mux)
}

func TestMiddleware_AllowsPublicPaths(t *testing.T) {
	store := NewStore(false)
	public := map[string]bool{
		"/api/v1/ping":          true,
		"/api/v1/stats":         true,
		"/api/v1/auth/login":    true,
	}
	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/ping"},
		{http.MethodGet, "/api/v1/stats"},
		{http.MethodPost, "/api/v1/auth/login"},
	}
	for _, c := range cases {
		r := newRouter(store, public, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot) // arbitrary marker
		})
		req := httptest.NewRequest(c.method, c.path, nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusTeapot {
			t.Errorf("%s %s: expected handler to run (418), got %d", c.method, c.path, rr.Code)
		}
	}
}

func TestMiddleware_BlocksUnauthenticatedOnProtected(t *testing.T) {
	store := NewStore(false)
	public := map[string]bool{
		"/api/v1/ping": true,
	}
	r := newRouter(store, public, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("handler should NOT run for unauthenticated protected request")
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMiddleware_AcceptsValidSessionCookie(t *testing.T) {
	store := NewStore(false)
	public := map[string]bool{"/api/v1/ping": true}
	issued, _ := store.Issue("alice")

	r := newRouter(store, public, func(w http.ResponseWriter, r *http.Request) {
		sess, ok := FromContext(r.Context())
		if !ok {
			t.Fatalf("expected session in context")
		}
		if sess.User != "alice" {
			t.Errorf("context user = %q, want alice", sess.User)
		}
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: issued.ID})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestMiddleware_RejectsBadCSRFOnPost(t *testing.T) {
	store := NewStore(false)
	public := map[string]bool{"/api/v1/ping": true}
	issued, _ := store.Issue("alice")

	called := false
	r := newRouter(store, public, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rules", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: issued.ID})
	req.Header.Set(CSRFHeader, "wrong-token")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
	if called {
		t.Fatalf("handler should not run with bad CSRF")
	}
}

func TestMiddleware_AcceptsValidCSRFOnPost(t *testing.T) {
	store := NewStore(false)
	public := map[string]bool{"/api/v1/ping": true}
	issued, _ := store.Issue("alice")

	called := false
	r := newRouter(store, public, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rules", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: issued.ID})
	req.Header.Set(CSRFHeader, issued.CSRF)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatalf("handler should have run")
	}
}

func TestMiddleware_GetDoesNotRequireCSRF(t *testing.T) {
	store := NewStore(false)
	public := map[string]bool{"/api/v1/ping": true}
	issued, _ := store.Issue("alice")

	called := false
	r := newRouter(store, public, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: issued.ID})
	// No CSRF header on a GET — must succeed.
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !called {
		t.Fatalf("handler should have run for GET without CSRF")
	}
}