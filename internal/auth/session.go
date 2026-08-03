// Package auth: server-side sessions, CSRF, login/logout/me handlers (M2-5).
//
// Storage is an in-memory map — fine for MVP single-process. Upgrade path:
// swap Store for a Redis-backed implementation behind the same interface when
// we need to scale beyond one Go process or survive restarts.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

// CookieName is the HttpOnly session cookie name.
const CookieName = "aico_session"

// CSRFHeader is the request header carrying the CSRF token on state-changing calls.
const CSRFHeader = "X-CSRF-Token"

// CSRFCookieName is the readable CSRF cookie (double-submit pattern).
const CSRFCookieName = "aico_csrf"

// Session is a server-side session record.
type Session struct {
	ID        string    // 32 random bytes, hex-encoded
	User      string    // login name
	CreatedAt time.Time // server-side timestamp
	CSRF      string    // 16 random bytes, hex-encoded
}

// Store is an in-memory session registry, safe for concurrent use.
type Store struct {
	mu        sync.RWMutex
	sessions  map[string]Session
	cookieSec bool // Set Secure flag on cookies
}

// NewStore constructs an empty Store. secure=true adds the Secure flag to
// cookies (only behind TLS).
func NewStore(secure bool) *Store {
	return &Store{
		sessions:  map[string]Session{},
		cookieSec: secure,
	}
}

// Issue creates a new session for user with a random ID + CSRF token.
func (s *Store) Issue(user string) (Session, error) {
	id, err := randHex(32) // 64-char hex string
	if err != nil {
		return Session{}, err
	}
	csrf, err := randHex(16) // 32-char hex string
	if err != nil {
		return Session{}, err
	}
	sess := Session{ID: id, User: user, CreatedAt: time.Now().UTC(), CSRF: csrf}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
	return sess, nil
}

// Get looks up a session by ID. The bool is false if the ID is unknown.
func (s *Store) Get(id string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

// Revoke removes a session. Missing IDs are a no-op.
func (s *Store) Revoke(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

// Secure reports whether cookies should set the Secure flag.
func (s *Store) Secure() bool { return s.cookieSec }

// --- middleware ---

type ctxKey struct{ name string }

var sessionKey = ctxKey{name: "auth.session"}

// FromContext extracts the Session injected by Middleware. ok=false when
// middleware was bypassed (e.g. on public paths).
func FromContext(ctx context.Context) (Session, bool) {
	v, ok := ctx.Value(sessionKey).(Session)
	return v, ok
}

// Middleware enforces auth + CSRF on protected routes. publicPaths lists the
// routes that bypass the check entirely (ping, login, stats, ws, ...).
// Routes NOT in publicPaths require:
//   - a valid session cookie (aico_session), otherwise 401
//   - for POST/PUT/DELETE: a matching CSRF token in X-CSRF-Token, otherwise 403
func Middleware(store *Store, publicPaths map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if publicPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			c, err := r.Cookie(CookieName)
			if err != nil || c.Value == "" {
				http.Error(w, "unauthenticated", http.StatusUnauthorized)
				return
			}
			sess, ok := store.Get(c.Value)
			if !ok {
				http.Error(w, "invalid session", http.StatusUnauthorized)
				return
			}
			if isStateChanging(r.Method) {
				tok := r.Header.Get(CSRFHeader)
				if tok == "" || tok != sess.CSRF {
					http.Error(w, "csrf mismatch", http.StatusForbidden)
					return
				}
			}
			ctx := context.WithValue(r.Context(), sessionKey, sess)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func isStateChanging(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		return true
	}
	return false
}

func randHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}