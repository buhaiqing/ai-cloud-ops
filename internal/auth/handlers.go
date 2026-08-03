// Handlers: login, logout, me.
//
// Login validates against env-configured admin creds and issues a session
// cookie + CSRF cookie. Logout revokes the session and clears both cookies.
// Me returns the current user — used by the frontend to gate UI.
package auth

import (
	"encoding/json"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Handlers wires the auth store + configured credentials.
type Handlers struct {
	Store    *Store
	User     string // expected login user (AICO_ADMIN_USER)
	PassHash string // bcrypt hash (AICO_ADMIN_PASS_HASH)
}

// NewHandlers constructs the handler set.
func NewHandlers(store *Store, user, passHash string) *Handlers {
	return &Handlers{Store: store, User: user, PassHash: passHash}
}

type loginBody struct {
	User string `json:"user"`
	Pass string `json:"pass"`
}

// Login handles POST /api/v1/auth/login. Always constant-time on the password
// side (bcrypt); never reveals which field was wrong to avoid user enumeration.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var body loginBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
		body.User == "" || body.Pass == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Compare user with bcrypt-comparable constant-time path.
	// bcrypt.CompareHashAndPassword is itself constant-time on the hash side;
	// we use it for both bad-user (compare against a precomputed dummy hash)
	// and bad-password to keep timing roughly even.
	userOK := body.User == h.User
	passHash := h.PassHash
	if !userOK {
		// Compare against the dummy hash so we spend similar time on both branches.
		passHash = dummyHash
	}
	passErr := bcrypt.CompareHashAndPassword([]byte(passHash), []byte(body.Pass))
	if !userOK || passErr != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	sess, err := h.Store.Issue(body.User)
	if err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	setSessionCookies(w, &sess, h.Store.Secure())
	writeJSON(w, http.StatusOK, map[string]any{"user": sess.User})
}

// Logout handles POST /api/v1/auth/logout. Revokes the session (if any) and
// clears the cookies. Always 204.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		h.Store.Revoke(c.Value)
	}
	clearSessionCookies(w, h.Store.Secure())
	w.WriteHeader(http.StatusNoContent)
}

// Me handles GET /api/v1/auth/me. Reads the session injected by middleware
// and returns the user. If middleware isn't mounted (or session invalid),
// returns 401.
func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	sess, ok := FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": sess.User})
}

// --- cookie helpers ---

func setSessionCookies(w http.ResponseWriter, s *Session, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    s.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  s.CreatedAt.Add(24 * time.Hour),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    s.CSRF,
		Path:     "/",
		HttpOnly: false, // JS must read this for the double-submit header.
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  s.CreatedAt.Add(24 * time.Hour),
	})
}

func clearSessionCookies(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// dummyHash is a precomputed bcrypt hash used for timing equalisation on the
// bad-user branch. It's the hash of "" (empty password) — never matches
// anything real. Generated at startup.
var dummyHash string

func init() {
	h, err := bcrypt.GenerateFromPassword([]byte(""), bcrypt.MinCost)
	if err == nil {
		dummyHash = string(h)
	}
}