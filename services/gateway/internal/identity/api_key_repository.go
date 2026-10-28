package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrAPIKeyNotFound = errors.New("API key not found")
	ErrInvalidAPIKey  = errors.New("invalid API key")
)

// CreateAPIKey generates a high-entropy key and stores only its SHA-256 hash.
// The raw key is returned once and must not be logged or persisted.
func (r *Repository) CreateAPIKey(
	ctx context.Context,
	userID uuid.UUID,
	name string,
	expiresAt *time.Time,
) (string, *APIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil, errors.New("API key name is required")
	}

	// 32 random bytes provide 256 bits of entropy.
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", nil, fmt.Errorf("generate API key: %w", err)
	}

	prefixBytes := make([]byte, 8)
	if _, err := rand.Read(prefixBytes); err != nil {
		return "", nil, fmt.Errorf("generate API key prefix: %w", err)
	}

	prefix := hex.EncodeToString(prefixBytes)
	rawKey := "tb_" + prefix + "_" + hex.EncodeToString(secret)
	hash := sha256.Sum256([]byte(rawKey))

	key := &APIKey{}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO api_keys (user_id, name, prefix, key_hash, expires_at)
		SELECT id, $2, $3, $4, $5
		FROM users
		WHERE id = $1 AND disabled = false
		RETURNING id, user_id, name, prefix, created_at,
		          expires_at, last_used_at, revoked_at`,
		userID, name, prefix, hash[:], expiresAt,
	).Scan(
		&key.ID, &key.UserID, &key.Name, &key.Prefix,
		&key.CreatedAt, &key.ExpiresAt, &key.LastUsedAt, &key.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, ErrUserNotFound
	}
	if err != nil {
		return "", nil, fmt.Errorf("create API key: %w", err)
	}

	key.KeyHash = hash
	return rawKey, key, nil
}

// AuthenticateAPIKey validates the raw key and returns its active user.
// The key's last-used timestamp is updated after successful authentication.
func (r *Repository) AuthenticateAPIKey(
	ctx context.Context,
	rawKey string,
) (*User, error) {
	if len(rawKey) < 20 || !strings.HasPrefix(rawKey, "tb_") {
		return nil, ErrInvalidAPIKey
	}

	hash := sha256.Sum256([]byte(rawKey))
	user := &User{}

	err := r.pool.QueryRow(ctx, `
		UPDATE api_keys AS k
		SET last_used_at = now()
		FROM users AS u
		WHERE k.key_hash = $1
		  AND k.user_id = u.id
		  AND k.revoked_at IS NULL
		  AND (k.expires_at IS NULL OR k.expires_at > now())
		  AND u.disabled = false
		RETURNING u.id, u.email, u.password_hash, u.role,
		          u.disabled, u.created_at, u.updated_at`,
		hash[:],
	).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Role,
		&user.Disabled, &user.CreatedAt, &user.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidAPIKey
	}
	if err != nil {
		return nil, fmt.Errorf("authenticate API key: %w", err)
	}
	return user, nil
}

// RevokeAPIKey revokes a key owned by the specified user.
func (r *Repository) RevokeAPIKey(
	ctx context.Context,
	userID uuid.UUID,
	keyID uuid.UUID,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE api_keys
		SET revoked_at = now()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		keyID, userID,
	)
	if err != nil {
		return fmt.Errorf("revoke API key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

func (r *Repository) ListAPIKeys(
	ctx context.Context,
	userID uuid.UUID,
) ([]APIKey, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, name, prefix, created_at,
		       expires_at, last_used_at, revoked_at
		FROM api_keys
		WHERE user_id = $1
		ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list API keys: %w", err)
	}
	defer rows.Close()

	keys := make([]APIKey, 0)
	for rows.Next() {
		var key APIKey
		if err := rows.Scan(
			&key.ID, &key.UserID, &key.Name, &key.Prefix,
			&key.CreatedAt, &key.ExpiresAt, &key.LastUsedAt, &key.RevokedAt,
		); err != nil {
			return nil, fmt.Errorf("scan API key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate API keys: %w", err)
	}

	return keys, nil
}
