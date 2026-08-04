package ingest

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// dailySalt provides a random salt that rotates once every 24 hours, used
// to hash visitor IPs for a coarse "unique visitors" signal without ever
// storing the raw IP or letting a hash be correlated across days.
type dailySalt struct {
	mu        sync.Mutex
	value     string
	expiresAt time.Time
}

var currentSalt = &dailySalt{}

// current returns today's salt, generating a fresh one if the previous
// salt has expired (or none exists yet).
func (s *dailySalt) current() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if time.Now().After(s.expiresAt) {
		s.value = generateSalt()
		s.expiresAt = time.Now().Add(24 * time.Hour)
	}
	return s.value
}

func generateSalt() string {
	buf := make([]byte, 16)
	// crypto/rand.Read practically never fails on supported platforms; if
	// it somehow does, fall back to a time-derived value rather than
	// panicking, since this is a secondary signal, not critical-path auth.
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))
	}
	return hex.EncodeToString(buf)
}

// hashVisitorIP returns a salted, one-way hash of an IP address. The salt
// rotates daily, so the same visitor produces a different hash each day,
// making the hash unusable for tracking a visitor across days even if it
// were ever exposed. The raw IP is never stored, only passed here in
// memory and discarded after hashing.
func hashVisitorIP(ip string) string {
	if ip == "" {
		return ""
	}
	salt := currentSalt.current()
	sum := sha256.Sum256([]byte(salt + ip))
	return hex.EncodeToString(sum[:])
}
