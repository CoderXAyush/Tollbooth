// Package main is the entrypoint for the woo-mcp service.
//
// woo-mcp is an MCP server (Streamable HTTP transport) that exposes
// WooCommerce store data (products, orders) as MCP tools. It talks to
// the WooCommerce REST API using consumer key/secret credentials.
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

	"github.com/CoderXAyush/Tollbooth/services/woo-mcp/internal/client"
	"github.com/CoderXAyush/Tollbooth/services/woo-mcp/internal/server"
)

func main() {
	// ── Structured Logger ───────────────────────────────────
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// ── Load & Validate Configuration ────────────────────────
	cfg, err := server.LoadConfig()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// ── WooCommerce Client ──────────────────────────────────
	wooClient := client.NewWooClient(
		cfg.WooBaseURL,
		cfg.WooConsumerKey,
		cfg.WooConsumerSecret,
		cfg.HTTPTimeout,
	)

	// ── Server Handler ──────────────────────────────────────
	handler := server.NewServerHandler(cfg, wooClient)

	// ── HTTP Server ─────────────────────────────────────────
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// ── Graceful Shutdown ───────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("woo-mcp starting", "port", cfg.Port, "store_url", cfg.WooBaseURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("listen error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down gracefully")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}
	slog.Info("woo-mcp stopped")
}
