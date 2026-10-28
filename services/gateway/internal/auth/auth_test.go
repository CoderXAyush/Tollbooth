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

func TestAdminOnly(t *testing.T) {
	tests := []struct {
		name       string
		identity   *Identity
		wantStatus int
	}{
		{
			name:       "Unauthenticated request",
			identity:   nil,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "Member is forbidden",
			identity: &Identity{
				Role: "member",
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "Admin is allowed",
			identity: &Identity{
				Role: "admin",
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false

			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			})

			handler := AdminOnly(next)
			req := httptest.NewRequest(http.MethodGet, "/api/servers", nil)

			if tt.identity != nil {
				ctx := context.WithValue(req.Context(), contextKey{}, tt.identity)
				req = req.WithContext(ctx)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}

			if called != (tt.wantStatus == http.StatusOK) {
				t.Errorf("next handler called = %v, want %v",
					called, tt.wantStatus == http.StatusOK)
			}
		})
	}
}

func TestAPIKeyAuthenticatorRejectsMissingOrMalformedBearerToken(t *testing.T) {
	authenticator := NewAPIKeyAuthenticator(nil)

	tests := []struct {
		name   string
		header string
	}{
		{name: "missing header", header: ""},
		{name: "basic authentication", header: "Basic dXNlcjpwYXNz"},
		{name: "missing token", header: "Bearer"},
		{name: "empty token", header: "Bearer "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			_, err := authenticator.Authenticate(context.Background(), req)
			if err == nil {
				t.Fatal("expected authentication error")
			}
		})
	}
}
