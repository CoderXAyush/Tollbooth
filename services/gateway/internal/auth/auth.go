// Package auth provides request authentication for the Tollbooth gateway.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/identity"
	"github.com/google/uuid"
)

var ErrUnauthorized = errors.New("unauthorized: missing or invalid credentials")

type Identity struct {
	ID       string
	UserID   uuid.UUID
	Email    string
	Role     string
	Client   string
	IsBearer bool
}

type Authenticator interface {
	Authenticate(ctx context.Context, r *http.Request) (*Identity, error)
}

type BootstrapAuthenticator struct {
	token string
}

func NewBootstrapAuthenticator(token string) *BootstrapAuthenticator {
	return &BootstrapAuthenticator{token: token}
}

func (a *BootstrapAuthenticator) Authenticate(_ context.Context, r *http.Request) (*Identity, error) {
	token, ok := bearerToken(r)
	if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(a.token)) != 1 {
		return nil, ErrUnauthorized
	}
	return &Identity{
		ID:       "bootstrap-admin",
		Role:     identity.RoleAdmin,
		Client:   "bootstrap",
		IsBearer: true,
	}, nil
}

type SessionAuthenticator struct {
	repo *identity.Repository
}

func NewSessionAuthenticator(repo *identity.Repository) *SessionAuthenticator {
	return &SessionAuthenticator{repo: repo}
}

func (a *SessionAuthenticator) Authenticate(ctx context.Context, r *http.Request) (*Identity, error) {
	token, ok := bearerToken(r)
	if !ok {
		return nil, ErrUnauthorized
	}

	hash := sha256.Sum256([]byte(token))
	user, err := a.repo.FindActiveSessionUser(ctx, hash)
	if err != nil || user.Disabled {
		return nil, ErrUnauthorized
	}

	return &Identity{
		ID:       user.ID.String(),
		UserID:   user.ID,
		Email:    user.Email,
		Role:     user.Role,
		Client:   "dashboard-session",
		IsBearer: true,
	}, nil
}

type APIKeyAuthenticator struct {
	repo *identity.Repository
}

func NewAPIKeyAuthenticator(repo *identity.Repository) *APIKeyAuthenticator {
	return &APIKeyAuthenticator{repo: repo}
}

func (a *APIKeyAuthenticator) Authenticate(
	ctx context.Context,
	r *http.Request,
) (*Identity, error) {
	token, ok := bearerToken(r)
	if !ok {
		return nil, ErrUnauthorized
	}

	user, err := a.repo.AuthenticateAPIKey(ctx, token)
	if err != nil {
		return nil, ErrUnauthorized
	}

	return &Identity{
		ID:       user.ID.String(),
		UserID:   user.ID,
		Email:    user.Email,
		Role:     user.Role,
		Client:   "api-key",
		IsBearer: true,
	}, nil
}

func bearerToken(r *http.Request) (string, bool) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

type contextKey struct{}

func Middleware(authenticator Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, err := authenticator.Authenticate(r.Context(), r)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("WWW-Authenticate", `Bearer realm="tollbooth-gateway"`)
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"code":"UNAUTHORIZED","message":"missing or invalid authentication credentials"}}`))
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, principal)))
		})
	}
}

func IdentityFromContext(ctx context.Context) (*Identity, bool) {
	principal, ok := ctx.Value(contextKey{}).(*Identity)
	return principal, ok
}

func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := IdentityFromContext(r.Context())
		if !ok || principal == nil {
			http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"authentication required"}}`,
				http.StatusUnauthorized)
			return
		}

		if principal.Role != identity.RoleAdmin {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(
				`{"error":{"code":"FORBIDDEN","message":"admin role required"}}`,
			))
			return
		}

		next.ServeHTTP(w, r)
	})
}
