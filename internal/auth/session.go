package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// sessionTTL is how long a dashboard session stays valid after its last
// use. It rolls forward on every authenticated request, so an active
// user is never logged out, but a genuinely abandoned session (lost
// device, old browser profile) expires on its own within a week.
const sessionTTL = 7 * 24 * time.Hour

// SessionStore holds active dashboard sessions in memory. There is no
// need for this to survive a restart or be shared across instances: a
// restart forcing re-login is an acceptable, rare inconvenience, and
// Gnat is a single-process binary by design.
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]time.Time // token -> expiresAt
}

func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]time.Time),
	}
}

// Create generates a new random session token and stores it with a
// fresh expiry.
func (s *SessionStore) Create() (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = time.Now().Add(sessionTTL)

	return token, nil
}

// Validate reports whether token corresponds to a live, unexpired
// session. On success it also refreshes the session's expiry, giving
// the rolling-window behavior described on sessionTTL. An expired
// token is removed from the store rather than left to linger.
func (s *SessionStore) Validate(token string) bool {
	if token == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	expiresAt, ok := s.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(expiresAt) {
		delete(s.sessions, token)
		return false
	}

	s.sessions[token] = time.Now().Add(sessionTTL)
	return true
}

// Revoke deletes a session, used on logout. Deleting a token that
// doesn't exist is a harmless no-op, so callers don't need to check
// Validate first.
func (s *SessionStore) Revoke(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
