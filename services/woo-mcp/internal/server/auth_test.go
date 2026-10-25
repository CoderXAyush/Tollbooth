package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearerAuthMiddleware(t *testing.T) {
	expectedToken := "secret-test-token-12345"
	nextCalled := false

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success"}`))
	})

	mw := BearerAuthMiddleware(expectedToken, dummyHandler)

	tests := []struct {
		name           string
		authHeader     string
		wantStatusCode int
		wantNextCalled bool
	}{
		{
			name:           "Missing Authorization header",
			authHeader:     "",
			wantStatusCode: http.StatusUnauthorized,
			wantNextCalled: false,
		},
		{
			name:           "Invalid Authorization scheme (Basic)",
			authHeader:     "Basic dXNlcjpwYXNz",
			wantStatusCode: http.StatusUnauthorized,
			wantNextCalled: false,
		},
		{
			name:           "Malformed Authorization header",
			authHeader:     "Bearer",
			wantStatusCode: http.StatusUnauthorized,
			wantNextCalled: false,
		},
		{
			name:           "Wrong Bearer token",
			authHeader:     "Bearer wrong-token",
			wantStatusCode: http.StatusUnauthorized,
			wantNextCalled: false,
		},
		{
			name:           "Correct Bearer token",
			authHeader:     "Bearer secret-test-token-12345",
			wantStatusCode: http.StatusOK,
			wantNextCalled: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nextCalled = false
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()

			mw.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatusCode {
				t.Errorf("StatusCode = %d, want %d", rec.Code, tc.wantStatusCode)
			}
			if nextCalled != tc.wantNextCalled {
				t.Errorf("nextCalled = %v, want %v", nextCalled, tc.wantNextCalled)
			}
		})
	}
}
