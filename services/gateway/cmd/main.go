// Package main is the entrypoint for the Tollbooth gateway service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/auth"
	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/crypto"
	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/db"
	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/registry"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	// ── Logger ──────────────────────────────────────────────
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// ── Configuration from environment ──────────────────────
	port := envOr("PORT", "8080")
	pgHost := envOr("POSTGRES_HOST", "postgres")
	pgPort := envOr("POSTGRES_PORT", "5432")
	pgUser := envOr("POSTGRES_USER", "tollbooth")
	pgPass := os.Getenv("POSTGRES_PASSWORD")
	pgDB := envOr("POSTGRES_DB", "tollbooth")

	encKeyStr := os.Getenv("GATEWAY_ENCRYPTION_KEY")
	if encKeyStr == "" {
		slog.Error("GATEWAY_ENCRYPTION_KEY environment variable is required")
		os.Exit(1)
	}

	bootstrapToken := os.Getenv("GATEWAY_BOOTSTRAP_TOKEN")
	if bootstrapToken == "" {
		slog.Error("GATEWAY_BOOTSTRAP_TOKEN environment variable is required")
		os.Exit(1)
	}

	seedWooURL := os.Getenv("SEED_WOO_URL")
	seedWooToken := os.Getenv("SEED_WOO_TOKEN")

	// ── Cryptography ────────────────────────────────────────
	encKey, err := crypto.ParseKey(encKeyStr)
	if err != nil {
		slog.Error("failed to parse GATEWAY_ENCRYPTION_KEY", "err", err)
		os.Exit(1)
	}
	encryptor, err := crypto.NewGCMEncryptor(encKey)
	if err != nil {
		slog.Error("failed to initialize AES-256-GCM encryptor", "err", err)
		os.Exit(1)
	}

	// ── Database Connection ─────────────────────────────────
	dbCfg := db.Config{
		Host:     pgHost,
		Port:     pgPort,
		User:     pgUser,
		Password: pgPass,
		Database: pgDB,
		SSLMode:  envOr("POSTGRES_SSLMODE", "disable"),
	}

	// Connect with startup retry backoff (up to 30s) to allow DB pod to become ready
	var poolErr error
	var pool *db.Config
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()

	pgxPool, poolErr := connectWithRetry(ctx, dbCfg)
	if poolErr != nil {
		slog.Error("failed to connect to PostgreSQL after retries", "err", poolErr)
		os.Exit(1)
	}
	defer pgxPool.Close()
	_ = pool

	// ── Migrations ──────────────────────────────────────────
	if err := db.Migrate(ctx, pgxPool); err != nil {
		slog.Error("failed to run database migrations", "err", err)
		os.Exit(1)
	}

	// ── Registry Repository & Handlers ──────────────────────
	repo := registry.NewPostgresRepository(pgxPool, encryptor)

	// Auto-seed woo-mcp if configured
	if seedWooURL != "" {
		if err := registry.SeedWooServer(ctx, repo, registry.SeedWooConfig{
			URL:   seedWooURL,
			Token: seedWooToken,
		}); err != nil {
			slog.Error("failed to auto-seed woo-mcp server", "err", err)
			os.Exit(1)
		}
	}

	// TEMPORARY: Bootstrap authenticator for Chunk 3. Chunk 4 will swap this for API keys & user sessions.
	authenticator := auth.NewBootstrapAuthenticator(bootstrapToken)
	authMiddleware := auth.Middleware(authenticator)

	regHandler := registry.NewHandler(repo)

	// ── HTTP mux ────────────────────────────────────────────
	mux := http.NewServeMux()

	// /healthz – liveness: process-only check
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// /readyz – readiness: checks database connectivity
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, pingCancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer pingCancel()

		if err := pgxPool.Ping(pingCtx); err != nil {
			slog.Warn("readyz check failed: DB ping error", "err", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unavailable","error":"database unreachable"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})

	// Mount protected registry API routes
	apiMux := http.NewServeMux()
	regHandler.RegisterRoutes(apiMux)
	mux.Handle("/api/servers", authMiddleware(apiMux))
	mux.Handle("/api/servers/", authMiddleware(apiMux))

	// ── Server ──────────────────────────────────────────────
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// ── Graceful shutdown ───────────────────────────────────
	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("gateway starting", "port", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("listen error", "err", err)
			os.Exit(1)
		}
	}()

	<-shutdownCtx.Done()
	slog.Info("shutting down gracefully")

	drainCtx, drainCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer drainCancel()
	if err := srv.Shutdown(drainCtx); err != nil {
		slog.Error("forced shutdown", "err", err)
	}
	slog.Info("gateway stopped")
}

func connectWithRetry(ctx context.Context, cfg db.Config) (*pgxpool.Pool, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}

	for {
		pool, err := db.NewPool(ctx, cfg)
		if err == nil {
			slog.Info("successfully connected to PostgreSQL")
			return pool, nil
		}

		if time.Now().After(deadline) {
			return nil, err
		}

		slog.Info("waiting for PostgreSQL to become ready...", "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
