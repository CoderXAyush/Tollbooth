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

//go:embed 000002_create_identity_tables.up.sql
var migrationIdentityUp string

//go:embed 000003_create_api_keys.up.sql
var migrationApiKeysUp string

const AdvisoryLockID int64 = 742910381923

type migration struct {
	version int
	name    string
	sql     string
}

var migrations = []migration{
	{version: 1, name: "000001_create_chunk3_tables.up.sql", sql: migrationChunk3Up},
	{version: 2, name: "000002_create_identity_tables.up.sql", sql: migrationIdentityUp},
	{version: 3, name: "000003_create_api_keys.up.sql", sql: migrationApiKeysUp},
}

// Migrate applies embedded migrations once, in version order, under one advisory lock.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", AdvisoryLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", AdvisoryLockID)
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, migration := range migrations {
		var applied bool
		if err := conn.QueryRow(
			ctx,
			"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)",
			migration.version,
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %d: %w", migration.version, err)
		}
		if applied {
			continue
		}

		slog.Info("applying database migration", "version", migration.version, "name", migration.name)
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", migration.version, err)
		}

		if _, err := tx.Exec(ctx, migration.sql); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("execute migration %d: %w", migration.version, err)
		}
		if _, err := tx.Exec(
			ctx,
			"INSERT INTO schema_migrations (version, applied_at) VALUES ($1, $2)",
			migration.version,
			time.Now().UTC(),
		); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %d: %w", migration.version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %d: %w", migration.version, err)
		}
	}
	return nil
}
