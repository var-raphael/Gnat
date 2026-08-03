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
}

type RetentionConfig struct {
	RawEventsDays int `yaml:"raw_events_days"` // 0 = keep forever
}

type UpdatesConfig struct {
	CheckForUpdates bool `yaml:"check_for_updates"`
}

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	APIKey    string          `yaml:"api_key"`
	Retention RetentionConfig `yaml:"retention"`
	Updates   UpdatesConfig   `yaml:"updates"`
}

// Load reads and parses the YAML config file at path, applying defaults
// for any fields left unset.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := defaultConfig()

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

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
			Driver: "sqlite",
			Path:   "./analytics.db",
		},
		Retention: RetentionConfig{
			RawEventsDays: 0,
		},
		Updates: UpdatesConfig{
			CheckForUpdates: true,
		},
	}
}

func (c *Config) validate() error {
	if c.APIKey == "" {
		return fmt.Errorf("api_key must be set")
	}
	switch c.Database.Driver {
	case "sqlite", "postgres", "mysql":
	default:
		return fmt.Errorf("unsupported database driver: %s", c.Database.Driver)
	}
	return nil
}
