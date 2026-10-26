// Package auth provides request authentication for the Tollbooth gateway.
package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

var (
	ErrUnauthorized = errors.New("unauthorized: missing or invalid credentials")
)

// Identity represents the authenticated caller.
// In Chunk 3 this is a static temporary bootstrap identity.
// In Chunk 4 this will represent real users, roles, sessions, and API keys.
type Identity struct {
	ID       string
	Role     string // "admin" or "member"
	IsBearer bool
}

// Authenticator validates incoming requests.
type Authenticator interface {
	Authenticate(ctx context.Context, r *http.Request) (*Identity, error)
}

// BootstrapAuthenticator validates requests against a single static bootstrap token.
// TEMPORARY: This implementation is for Chunk 3 bootstrap and pipeline testing.
// In Chunk 4, this interface implementation will be swapped out for real database-backed
// user sessions, roles, and hashed API keys.
type BootstrapAuthenticator struct {
	token string
}

// NewBootstrapAuthenticator creates a new BootstrapAuthenticator.
// TEMPORARY: Marked temporary until Chunk 4 user/API key implementation.
func NewBootstrapAuthenticator(token string) *BootstrapAuthenticator {
	return &BootstrapAuthenticator{token: token}
}

// Authenticate verifies the Bearer token in constant time.
func (a *BootstrapAuthenticator) Authenticate(_ context.Context, r *http.Request) (*Identity, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, ErrUnauthorized
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil, ErrUnauthorized
	}

	providedToken := strings.TrimSpace(parts[1])
	if subtle.ConstantTimeCompare([]byte(providedToken), []byte(a.token)) != 1 {
		return nil, ErrUnauthorized
	}

	return &Identity{
		ID:       "bootstrap-admin",
		Role:     "admin",
		IsBearer: true,
	}, nil
}

type contextKey struct{}

// Middleware returns an HTTP middleware that requires valid authentication.
func Middleware(authenticator Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, err := authenticator.Authenticate(r.Context(), r)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("WWW-Authenticate", `Bearer realm="tollbooth-gateway"`)
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"code":"UNAUTHORIZED","message":"missing or invalid authentication credentials"}}`))
				return
			}

			ctx := context.WithValue(r.Context(), contextKey{}, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// IdentityFromContext returns the authenticated caller's Identity from the request context.
func IdentityFromContext(ctx context.Context) (*Identity, bool) {
	id, ok := ctx.Value(contextKey{}).(*Identity)
	return id, ok
}
