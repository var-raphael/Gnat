package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type ServerConfig struct {
	BindPort  int
	PublicURL string
}

type DatabaseConfig struct {
	Driver   string // sqlite, postgres, mysql
	Path     string // used for sqlite
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string // postgres only, defaults to "disable"
}

type RetentionConfig struct {
	RawEventsDays int
}

type AIConfig struct {
	Provider string // anthropic, openai, mistral, ollama
	Model    string
}

// Config holds every setting Gnat needs, sourced entirely from
// environment variables. There is no config file: secrets and settings
// living in one place, rather than split across a yaml file and env
// overrides, is a deliberate security/simplicity choice.
type Config struct {
	Server    ServerConfig
	Database  DatabaseConfig
	Retention RetentionConfig
	AI        AIConfig

	// APIKey authorizes writes to /api/event only. It is never used to
	// protect the dashboard or stats endpoints — see DashboardPassword.
	APIKey string

	// DashboardPassword gates the dashboard page and every /api/stats
	// and /api/export endpoint. Deliberately separate from APIKey so
	// rotating one never affects the other.
	DashboardPassword string

	// Sites is the operator-controlled allowlist of domains permitted
	// to send events, e.g. "example.com,app.example.com". Incoming
	// events are matched against this list via the Origin header; any
	// origin not on this list is silently dropped at ingest.
	Sites []string

	// aiAPIKey is sourced only from its own env var, deliberately kept
	// unexported so nothing accidentally logs or serializes it alongside
	// the rest of Config.
	aiAPIKey string
}

// Load builds Config entirely from environment variables and validates
// the result. It does not read any file itself; call godotenv.Load()
// earlier in main() if you want local .env support, this function only
// ever looks at the process environment.
func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			BindPort:  envInt("GNAT_BIND_PORT", 8080),
			PublicURL: envString("GNAT_PUBLIC_URL", "http://localhost:8080"),
		},
		Database: DatabaseConfig{
			Driver:   envString("GNAT_DB_DRIVER", "sqlite"),
			Path:     envString("GNAT_DB_PATH", "./analytics.db"),
			Host:     envString("GNAT_DB_HOST", ""),
			Port:     envInt("GNAT_DB_PORT", 0),
			User:     envString("GNAT_DB_USER", ""),
			Password: envString("GNAT_DB_PASSWORD", ""),
			DBName:   envString("GNAT_DB_NAME", ""),
			SSLMode:  envString("GNAT_DB_SSLMODE", "disable"),
		},
		Retention: RetentionConfig{
			RawEventsDays: envInt("GNAT_RETENTION_RAW_EVENTS_DAYS", 0),
		},
		AI: AIConfig{
			Provider: envString("GNAT_AI_PROVIDER", ""),
			Model:    envString("GNAT_AI_MODEL", ""),
		},
		APIKey:            os.Getenv("GNAT_API_KEY"),
		DashboardPassword: os.Getenv("GNAT_DASHBOARD_PASSWORD"),
		Sites:             parseSites(os.Getenv("GNAT_SITES")),
		aiAPIKey:          os.Getenv("GNAT_AI_API_KEY"),
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return parsed
}

// parseSites splits a comma-separated domain list into a clean slice,
// trimming whitespace and dropping empty entries (e.g. from trailing
// commas or accidental double commas).
func parseSites(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	sites := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			sites = append(sites, p)
		}
	}
	return sites
}

// validate fails fast on any missing setting that would otherwise leave
// Gnat running in a silently broken or insecure state: no ingest key
// means anyone could write events, no dashboard password means anyone
// could read them, and no sites means every event would be dropped with
// no way to tell why, same as never registering a property with any
// other analytics tool.
func (c *Config) validate() error {
	if c.APIKey == "" {
		return fmt.Errorf("GNAT_API_KEY must be set")
	}
	if c.DashboardPassword == "" {
		return fmt.Errorf("GNAT_DASHBOARD_PASSWORD must be set")
	}
	if len(c.Sites) == 0 {
		return fmt.Errorf("GNAT_SITES must be set to at least one domain (comma-separated for more), e.g. GNAT_SITES=example.com")
	}

	switch c.Database.Driver {
	case "sqlite":
		// path has a default, nothing further required
	case "postgres", "mysql":
		if c.Database.Host == "" || c.Database.User == "" || c.Database.DBName == "" {
			return fmt.Errorf("%s requires GNAT_DB_HOST, GNAT_DB_USER, and GNAT_DB_NAME", c.Database.Driver)
		}
	default:
		return fmt.Errorf("unsupported GNAT_DB_DRIVER: %s", c.Database.Driver)
	}

	return nil
}

// AIAPIKey returns the AI provider API key, sourced only from
// GNAT_AI_API_KEY.
func (c *Config) AIAPIKey() string {
	return c.aiAPIKey
}
