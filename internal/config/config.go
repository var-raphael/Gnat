package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	BindPort  int    `yaml:"bind_port"`
	PublicURL string `yaml:"public_url"`
}

type DatabaseConfig struct {
	Driver   string `yaml:"driver"` // sqlite, postgres, mysql
	Path     string `yaml:"path"`   // used for sqlite
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
	SSLMode  string `yaml:"sslmode"` // postgres only, defaults to "disable"
}

type RetentionConfig struct {
	RawEventsDays int `yaml:"raw_events_days"`
}

type UpdatesConfig struct {
	CheckForUpdates bool `yaml:"check_for_updates"`
}

type AIConfig struct {
	Provider string `yaml:"provider"` // anthropic, openai, mistral, ollama
	Model    string `yaml:"model"`
}

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	APIKey    string          `yaml:"api_key"`
	Retention RetentionConfig `yaml:"retention"`
	Updates   UpdatesConfig   `yaml:"updates"`
	AI        AIConfig        `yaml:"ai"`

	// aiAPIKey is deliberately not yaml-tagged. It is never read from or
	// written to the config file, only ever set from GNAT_AI_API_KEY.
	aiAPIKey string
}

// Load reads and parses the YAML config file at path, applies env var
// overrides for anything sensitive, then validates the result.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := defaultConfig()

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	cfg.applyEnvOverrides()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			BindPort:  8080,
			PublicURL: "http://localhost:8080",
		},
		Database: DatabaseConfig{
			Driver:  "sqlite",
			Path:    "./analytics.db",
			SSLMode: "disable",
		},
		Retention: RetentionConfig{
			RawEventsDays: 0,
		},
		Updates: UpdatesConfig{
			CheckForUpdates: true,
		},
	}
}

// applyEnvOverrides lets deployment secrets come from the environment
// instead of the yaml file. Env vars win if set, since these are the
// values people should not be committing to a repo.
func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("GNAT_API_KEY"); v != "" {
		c.APIKey = v
	}
	if v := os.Getenv("GNAT_DB_HOST"); v != "" {
		c.Database.Host = v
	}
	if v := os.Getenv("GNAT_DB_USER"); v != "" {
		c.Database.User = v
	}
	if v := os.Getenv("GNAT_DB_PASSWORD"); v != "" {
		c.Database.Password = v
	}
	if v := os.Getenv("GNAT_DB_NAME"); v != "" {
		c.Database.DBName = v
	}
	if v := os.Getenv("GNAT_AI_API_KEY"); v != "" {
		c.aiAPIKey = v
	}
}

func (c *Config) validate() error {
	if c.APIKey == "" {
		return fmt.Errorf("api_key must be set (via config file or GNAT_API_KEY)")
	}
	switch c.Database.Driver {
	case "sqlite":
		// path has a default, nothing further required
	case "postgres", "mysql":
		if c.Database.Host == "" || c.Database.User == "" || c.Database.DBName == "" {
			return fmt.Errorf("%s requires host, user, and dbname (via config or env)", c.Database.Driver)
		}
	default:
		return fmt.Errorf("unsupported database driver: %s", c.Database.Driver)
	}
	return nil
}

// AIAPIKey returns the AI provider API key. Sourced only from the
// GNAT_AI_API_KEY environment variable, never from the config file.
func (c *Config) AIAPIKey() string {
	return c.aiAPIKey
}
