package registry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// SeedWooConfig contains configuration for auto-seeding the default woo-mcp server.
type SeedWooConfig struct {
	URL   string
	Token string
}

// SeedWooServer idempotently registers the woo-mcp server if it does not already exist.
// It never overwrites existing configurations or admin edits.
func SeedWooServer(ctx context.Context, repo Repository, cfg SeedWooConfig) error {
	cfg.URL = strings.TrimSpace(cfg.URL)
	if cfg.URL == "" {
		slog.Info("no woo seed URL configured, skipping auto-seeding")
		return nil
	}

	// Check if server with slug 'woo' already exists
	existing, err := repo.GetBySlug(ctx, "woo")
	if err == nil && existing != nil {
		slog.Info("woo-mcp server already registered in registry, skipping seed to preserve admin edits", "id", existing.ID, "status", existing.Status)
		return nil
	}

	if err != nil && !errors.Is(err, ErrServerNotFound) {
		return fmt.Errorf("failed to check existing woo server in registry: %w", err)
	}

	var cred *UpstreamCredential
	token := strings.TrimSpace(cfg.Token)
	if token != "" {
		cred = &UpstreamCredential{
			Type:  "bearer",
			Token: token,
		}
	}

	server := &Server{
		Slug:      "woo",
		Name:      "WooCommerce Store",
		URL:       cfg.URL,
		Enabled:   true,
		TimeoutMS: 30000,
	}

	created, err := repo.Create(ctx, server, cred)
	if err != nil {
		if errors.Is(err, ErrSlugAlreadyExists) {
			slog.Info("woo server was seeded concurrently, skipping")
			return nil
		}
		return fmt.Errorf("failed to seed woo server: %w", err)
	}

	slog.Info("successfully seeded default woo server", "id", created.ID, "slug", created.Slug, "url", created.URL, "has_credential", created.HasCredential)
	return nil
}
