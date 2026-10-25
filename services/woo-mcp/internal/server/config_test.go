package server

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfig_Success(t *testing.T) {
	_ = os.Setenv("PORT", "9090")
	_ = os.Setenv("WOO_BASE_URL", "http://store-wordpress:80")
	_ = os.Setenv("WOO_CONSUMER_KEY", "ck_123")
	_ = os.Setenv("WOO_CONSUMER_SECRET", "cs_456")
	_ = os.Setenv("MCP_AUTH_TOKEN", "token_789")
	_ = os.Setenv("HTTP_TIMEOUT", "20s")
	defer func() {
		_ = os.Unsetenv("PORT")
		_ = os.Unsetenv("WOO_BASE_URL")
		_ = os.Unsetenv("WOO_CONSUMER_KEY")
		_ = os.Unsetenv("WOO_CONSUMER_SECRET")
		_ = os.Unsetenv("MCP_AUTH_TOKEN")
		_ = os.Unsetenv("HTTP_TIMEOUT")
	}()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Port != "9090" {
		t.Errorf("Port = %s, want 9090", cfg.Port)
	}
	if cfg.WooBaseURL != "http://store-wordpress:80" {
		t.Errorf("WooBaseURL = %s, want http://store-wordpress:80", cfg.WooBaseURL)
	}
	if cfg.WooConsumerKey != "ck_123" || cfg.WooConsumerSecret != "cs_456" {
		t.Errorf("Unexpected keys: %s, %s", cfg.WooConsumerKey, cfg.WooConsumerSecret)
	}
	if cfg.MCPAuthToken != "token_789" {
		t.Errorf("MCPAuthToken = %s, want token_789", cfg.MCPAuthToken)
	}
	if cfg.HTTPTimeout != 20*time.Second {
		t.Errorf("HTTPTimeout = %v, want 20s", cfg.HTTPTimeout)
	}
}

func TestLoadConfig_MissingRequired(t *testing.T) {
	_ = os.Unsetenv("WOO_BASE_URL")
	_ = os.Unsetenv("WOO_CONSUMER_KEY")
	_ = os.Unsetenv("WOO_CONSUMER_SECRET")
	_ = os.Unsetenv("MCP_AUTH_TOKEN")

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error for missing required vars, got nil")
	}
}
