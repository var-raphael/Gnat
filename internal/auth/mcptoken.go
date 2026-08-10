package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/storage"
)

// McpTokenStore manages the single active MCP access token, persisted
// in the database (see storage.McpToken) so it survives a restart.
// Unlike SessionStore, there's no in-memory cache: MCP requests are
// expected to be far less frequent than dashboard page loads, so a
// lookup hitting the DB on every call is an acceptable, simple tradeoff
// rather than one more piece of in-memory state to keep in sync.
type McpTokenStore struct {
	db *gorm.DB
}

func NewMcpTokenStore(db *gorm.DB) *McpTokenStore {
	return &McpTokenStore{db: db}
}

// Generate creates a new random token, replacing any existing one, and
// returns the plaintext value. This is the only moment the plaintext
// ever exists outside the caller's immediate response — it is not
// logged and no other method can ever recover it once returned, only
// re-generate a new one that invalidates this one.
func (s *McpTokenStore) Generate() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	plaintext := hex.EncodeToString(buf)
	hash := hashToken(plaintext)

	// Replace-not-append: a compromised token must be fully invalidated
	// by regenerating, not left valid alongside a new one.
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&storage.McpToken{}).Error; err != nil {
			return err
		}
		return tx.Create(&storage.McpToken{TokenHash: hash, CreatedAt: time.Now()}).Error
	})
	if err != nil {
		return "", err
	}

	return plaintext, nil
}

// HasToken reports whether an MCP token currently exists, so the
// dashboard can show "Generate" vs "Regenerate" without ever needing
// the plaintext itself.
func (s *McpTokenStore) HasToken() (bool, error) {
	var count int64
	if err := s.db.Model(&storage.McpToken{}).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// Validate reports whether the given plaintext token matches the
// currently active one. A missing token (never generated, or revoked
// with no replacement) always fails closed, as does any DB error —
// callers only need a bool, so an error here is treated the same as
// "not valid" rather than surfaced separately.
func (s *McpTokenStore) Validate(plaintext string) bool {
	if plaintext == "" {
		return false
	}

	var row storage.McpToken
	if err := s.db.First(&row).Error; err != nil {
		return false
	}

	return constantTimeEqual(hashToken(plaintext), row.TokenHash)
}

func hashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// ---- dashboard-facing handlers ----------------------------------------
//
// These sit behind DashboardAuth.RequireSession — generating or viewing
// the *existence* of an MCP token is a dashboard-login-gated action,
// same trust level as editing a funnel. The token's actual use (calling
// the MCP server) is gated separately below by McpTokenMiddleware,
// deliberately independent of dashboard sessions.

type mcpTokenStatusResponse struct {
	HasToken bool `json:"has_token"`
}

// McpTokenStatusHandler returns GET /api/dashboard/mcp-token, so the
// dashboard can render "Generate" vs "Regenerate" without ever
// receiving the plaintext token itself.
func McpTokenStatusHandler(store *McpTokenStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		has, err := store.HasToken()
		if err != nil {
			http.Error(w, "failed to check token status", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mcpTokenStatusResponse{HasToken: has})
	}
}

type mcpTokenGenerateResponse struct {
	Token string `json:"token"`
}

// McpTokenGenerateHandler returns POST /api/dashboard/mcp-token. Issues
// a brand-new token, immediately invalidating any previous one, and
// returns the plaintext exactly once — the dashboard must show and let
// the person copy it right away, since it can never be retrieved again
// after this response.
func McpTokenGenerateHandler(store *McpTokenStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		token, err := store.Generate()
		if err != nil {
			http.Error(w, "failed to generate token", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mcpTokenGenerateResponse{Token: token})
	}
}

// ---- MCP-facing middleware ---------------------------------------------

// McpTokenMiddleware gates the MCP server's own routes. Deliberately
// separate from DashboardAuth.RequireSession: an MCP client is not a
// browser and was never expected to hold a dashboard session cookie,
// so it authenticates with its own token instead, checked two ways:
//
//   - Authorization: Bearer <token> header, for clients that support
//     custom headers on their MCP connection (the recommended path,
//     since a header never ends up logged in URL-based access logs).
//   - A token path segment (/mcp/{token}/sse), for clients that can only
//     be configured with a bare URL and no custom headers. This is
//     exactly why the token is a separate, independently-regenerable
//     secret rather than the dashboard password itself — a URL is far
//     more likely to end up in logs or a saved config file, so the
//     credential that might appear there needs to be cheap to rotate
//     without affecting dashboard login at all.
func McpTokenMiddleware(store *McpTokenStore, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			token = r.PathValue("token")
		}

		if !store.Validate(token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return h[len(prefix):]
	}
	return ""
}
