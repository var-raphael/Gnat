package auth

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// sessionCookieName is the cookie carrying the dashboard session token.
const sessionCookieName = "gnat_session"

// DashboardAuth wires the configured dashboard password to a
// SessionStore, and exposes the login/logout handlers and gating
// middleware needed to protect the dashboard and its stats endpoints.
type DashboardAuth struct {
	password string
	sessions *SessionStore

	// secureCookies controls the cookie's Secure flag. Browsers refuse
	// to send Secure cookies over plain http://, so this is turned off
	// automatically for local http://localhost / http://127.0.0.1
	// deployments and left on for everything else, since any real
	// deployment should be behind https regardless.
	secureCookies bool
}

// NewDashboardAuth builds a DashboardAuth. publicURL is the configured
// GNAT_PUBLIC_URL, used only to decide whether the session cookie can
// safely require https (see secureCookies above).
func NewDashboardAuth(password string, sessions *SessionStore, publicURL string) *DashboardAuth {
	return &DashboardAuth{
		password:      password,
		sessions:      sessions,
		secureCookies: !isLocalURL(publicURL),
	}
}

// isLocalURL reports whether publicURL points at localhost/127.0.0.1
// over plain http, the one case where requiring a Secure cookie would
// silently break login rather than add real protection.
func isLocalURL(publicURL string) bool {
	lower := strings.ToLower(publicURL)
	return strings.HasPrefix(lower, "http://localhost") ||
		strings.HasPrefix(lower, "http://127.0.0.1")
}

type loginRequest struct {
	Password string `json:"password"`
}

// LoginHandler returns POST /api/dashboard/login. On a correct password
// it issues a session cookie; on failure it returns 401 without
// revealing anything about why (no distinction between "no such
// session" and "wrong password" is ever exposed to the client).
func (a *DashboardAuth) LoginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}

		// Constant-time comparison: a password check is a place where
		// timing differences between "matched the first byte" and
		// "matched the whole string" could in principle leak
		// information, so this avoids the naive == comparison here
		// even though the practical risk for a self-hosted single-user
		// tool is low.
		if !constantTimeEqual(req.Password, a.password) {
			http.Error(w, "incorrect password", http.StatusUnauthorized)
			return
		}

		token, err := a.sessions.Create()
		if err != nil {
			http.Error(w, "failed to create session", http.StatusInternalServerError)
			return
		}

		a.setSessionCookie(w, token)
		w.WriteHeader(http.StatusNoContent)
	}
}

// LogoutHandler returns POST /api/dashboard/logout. Revokes the
// session server-side and clears the cookie client-side. Safe to call
// even with no active session.
func (a *DashboardAuth) LogoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if cookie, err := r.Cookie(sessionCookieName); err == nil {
			a.sessions.Revoke(cookie.Value)
		}
		a.clearSessionCookie(w)
		w.WriteHeader(http.StatusNoContent)
	}
}

// SessionHandler returns GET /api/dashboard/session, used by the
// frontend on load to ask "am I currently logged in?" rather than
// trusting a client-side flag it could set itself.
func (a *DashboardAuth) SessionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authenticated := a.isAuthenticated(r)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"authenticated": authenticated})
	}
}

// RequireSession wraps a handler so it only runs for requests carrying
// a valid session cookie. Everything else gets a 401 with no body
// beyond a short message, since this guards both HTML (dashboard page)
// and JSON (stats endpoints) routes alike.
func (a *DashboardAuth) RequireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.isAuthenticated(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (a *DashboardAuth) isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	return a.sessions.Validate(cookie.Value)
}

func (a *DashboardAuth) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(sessionTTL),
	})
}

func (a *DashboardAuth) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func constantTimeEqual(a, b string) bool {
	// subtle.ConstantTimeCompare requires equal-length inputs to be
	// meaningful; a length mismatch is itself not a useful timing
	// signal here (password lengths aren't secret), so it's fine to
	// short-circuit on that before the constant-time byte comparison.
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
