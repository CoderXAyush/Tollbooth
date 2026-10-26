package db

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed 000001_create_chunk3_tables.up.sql
var migrationChunk3Up string

const (
	// AdvisoryLockID is a chosen 64-bit integer used to ensure only one gateway replica
	// executes migrations at a time.
	AdvisoryLockID int64 = 742910381923
)

// Migrate runs embedded database migrations safely with PostgreSQL advisory locks.
// It is idempotent and safe across multiple concurrently starting gateway pods.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	slog.Info("running database migrations check")

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection for migration: %w", err)
	}
	defer conn.Release()

	// Acquire advisory lock
	var locked bool
	lockQuery := "SELECT pg_advisory_lock($1)"
	if _, err := conn.Exec(ctx, lockQuery, AdvisoryLockID); err != nil {
		return fmt.Errorf("failed to acquire migration advisory lock: %w", err)
	}
	locked = true
	defer func() {
		if locked {
			_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", AdvisoryLockID)
		}
	}()

	// Ensure schema_migrations table exists
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version INT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	);`
	if _, err := conn.Exec(ctx, createTableSQL); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	// Check if version 1 is applied
	var count int
	err = conn.QueryRow(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = 1").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check applied migrations: %w", err)
	}

	if count == 0 {
		slog.Info("applying migration 000001_create_chunk3_tables.up.sql")
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return fmt.Errorf("failed to begin migration transaction: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, migrationChunk3Up); err != nil {
			return fmt.Errorf("migration 1 failed: %w", err)
		}

		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, applied_at) VALUES (1, $1)", time.Now().UTC()); err != nil {
			return fmt.Errorf("failed to record migration 1: %w", err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit migration 1: %w", err)
		}
		slog.Info("migration 000001 applied successfully")
	} else {
		slog.Info("migration 000001 already applied, skipping")
	}

	return nil
}
