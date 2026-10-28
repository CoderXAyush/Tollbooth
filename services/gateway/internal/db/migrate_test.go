package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrateIsIdempotent(t *testing.T) {
	dsn := os.Getenv("TOLLBOOTH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TOLLBOOTH_TEST_DATABASE_URL to run migration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("first migration run: %v", err)
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("second migration run: %v", err)
	}

	rows, err := pool.Query(ctx,
		"SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var versions []int
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if len(versions) != 3 {
		t.Fatalf("expected 3 migrations, got %v", versions)
	}
	for i, version := range versions {
		if version != i+1 {
			t.Fatalf("expected migration versions [1 2 3], got %v", versions)
		}
	}
}
