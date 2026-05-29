package config

import (
	"fmt"
	"os"
)

type Config struct {
	AsimutEmail    string
	AsimutPassword string
	AppPassword    string
	CalendarToken  string
	DatabasePath   string
	Port           string
}

func Load() (*Config, error) {
	cfg := &Config{
		AsimutEmail:    os.Getenv("ASIMUT_EMAIL"),
		AsimutPassword: os.Getenv("ASIMUT_PASSWORD"),
		AppPassword:    os.Getenv("APP_PASSWORD"),
		CalendarToken:  os.Getenv("CALENDAR_TOKEN"),
		DatabasePath:   getEnvOrDefault("DATABASE_PATH", "/data/asimut.db"),
		Port:           getEnvOrDefault("PORT", "8080"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks that required environment variables are set.
func (c *Config) Validate() error {
	if c.AppPassword == "" {
		return fmt.Errorf("APP_PASSWORD environment variable is required")
	}
	if c.AsimutEmail == "" {
		return fmt.Errorf("ASIMUT_EMAIL environment variable is required")
	}
	if c.AsimutPassword == "" {
		return fmt.Errorf("ASIMUT_PASSWORD environment variable is required")
	}
	return nil
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
