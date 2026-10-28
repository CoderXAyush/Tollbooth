package identity

import (
	"time"

	"github.com/google/uuid"
)

const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	Role         string
	Disabled     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Session struct {
	TokenHash [32]byte
	UserID    uuid.UUID
	ExpiresAt time.Time
	CreatedAt time.Time
}

type APIKey struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Name       string
	Prefix     string
	KeyHash    [32]byte
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}
