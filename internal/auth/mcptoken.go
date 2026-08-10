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


type McpTokenStore struct {
	db *gorm.DB
}

func NewMcpTokenStore(db *gorm.DB) *McpTokenStore {
	return &McpTokenStore{db: db}
}


func (s *McpTokenStore) Generate() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	plaintext := hex.EncodeToString(buf)
	hash := hashToken(plaintext)


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


func (s *McpTokenStore) HasToken() (bool, error) {
	var count int64
	if err := s.db.Model(&storage.McpToken{}).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}


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



type mcpTokenStatusResponse struct {
	HasToken bool `json:"has_token"`
}


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
