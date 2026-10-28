package identity

import (
	"context"
	"crypto/sha256"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAPIKeyPostgresIntegration(t *testing.T) {
	dsn := os.Getenv("TOLLBOOTH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TOLLBOOTH_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	var tableExists bool
	err = pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'api_keys'
		)`).Scan(&tableExists)
	if err != nil {
		t.Fatal(err)
	}
	if !tableExists {
		t.Fatal("api_keys table missing; apply database migrations first")
	}

	repo := NewRepository(pool)
	email := "api-key-test-" + uuid.NewString() + "@example.test"

	user, err := repo.CreateUser(ctx, email, "integration-test-password", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			"DELETE FROM users WHERE id = $1", user.ID)
	})

	// Creation returns a raw key, but the database stores only its hash.
	rawKey, key, err := repo.CreateAPIKey(ctx, user.ID, "integration test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rawKey, "tb_"+key.Prefix+"_") {
		t.Fatal("raw key has an unexpected format")
	}

	hash := sha256.Sum256([]byte(rawKey))
	var storedHash []byte
	err = pool.QueryRow(ctx,
		"SELECT key_hash FROM api_keys WHERE id = $1", key.ID,
	).Scan(&storedHash)
	if err != nil {
		t.Fatal(err)
	}
	if string(storedHash) != string(hash[:]) {
		t.Fatal("database does not contain the expected key hash")
	}
	if string(storedHash) == rawKey {
		t.Fatal("raw API key must not be stored in the database")
	}

	authenticatedUser, err := repo.AuthenticateAPIKey(ctx, rawKey)
	if err != nil {
		t.Fatalf("authenticate valid key: %v", err)
	}
	if authenticatedUser.ID != user.ID {
		t.Fatal("API key authenticated the wrong user")
	}

	keys, err := repo.ListAPIKeys(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].ID != key.ID {
		t.Fatalf("unexpected listed keys: got %d", len(keys))
	}

	if err := repo.RevokeAPIKey(ctx, user.ID, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AuthenticateAPIKey(ctx, rawKey); err != ErrInvalidAPIKey {
		t.Fatalf("expected revoked key to be rejected, got %v", err)
	}
}
