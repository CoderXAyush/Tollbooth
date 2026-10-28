package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserNotFound = errors.New("user not found")
	ErrEmailExists  = errors.New("email already exists")
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) CreateUser(ctx context.Context, email, password, role string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return nil, errors.New("email and password are required")
	}
	if role != RoleAdmin && role != RoleMember {
		return nil, errors.New("invalid role")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &User{}
	err = r.pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, role)
		VALUES ($1, $2, $3)
		RETURNING id, email, password_hash, role, disabled, created_at, updated_at`,
		email, string(hash), role,
	).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Role,
		&user.Disabled, &user.CreatedAt, &user.UpdatedAt,
	)
	if err == nil {
		return user, nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, ErrEmailExists
	}
	return nil, fmt.Errorf("create user: %w", err)
}

func (r *Repository) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	return r.findUser(ctx, "WHERE email = $1", strings.ToLower(strings.TrimSpace(email)))
}

func (r *Repository) FindUserByID(ctx context.Context, id uuid.UUID) (*User, error) {
	return r.findUser(ctx, "WHERE id = $1", id)
}

func (r *Repository) findUser(ctx context.Context, where string, value any) (*User, error) {
	user := &User{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, role, disabled, created_at, updated_at
		FROM users `+where, value,
	).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Role,
		&user.Disabled, &user.CreatedAt, &user.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	return user, nil
}

func (r *Repository) CreateSession(ctx context.Context, tokenHash [32]byte, userID uuid.UUID, expiresAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO sessions (token_hash, user_id, expires_at)
		VALUES ($1, $2, $3)`,
		tokenHash[:], userID, expiresAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (r *Repository) DeleteSession(ctx context.Context, tokenHash [32]byte) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash = $1", tokenHash[:])
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *Repository) FindActiveSessionUser(ctx context.Context, tokenHash [32]byte) (*User, error) {
	user := &User{}
	err := r.pool.QueryRow(ctx, `
		SELECT u.id, u.email, u.password_hash, u.role, u.disabled, u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`,
		tokenHash[:],
	).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Role,
		&user.Disabled, &user.CreatedAt, &user.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find active session user: %w", err)
	}
	return user, nil
}

// SeedAdmin creates the first administrator only. Existing accounts are never modified.
func (r *Repository) SeedAdmin(ctx context.Context, email, password string) error {
	if strings.TrimSpace(email) == "" || password == "" {
		return errors.New("admin email and password are required")
	}

	_, err := r.FindUserByEmail(ctx, email)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrUserNotFound) {
		return err
	}

	_, err = r.CreateUser(ctx, email, password, RoleAdmin)
	if errors.Is(err, ErrEmailExists) {
		return nil
	}
	return err
}
