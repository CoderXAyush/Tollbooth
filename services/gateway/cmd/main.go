// Package main is the entrypoint for the Tollbooth gateway service.
//
// The gateway is both an MCP client (connecting to upstream servers like woo-mcp)
// and an MCP server (exposing aggregated tools to the dashboard and external
// clients). In this chunk it only serves health and readiness probes.
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
)

func main() {
	// ── Logger ──────────────────────────────────────────────
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// ── Config from env ─────────────────────────────────────
	port := envOr("PORT", "8080")

	// ── HTTP mux ────────────────────────────────────────────
	mux := http.NewServeMux()

	// /healthz – liveness: if this fails Kubernetes restarts the pod.
	// It returns 200 as long as the process is alive and the HTTP server
	// is accepting connections.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// /readyz – readiness: if this fails Kubernetes removes the pod from
	// service endpoints. Later chunks will add checks for DB connectivity
	// and upstream health.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})

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
	// We listen for SIGINT (Ctrl-C) and SIGTERM (what Kubernetes sends on
	// pod termination). On signal we give in-flight requests up to 15s to
	// complete before forcing shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("gateway starting", "port", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("listen error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down gracefully")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("forced shutdown", "err", err)
	}
	slog.Info("gateway stopped")
}

// envOr returns the value of an environment variable or a fallback default.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
