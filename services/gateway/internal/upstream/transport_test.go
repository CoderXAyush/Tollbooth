package upstream

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/registry"
)

func TestUpstreamRoundTripper_CredentialIsolation(t *testing.T) {
	var capturedAuth string
	var capturedCustomHeader string

	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedCustomHeader = r.Header.Get("X-Custom-Auth")
		w.WriteHeader(http.StatusOK)
	}))
	defer fakeUpstream.Close()

	// Test 1: Bearer credential injection and client token stripping
	rt := NewUpstreamRoundTripper(&registry.UpstreamCredential{
		Type:  "bearer",
		Token: "upstream-secret-token",
	}, nil)

	req, err := http.NewRequest("GET", fakeUpstream.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	// Simulate incoming request carrying client's own bootstrap/session token
	req.Header.Set("Authorization", "Bearer client-bootstrap-secret-token")

	client := &http.Client{Transport: rt}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do failed: %v", err)
	}
	_ = resp.Body.Close()

	// Assertions:
	// 1. Client bootstrap token must NOT be present
	// 2. Upstream secret token MUST be present
	if capturedAuth == "Bearer client-bootstrap-secret-token" {
		t.Errorf("CRITICAL SECURITY FAILURE: client authorization token was forwarded upstream!")
	}
	if capturedAuth != "Bearer upstream-secret-token" {
		t.Errorf("expected Authorization 'Bearer upstream-secret-token', got %q", capturedAuth)
	}

	// Test 2: Custom header injection
	rt2 := NewUpstreamRoundTripper(&registry.UpstreamCredential{
		Type:        "header",
		HeaderName:  "X-Custom-Auth",
		HeaderValue: "custom-secret-key",
	}, nil)

	capturedAuth = ""
	capturedCustomHeader = ""

	req2, _ := http.NewRequest("GET", fakeUpstream.URL, nil)
	req2.Header.Set("Authorization", "Bearer client-token")

	client2 := &http.Client{Transport: rt2}
	resp2, err := client2.Do(req2)
	if err != nil {
		t.Fatalf("client2.Do failed: %v", err)
	}
	_ = resp2.Body.Close()

	if capturedAuth != "" {
		t.Errorf("expected Authorization to be stripped, got %q", capturedAuth)
	}
	if capturedCustomHeader != "custom-secret-key" {
		t.Errorf("expected X-Custom-Auth 'custom-secret-key', got %q", capturedCustomHeader)
	}
}
