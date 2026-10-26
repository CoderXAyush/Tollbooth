package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBootstrapAuthenticator(t *testing.T) {
	const validToken = "tb_boot_test_secret_token_12345"
	authenticator := NewBootstrapAuthenticator(validToken)
	ctx := context.Background()

	t.Run("Missing Authorization Header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/servers", nil)
		id, err := authenticator.Authenticate(ctx, req)
		if err == nil || id != nil {
			t.Fatalf("expected error on missing auth header, got id: %+v", id)
		}
	})

	t.Run("Non-Bearer Authorization Header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/servers", nil)
		req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
		id, err := authenticator.Authenticate(ctx, req)
		if err == nil || id != nil {
			t.Fatalf("expected error on Basic auth header")
		}
	})

	t.Run("Wrong Bearer Token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/servers", nil)
		req.Header.Set("Authorization", "Bearer wrong-token-value")
		id, err := authenticator.Authenticate(ctx, req)
		if err == nil || id != nil {
			t.Fatalf("expected error on wrong token")
		}
	})

	t.Run("Correct Bearer Token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/servers", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		id, err := authenticator.Authenticate(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error on correct token: %v", err)
		}
		if id == nil || id.Role != "admin" {
			t.Fatalf("expected admin identity, got: %+v", id)
		}
	})
}

func TestAuthMiddleware(t *testing.T) {
	const validToken = "valid-token-xyz"
	authenticator := NewBootstrapAuthenticator(validToken)
	middleware := Middleware(authenticator)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := IdentityFromContext(r.Context())
		if !ok || id == nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	t.Run("Rejects Unauthenticated Request with 401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/servers", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected status 401, got: %d", rr.Code)
		}
		if rr.Header().Get("WWW-Authenticate") == "" {
			t.Fatal("expected WWW-Authenticate header")
		}
	})

	t.Run("Allows Authenticated Request with 200", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/servers", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200, got: %d", rr.Code)
		}
	})
}
