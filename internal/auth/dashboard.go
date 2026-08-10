package auth

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)


const sessionCookieName = "gnat_session"


type DashboardAuth struct {
	password string
	sessions *SessionStore


	secureCookies bool
}


func NewDashboardAuth(password string, sessions *SessionStore, publicURL string) *DashboardAuth {
	return &DashboardAuth{
		password:      password,
		sessions:      sessions,
		secureCookies: !isLocalURL(publicURL),
	}
}


func isLocalURL(publicURL string) bool {
	lower := strings.ToLower(publicURL)
	return strings.HasPrefix(lower, "http://localhost") ||
		strings.HasPrefix(lower, "http://127.0.0.1")
}

type loginRequest struct {
	Password string `json:"password"`
}


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


func (a *DashboardAuth) SessionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authenticated := a.isAuthenticated(r)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"authenticated": authenticated})
	}
}


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
	
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
