// Tests for auth/handlers.go — login/logout/me.
package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

// testDeps builds a *Handlers wired against a fresh Store and the given creds.
func testDeps(user, pass string) (*Handlers, *Store) {
	store := NewStore(false)
	h, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.MinCost)
	if err != nil {
		panic(err)
	}
	return NewHandlers(store, user, string(h)), store
}

func mountAuth(h *Handlers) http.Handler {
	return mountAuthWithMW(h, nil)
}

func mountAuthWithMW(h *Handlers, store *Store) http.Handler {
	r := chi.NewRouter()
	// Login + logout are public; everything else requires middleware.
	public := map[string]bool{
		"/api/v1/auth/login":  true,
		"/api/v1/auth/logout": true,
	}
	r.Post("/api/v1/auth/login", h.Login)
	r.Post("/api/v1/auth/logout", h.Logout)
	if store != nil {
		r.Group(func(protected chi.Router) {
			protected.Use(Middleware(store, public))
			protected.Get("/api/v1/auth/me", h.Me)
		})
	} else {
		r.Get("/api/v1/auth/me", h.Me)
	}
	return r
}

func TestLogin_SuccessSetsCookie(t *testing.T) {
	h, store := testDeps("admin", "s3cret")
	r := mountAuth(h)

	body := bytes.NewBufferString(`{"user":"admin","pass":"s3cret"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp["user"] != "admin" {
		t.Fatalf("expected user=admin, got %v", resp)
	}
	var sessCookie, csrfCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		switch c.Name {
		case CookieName:
			sessCookie = c
		case CSRFCookieName:
			csrfCookie = c
		}
	}
	if sessCookie == nil || sessCookie.Value == "" {
		t.Fatalf("missing aico_session cookie: %+v", rr.Result().Cookies())
	}
	if !sessCookie.HttpOnly {
		t.Errorf("session cookie must be HttpOnly")
	}
	if sessCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("session cookie SameSite = %v, want Lax", sessCookie.SameSite)
	}
	if csrfCookie == nil || csrfCookie.Value == "" {
		t.Fatalf("missing aico_csrf cookie")
	}
	if csrfCookie.HttpOnly {
		t.Errorf("CSRF cookie must be JS-readable (double-submit)")
	}
	if _, ok := store.Get(sessCookie.Value); !ok {
		t.Errorf("session must be stored server-side")
	}
	if csrfCookie.Value != "" && len(csrfCookie.Value) < 16 {
		t.Errorf("CSRF cookie too short: %q", csrfCookie.Value)
	}
}

func TestLogin_BadPasswordReturns401(t *testing.T) {
	h, _ := testDeps("admin", "s3cret")
	r := mountAuth(h)
	body := bytes.NewBufferString(`{"user":"admin","pass":"wrong"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	// Body must NOT distinguish between bad user vs bad password.
	if strings.Contains(strings.ToLower(rr.Body.String()), "user") {
		t.Errorf("error message reveals 'user': %s", rr.Body.String())
	}
}

func TestLogin_BadUserReturns401(t *testing.T) {
	h, _ := testDeps("admin", "s3cret")
	r := mountAuth(h)
	body := bytes.NewBufferString(`{"user":"nobody","pass":"s3cret"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestLogin_MissingFieldsReturns400(t *testing.T) {
	h, _ := testDeps("admin", "s3cret")
	r := mountAuth(h)
	cases := []string{
		`{"user":"admin"}`,
		`{"pass":"s3cret"}`,
		`{}`,
		`not-json`,
	}
	for _, body := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("body %q: expected 400, got %d", body, rr.Code)
		}
	}
}

func TestLogin_AllowsThroughWithoutCSRF(t *testing.T) {
	// Login is in the public path — middleware isn't even mounted here.
	// This confirms the handler itself doesn't reject due to CSRF (handled by middleware).
	h, _ := testDeps("admin", "s3cret")
	r := mountAuth(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"user":"admin","pass":"s3cret"}`))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestLogout_ClearsCookieAndRevokesSession(t *testing.T) {
	h, store := testDeps("admin", "s3cret")
	r := mountAuth(h)

	// Log in first to obtain a real session.
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"user":"admin","pass":"s3cret"}`))
	loginRR := httptest.NewRecorder()
	r.ServeHTTP(loginRR, loginReq)
	if loginRR.Code != http.StatusOK {
		t.Fatalf("login setup failed: %d", loginRR.Code)
	}
	var sessionID string
	for _, c := range loginRR.Result().Cookies() {
		if c.Name == CookieName {
			sessionID = c.Value
		}
	}
	if sessionID == "" {
		t.Fatalf("login did not issue session cookie")
	}
	if _, ok := store.Get(sessionID); !ok {
		t.Fatalf("session not in store after login")
	}

	// Now log out.
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutReq.AddCookie(&http.Cookie{Name: CookieName, Value: sessionID})
	logoutReq.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: ""}) // populated below
	// Pull CSRF cookie value from login response so we can match the header.
	var csrfValue string
	for _, c := range loginRR.Result().Cookies() {
		if c.Name == CSRFCookieName {
			csrfValue = c.Value
		}
	}
	if csrfValue != "" {
		logoutReq.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrfValue})
		logoutReq.Header.Set(CSRFHeader, csrfValue)
	}

	logoutRR := httptest.NewRecorder()
	r.ServeHTTP(logoutRR, logoutReq)
	if logoutRR.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", logoutRR.Code, logoutRR.Body.String())
	}
	// Session should be revoked.
	if _, ok := store.Get(sessionID); ok {
		t.Errorf("session should be revoked after logout")
	}
	// Cookie should be cleared.
	var clearCookie *http.Cookie
	for _, c := range logoutRR.Result().Cookies() {
		if c.Name == CookieName {
			clearCookie = c
		}
	}
	if clearCookie == nil {
		t.Fatalf("logout did not set clearing cookie")
	}
	if clearCookie.MaxAge >= 0 && clearCookie.Value != "" {
		t.Errorf("expected clear cookie (max-age<0 or empty value), got MaxAge=%d Value=%q", clearCookie.MaxAge, clearCookie.Value)
	}
}

func TestMe_RequiresSession(t *testing.T) {
	h, store := testDeps("admin", "s3cret")
	r := mountAuthWithMW(h, store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without session, got %d", rr.Code)
	}
}

func TestMe_ReturnsCurrentUser(t *testing.T) {
	h, store := testDeps("admin", "s3cret")
	r := mountAuthWithMW(h, store)
	sess, _ := store.Issue("admin")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sess.ID})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp["user"] != "admin" {
		t.Fatalf("expected user=admin, got %v", resp)
	}
}