package server

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds runtime configuration for woo-mcp.
type Config struct {
	Port              string
	WooBaseURL        string
	WooConsumerKey    string
	WooConsumerSecret string
	MCPAuthToken      string
	HTTPTimeout       time.Duration
}

// LoadConfig reads configuration from the environment and validates required fields.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		Port:              envOr("PORT", "8081"),
		WooBaseURL:        strings.TrimRight(os.Getenv("WOO_BASE_URL"), "/"),
		WooConsumerKey:    os.Getenv("WOO_CONSUMER_KEY"),
		WooConsumerSecret: os.Getenv("WOO_CONSUMER_SECRET"),
		MCPAuthToken:      os.Getenv("MCP_AUTH_TOKEN"),
	}

	var missing []string
	if cfg.WooBaseURL == "" {
		missing = append(missing, "WOO_BASE_URL")
	}
	if cfg.WooConsumerKey == "" {
		missing = append(missing, "WOO_CONSUMER_KEY")
	}
	if cfg.WooConsumerSecret == "" {
		missing = append(missing, "WOO_CONSUMER_SECRET")
	}
	if cfg.MCPAuthToken == "" {
		missing = append(missing, "MCP_AUTH_TOKEN")
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	timeoutStr := envOr("HTTP_TIMEOUT", "15s")
	t, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return nil, fmt.Errorf("invalid HTTP_TIMEOUT duration %q: %w", timeoutStr, err)
	}
	if t <= 0 {
		return nil, errors.New("HTTP_TIMEOUT must be positive")
	}
	cfg.HTTPTimeout = t

	return cfg, nil
}

func envOr(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
