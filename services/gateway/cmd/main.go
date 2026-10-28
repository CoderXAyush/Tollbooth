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
	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/identity"
	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/mcpendpoint"
	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/registry"
	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/upstream"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

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

	adminEmail := os.Getenv("GATEWAY_ADMIN_EMAIL")
	adminPassword := os.Getenv("GATEWAY_ADMIN_PASSWORD")
	if adminEmail == "" || adminPassword == "" {
		slog.Error("GATEWAY_ADMIN_EMAIL and GATEWAY_ADMIN_PASSWORD are required")
		os.Exit(1)
	}

	// // Temporary until Stage 4b replaces /mcp authentication with API keys.
	// bootstrapToken := os.Getenv("GATEWAY_BOOTSTRAP_TOKEN")
	// if bootstrapToken == "" {
	// 	slog.Error("GATEWAY_BOOTSTRAP_TOKEN environment variable is required")
	// 	os.Exit(1)
	// }

	seedWooURL := os.Getenv("SEED_WOO_URL")
	seedWooToken := os.Getenv("SEED_WOO_TOKEN")

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

	dbCfg := db.Config{
		Host:     pgHost,
		Port:     pgPort,
		User:     pgUser,
		Password: pgPass,
		Database: pgDB,
		SSLMode:  envOr("POSTGRES_SSLMODE", "disable"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()

	pgxPool, err := connectWithRetry(ctx, dbCfg)
	if err != nil {
		slog.Error("failed to connect to PostgreSQL after retries", "err", err)
		os.Exit(1)
	}
	defer pgxPool.Close()

	if err := db.Migrate(ctx, pgxPool); err != nil {
		slog.Error("failed to run database migrations", "err", err)
		os.Exit(1)
	}

	identityRepo := identity.NewRepository(pgxPool)
	if err := identityRepo.SeedAdmin(ctx, adminEmail, adminPassword); err != nil {
		slog.Error("failed to seed first administrator", "err", err)
		os.Exit(1)
	}

	repo := registry.NewPostgresRepository(pgxPool, encryptor)
	if seedWooURL != "" {
		if err := registry.SeedWooServer(ctx, repo, registry.SeedWooConfig{
			URL: seedWooURL, Token: seedWooToken,
		}); err != nil {
			slog.Error("failed to auto-seed woo-mcp server", "err", err)
			os.Exit(1)
		}
	}

	toolsRepo := upstream.NewPostgresToolsRepository(pgxPool)
	upstreamMgr := upstream.NewManager(upstream.ManagerOptions{
		Repo: repo, ToolsRepo: toolsRepo, Recorder: upstream.NewSlogCallRecorder(logger),
	})
	if err := upstreamMgr.Start(ctx); err != nil {
		slog.Warn("upstream manager start encountered issues", "err", err)
	}
	defer upstreamMgr.Stop()

	sessionHandler := auth.NewSessionHandler(identityRepo)
	sessionMiddleware := auth.Middleware(auth.NewSessionAuthenticator(identityRepo))
	apiKeyMiddleware := auth.Middleware(auth.NewAPIKeyAuthenticator(identityRepo))

	registryHandler := registry.NewHandler(repo)
	registryMux := http.NewServeMux()
	registryHandler.RegisterRoutes(registryMux)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /readyz", readyHandler(pgxPool))

	// Dashboard API: session tokens only.
	mux.HandleFunc("POST /api/auth/login", sessionHandler.Login)
	mux.Handle("POST /api/auth/logout", sessionMiddleware(http.HandlerFunc(sessionHandler.Logout)))
	mux.Handle("GET /api/me", sessionMiddleware(http.HandlerFunc(sessionHandler.Me)))
	mux.Handle("/api/servers", sessionMiddleware(auth.AdminOnly(registryMux)))
	mux.Handle("/api/servers/", sessionMiddleware(auth.AdminOnly(registryMux)))

	// API-key management: dashboard sessions only.
	apiKeyHandler := auth.NewAPIKeyHandler(identityRepo)

	mux.Handle("POST /api/keys", sessionMiddleware(http.HandlerFunc(apiKeyHandler.Create)))
	mux.Handle("GET /api/keys", sessionMiddleware(http.HandlerFunc(apiKeyHandler.List)))
	mux.Handle("DELETE /api/keys/{id}", sessionMiddleware(http.HandlerFunc(apiKeyHandler.Revoke)))

	// External MCP API: authenticated using user API keys.
	mux.Handle("/mcp", apiKeyMiddleware(mcpendpoint.NewHandler(upstreamMgr)))

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("gateway starting", "port", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("gateway listen error", "err", err)
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
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func readyHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unavailable","error":"database unreachable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}
}

func connectWithRetry(ctx context.Context, cfg db.Config) (*pgxpool.Pool, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}

	for {
		pool, err := db.NewPool(ctx, cfg)
		if err == nil {
			return pool, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}

		slog.Info("waiting for PostgreSQL to become ready", "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
