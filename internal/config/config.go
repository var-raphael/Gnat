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
	Driver   string 
	Path     string 
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string 
}

type RetentionConfig struct {
	RawEventsDays int
}

type AIConfig struct {
	Provider string
	Model    string
}


type Config struct {
	Server    ServerConfig
	Database  DatabaseConfig
	Retention RetentionConfig
	AI        AIConfig


	APIKey string


	DashboardPassword string


	Sites []string


	aiAPIKey string
}


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
	case "postgres", "mysql":
		if c.Database.Host == "" || c.Database.User == "" || c.Database.DBName == "" {
			return fmt.Errorf("%s requires GNAT_DB_HOST, GNAT_DB_USER, and GNAT_DB_NAME", c.Database.Driver)
		}
	default:
		return fmt.Errorf("unsupported GNAT_DB_DRIVER: %s", c.Database.Driver)
	}

	return nil
}


func (c *Config) AIAPIKey() string {
	return c.aiAPIKey
}
