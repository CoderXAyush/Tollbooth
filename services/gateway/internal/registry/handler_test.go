package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/auth"
	"github.com/google/uuid"
)

// mockRepo is an in-memory implementation of Repository for testing.
type mockRepo struct {
	mu      sync.RWMutex
	servers map[uuid.UUID]*Server
	slugs   map[string]uuid.UUID
	creds   map[uuid.UUID]*UpstreamCredential
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		servers: make(map[uuid.UUID]*Server),
		slugs:   make(map[string]uuid.UUID),
		creds:   make(map[uuid.UUID]*UpstreamCredential),
	}
}

func (m *mockRepo) Create(_ context.Context, s *Server, cred *UpstreamCredential) (*Server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := ValidateSlug(s.Slug); err != nil {
		return nil, err
	}
	if err := ValidateURL(s.URL); err != nil {
		return nil, err
	}

	if _, exists := m.slugs[s.Slug]; exists {
		return nil, ErrSlugAlreadyExists
	}

	id := uuid.New()
	now := time.Now().UTC()
	created := &Server{
		ID:               id,
		Slug:             s.Slug,
		Name:             s.Name,
		URL:              s.URL,
		Enabled:          s.Enabled,
		HasCredential:    cred != nil,
		CredentialEnc:    []byte("encrypted-mock"),
		CredentialNonce:  []byte("nonce-mock"),
		Status:           "unknown",
		TimeoutMS:        s.TimeoutMS,
		ConsecutiveFails: 0,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	m.servers[id] = created
	m.slugs[s.Slug] = id
	if cred != nil {
		m.creds[id] = cred
	}

	return created, nil
}

func (m *mockRepo) GetByID(_ context.Context, id uuid.UUID) (*Server, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.servers[id]
	if !ok {
		return nil, ErrServerNotFound
	}
	copy := *s
	return &copy, nil
}

func (m *mockRepo) GetBySlug(_ context.Context, slug string) (*Server, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	id, ok := m.slugs[slug]
	if !ok {
		return nil, ErrServerNotFound
	}
	s := m.servers[id]
	copy := *s
	return &copy, nil
}

func (m *mockRepo) List(_ context.Context) ([]*Server, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make([]*Server, 0, len(m.servers))
	for _, s := range m.servers {
		copy := *s
		res = append(res, &copy)
	}
	return res, nil
}

func (m *mockRepo) Update(_ context.Context, id uuid.UUID, req UpdateServerRequest, cred *UpstreamCredential, credProvided bool) (*Server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.servers[id]
	if !ok {
		return nil, ErrServerNotFound
	}

	if req.Name != nil {
		s.Name = *req.Name
	}
	if req.URL != nil {
		s.URL = *req.URL
	}
	if req.Enabled != nil {
		s.Enabled = *req.Enabled
	}
	if req.TimeoutMS != nil {
		s.TimeoutMS = *req.TimeoutMS
	}

	if credProvided {
		if cred != nil {
			m.creds[id] = cred
			s.HasCredential = true
		} else {
			delete(m.creds, id)
			s.HasCredential = false
		}
	}

	s.UpdatedAt = time.Now().UTC()
	copy := *s
	return &copy, nil
}

func (m *mockRepo) Delete(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.servers[id]
	if !ok {
		return ErrServerNotFound
	}

	delete(m.slugs, s.Slug)
	delete(m.creds, id)
	delete(m.servers, id)
	return nil
}

func (m *mockRepo) GetDecryptedCredential(_ context.Context, id uuid.UUID) (*UpstreamCredential, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.servers[id]; !ok {
		return nil, ErrServerNotFound
	}
	return m.creds[id], nil
}

func (m *mockRepo) UpdateStatus(_ context.Context, id uuid.UUID, status string, lastErr *string, consecutiveFails int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.servers[id]
	if !ok {
		return ErrServerNotFound
	}
	s.Status = status
	s.LastError = lastErr
	s.ConsecutiveFails = consecutiveFails
	now := time.Now().UTC()
	s.LastCheckedAt = &now
	return nil
}

func TestSlugValidation(t *testing.T) {
	validSlugs := []string{
		"woo",
		"woo-store",
		"store-1",
		"a",
		"a-b-c-d",
		"12345",
		"abc-123-def",
		"12345678901234567890", // 20 chars
	}

	for _, slug := range validSlugs {
		if err := ValidateSlug(slug); err != nil {
			t.Errorf("expected slug %q to be valid, got: %v", slug, err)
		}
	}

	invalidSlugs := []string{
		"",
		" ",
		"woo_store", // underscores forbidden
		"-woo",      // leading hyphen
		"woo-",      // trailing hyphen
		"WOO",       // uppercase forbidden
		"woo.store", // dots forbidden
		"woo@store", // special characters forbidden
		"123456789012345678901", // 21 chars (exceeds 20)
	}

	for _, slug := range invalidSlugs {
		if err := ValidateSlug(slug); err == nil {
			t.Errorf("expected slug %q to be invalid, but passed", slug)
		}
	}
}

func TestCredentialParsing(t *testing.T) {
	// 1. Bearer string
	cred, err := ParseCredentialInput(json.RawMessage(`"my-token-123"`))
	if err != nil || cred == nil || cred.Type != "bearer" || cred.Token != "my-token-123" {
		t.Fatalf("expected bearer string, got %v, err=%v", cred, err)
	}

	// 2. Structured bearer object
	cred, err = ParseCredentialInput(json.RawMessage(`{"type":"bearer","token":"tok-abc"}`))
	if err != nil || cred == nil || cred.Type != "bearer" || cred.Token != "tok-abc" {
		t.Fatalf("expected bearer object, got %v, err=%v", cred, err)
	}

	// 3. Custom header object
	cred, err = ParseCredentialInput(json.RawMessage(`{"type":"header","header_name":"X-Custom","header_value":"val123"}`))
	if err != nil || cred == nil || cred.Type != "header" || cred.HeaderName != "X-Custom" || cred.HeaderValue != "val123" {
		t.Fatalf("expected header object, got %v, err=%v", cred, err)
	}

	// 4. Empty/null returns nil without error
	cred, err = ParseCredentialInput(json.RawMessage(`null`))
	if err != nil || cred != nil {
		t.Fatalf("expected nil for null, got %v, err=%v", cred, err)
	}
}

func setupTestServer(t *testing.T) (*mockRepo, http.Handler, string) {
	t.Helper()
	repo := newMockRepo()
	handler := NewHandler(repo)

	const testToken = "tb_boot_test_token_123"
	authenticator := auth.NewBootstrapAuthenticator(testToken)

	apiMux := http.NewServeMux()
	handler.RegisterRoutes(apiMux)

	mainMux := http.NewServeMux()
	mainMux.Handle("/api/servers", auth.Middleware(authenticator)(apiMux))
	mainMux.Handle("/api/servers/", auth.Middleware(authenticator)(apiMux))

	return repo, mainMux, testToken
}

func TestRegistryHandler_AuthEnforcement(t *testing.T) {
	_, srv, _ := setupTestServer(t)

	// 1. Missing Authorization header -> 401
	req := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for missing auth, got %d", w.Code)
	}

	// 2. Wrong token -> 401
	req = httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for wrong token, got %d", w.Code)
	}
}

func TestRegistryHandler_CredentialNonDisclosure(t *testing.T) {
	_, srv, token := setupTestServer(t)

	// Register a server with a sensitive credential
	secretToken := "super-secret-upstream-credential-987"
	body := fmt.Sprintf(`{
		"slug": "woo",
		"name": "WooCommerce Store",
		"url": "http://woo-mcp:8081/mcp",
		"credential": "%s",
		"enabled": true
	}`, secretToken)

	req := httptest.NewRequest(http.MethodPost, "/api/servers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	respBody := w.Body.String()
	// Assert credential is NOT in response body
	if strings.Contains(respBody, secretToken) {
		t.Fatalf("SECURITY VIOLATION: credential leaked in POST response body: %s", respBody)
	}
	if strings.Contains(respBody, "credential_enc") || strings.Contains(respBody, "credential_nonce") {
		t.Fatalf("SECURITY VIOLATION: encrypted credential internals leaked in response: %s", respBody)
	}
	if !strings.Contains(respBody, `"has_credential":true`) {
		t.Fatalf("expected has_credential: true in response, got: %s", respBody)
	}

	// GET /api/servers - verify credential is not in list
	req = httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}
	respBody = w.Body.String()
	if strings.Contains(respBody, secretToken) {
		t.Fatalf("SECURITY VIOLATION: credential leaked in GET /api/servers: %s", respBody)
	}
	if !strings.Contains(respBody, `"has_credential":true`) {
		t.Fatalf("expected has_credential: true in list response: %s", respBody)
	}
}

func TestRegistryHandler_CRUDAndValidation(t *testing.T) {
	_, srv, token := setupTestServer(t)

	// 1. Reject invalid slug
	req := httptest.NewRequest(http.MethodPost, "/api/servers", strings.NewReader(`{"slug":"invalid_slug","name":"Woo","url":"http://woo:8081"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for underscore in slug, got %d", w.Code)
	}

	// 2. Reject duplicate slug
	validBody := `{"slug":"store-a","name":"Store A","url":"http://store-a:8081"}`
	req = httptest.NewRequest(http.MethodPost, "/api/servers", strings.NewReader(validBody))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", w.Code)
	}

	// Try duplicate
	req = httptest.NewRequest(http.MethodPost, "/api/servers", strings.NewReader(validBody))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for duplicate slug, got %d", w.Code)
	}

	// 3. GET /api/servers/store-a
	req = httptest.NewRequest(http.MethodGet, "/api/servers/store-a", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for GET by slug, got %d", w.Code)
	}

	// 4. PUT /api/servers/store-a (update name and disabled)
	req = httptest.NewRequest(http.MethodPut, "/api/servers/store-a", strings.NewReader(`{"name":"Updated Store A","enabled":false}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for PUT, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("expected enabled: false, got %s", w.Body.String())
	}

	// 5. DELETE /api/servers/store-a
	req = httptest.NewRequest(http.MethodDelete, "/api/servers/store-a", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content for DELETE, got %d", w.Code)
	}

	// 6. Verify GET now returns 404
	req = httptest.NewRequest(http.MethodGet, "/api/servers/store-a", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found after deletion, got %d", w.Code)
	}
}

func TestRegistryHandler_TestConnection(t *testing.T) {
	// Fake upstream MCP server
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authH := r.Header.Get("Authorization")
		if authH != "Bearer upstream-token" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25"}}`))
	}))
	defer fakeUpstream.Close()

	repo, srv, token := setupTestServer(t)

	// Create server pointing to fakeUpstream
	server := &Server{
		Slug:    "test-upstream",
		Name:    "Test Upstream",
		URL:     fakeUpstream.URL,
		Enabled: true,
	}
	cred := &UpstreamCredential{Type: "bearer", Token: "upstream-token"}
	created, err := repo.Create(context.Background(), server, cred)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// POST /api/servers/{id}/test
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/servers/%s/test", created.ID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for test connection, got %d: %s", w.Code, w.Body.String())
	}

	if !strings.Contains(w.Body.String(), `"status":"connected"`) {
		t.Fatalf("expected status: connected, got: %s", w.Body.String())
	}
}

func TestSeederIdempotency(t *testing.T) {
	repo := newMockRepo()

	cfg := SeedWooConfig{
		URL:   "http://woo-mcp:8081/mcp",
		Token: "test-token",
	}

	// 1. Initial seed should succeed
	if err := SeedWooServer(context.Background(), repo, cfg); err != nil {
		t.Fatalf("initial seed failed: %v", err)
	}

	server, err := repo.GetBySlug(context.Background(), "woo")
	if err != nil {
		t.Fatalf("failed to get seeded woo server: %v", err)
	}
	if server.Name != "WooCommerce Store" || !server.HasCredential {
		t.Fatalf("unexpected seeded server state: %+v", server)
	}

	// 2. Modify server (simulate admin edit)
	updateName := "Custom Admin Name"
	_, err = repo.Update(context.Background(), server.ID, UpdateServerRequest{Name: &updateName}, nil, false)
	if err != nil {
		t.Fatalf("failed to update server: %v", err)
	}

	// 3. Re-run seed on startup
	if err := SeedWooServer(context.Background(), repo, cfg); err != nil {
		t.Fatalf("second seed failed: %v", err)
	}

	// 4. Verify admin edit was preserved!
	serverAfter, err := repo.GetBySlug(context.Background(), "woo")
	if err != nil {
		t.Fatalf("failed to get woo server after second seed: %v", err)
	}
	if serverAfter.Name != "Custom Admin Name" {
		t.Fatalf("admin edit was overwritten! Expected %q, got %q", "Custom Admin Name", serverAfter.Name)
	}
}
