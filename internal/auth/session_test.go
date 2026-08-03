// Tests for auth/session.go — Store + Middleware.
package auth

import (
	"net/http"
	"net/http/httptest"
	"sync"
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

// --- Concurrency tests (race-prone paths) ---

// TestStore_ConcurrentIssueProducesUniqueIDs — 8 goroutines issue 1000
// sessions each; assert all 8000 IDs are distinct. Catches a degenerate
// randHex that would collide under concurrent crypto/rand calls.
func TestStore_ConcurrentIssueProducesUniqueIDs(t *testing.T) {
	store := NewStore(false)
	const goroutines = 8
	const perG = 1000
	total := goroutines * perG
	ids := make(chan string, total)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				s, err := store.Issue("user")
				if err != nil {
					t.Errorf("issue failed: %v", err)
					return
				}
				ids <- s.ID
			}
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		seen[id] = true
	}
	if len(seen) != total {
		t.Fatalf("got %d unique IDs, want %d", len(seen), total)
	}
}

// TestStore_ConcurrentGetAndRevoke — 4 Get + 4 Revoke goroutines share a
// 1000-session pool. No panics; no torn reads (every successful Get returns
// a Session whose fields match the original Issue).
func TestStore_ConcurrentGetAndRevoke(t *testing.T) {
	store := NewStore(false)
	const N = 1000
	ids := make([]string, N)
	for i := 0; i < N; i++ {
		s, err := store.Issue("user")
		if err != nil {
			t.Fatalf("issue failed: %v", err)
		}
		ids[i] = s.ID
	}
	const iters = 5000
	var wg sync.WaitGroup
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				id := ids[(seed*1337+i)%N]
				sess, ok := store.Get(id)
				if !ok {
					continue
				}
				if sess.ID != id || sess.User != "user" || sess.CSRF == "" || sess.CreatedAt.IsZero() {
					t.Errorf("torn read for id=%s: %+v", id, sess)
					return
				}
			}
		}(r)
	}
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				store.Revoke(ids[(seed*997+i)%N])
			}
		}(w)
	}
	wg.Wait()
	// Post-condition: every session still present is structurally consistent.
	for _, id := range ids {
		sess, ok := store.Get(id)
		if !ok {
			continue
		}
		if sess.ID != id || sess.User != "user" || sess.CSRF == "" || sess.CreatedAt.IsZero() {
			t.Errorf("post-test corrupt session for id=%s: %+v", id, sess)
		}
	}
}

// TestStore_ConcurrentReadMostly — dashboard polling pattern: 10 readers
// hammer Get on a stable 100-ID set while 1 writer revokes + re-issues.
// Invariant: Get never returns a corrupted session (zero fields, ID mismatch).
func TestStore_ConcurrentReadMostly(t *testing.T) {
	store := NewStore(false)
	const N = 100
	const readerLoops = 100
	ids := make([]string, N)
	for i := 0; i < N; i++ {
		s, err := store.Issue("user")
		if err != nil {
			t.Fatalf("issue failed: %v", err)
		}
		ids[i] = s.ID
	}
	var wg sync.WaitGroup
	for r := 0; r < 10; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < readerLoops; i++ {
				for _, id := range ids {
					sess, ok := store.Get(id)
					if !ok {
						continue
					}
					if sess.ID != id || sess.User != "user" || sess.CSRF == "" || sess.CreatedAt.IsZero() {
						t.Errorf("torn read for id=%s: %+v", id, sess)
						return
					}
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			store.Revoke(ids[i%N])
			if _, err := store.Issue("user"); err != nil {
				t.Errorf("writer issue failed: %v", err)
				return
			}
		}
	}()
	wg.Wait()
}

// TestStore_RevokeDuringGet — race Get vs Revoke on the SAME session ID,
// 10000 iterations. Either Get returns ok=false, or ok=true with fully
// valid fields. A torn read would show zero User / zero CreatedAt / ID mismatch.
func TestStore_RevokeDuringGet(t *testing.T) {
	store := NewStore(false)
	for iter := 0; iter < 10000; iter++ {
		issued, err := store.Issue("alice")
		if err != nil {
			t.Fatalf("iter %d: issue failed: %v", iter, err)
		}
		var wg sync.WaitGroup
		var getOK bool
		var gotSess Session
		wg.Add(2)
		go func() {
			defer wg.Done()
			sess, ok := store.Get(issued.ID)
			getOK = ok
			gotSess = sess
		}()
		go func() {
			defer wg.Done()
			store.Revoke(issued.ID)
		}()
		wg.Wait()
		if !getOK {
			continue
		}
		if gotSess.User == "" {
			t.Fatalf("iter %d: empty User in returned session: %+v", iter, gotSess)
		}
		if gotSess.CreatedAt.IsZero() {
			t.Fatalf("iter %d: zero CreatedAt in returned session: %+v", iter, gotSess)
		}
		if gotSess.CSRF == "" {
			t.Fatalf("iter %d: empty CSRF in returned session: %+v", iter, gotSess)
		}
		if gotSess.ID != issued.ID {
			t.Fatalf("iter %d: ID mismatch: got %q want %q", iter, gotSess.ID, issued.ID)
		}
	}
}

// TestStore_HighContentionCSRFUniqueness — 10000 Issue calls fanned across
// 8 goroutines; assert every CSRF token is unique.
func TestStore_HighContentionCSRFUniqueness(t *testing.T) {
	store := NewStore(false)
	const total = 10000
	const goroutines = 8
	const perG = total / goroutines
	csrfCh := make(chan string, total)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				s, err := store.Issue("user")
				if err != nil {
					t.Errorf("issue failed: %v", err)
					return
				}
				csrfCh <- s.CSRF
			}
		}()
	}
	wg.Wait()
	close(csrfCh)
	seen := map[string]bool{}
	for c := range csrfCh {
		seen[c] = true
	}
	if len(seen) != total {
		t.Fatalf("got %d unique CSRF tokens, want %d", len(seen), total)
	}
}
