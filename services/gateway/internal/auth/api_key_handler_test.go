package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/identity"
	"github.com/google/uuid"
)

type mockAPIKeyRepository struct {
	rawKey    string
	key       *identity.APIKey
	keys      []identity.APIKey
	createErr error
	listErr   error
	revokeErr error
	created   bool
	listed    bool
	revoked   bool
	revokedID uuid.UUID
	userID    uuid.UUID
}

func (m *mockAPIKeyRepository) CreateAPIKey(
	_ context.Context,
	userID uuid.UUID,
	name string,
	expiresAt *time.Time,
) (string, *identity.APIKey, error) {
	m.created = true
	m.userID = userID
	if m.createErr != nil {
		return "", nil, m.createErr
	}
	if m.key != nil {
		return m.rawKey, m.key, nil
	}
	return m.rawKey, &identity.APIKey{
		ID: uuid.New(), UserID: userID, Name: name,
		Prefix: "a1b2c3d4", ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}, nil
}

func (m *mockAPIKeyRepository) ListAPIKeys(
	_ context.Context,
	userID uuid.UUID,
) ([]identity.APIKey, error) {
	m.listed = true
	m.userID = userID
	return m.keys, m.listErr
}

func (m *mockAPIKeyRepository) RevokeAPIKey(
	_ context.Context,
	userID, keyID uuid.UUID,
) error {
	m.revoked = true
	m.userID = userID
	m.revokedID = keyID
	return m.revokeErr
}

func authenticatedRequest(method, path string, body string, userID uuid.UUID) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	principal := &Identity{UserID: userID, ID: userID.String(), Role: identity.RoleMember}
	return req.WithContext(context.WithValue(req.Context(), contextKey{}, principal))
}

func TestAPIKeyHandlerRejectsUnauthenticatedRequests(t *testing.T) {
	repo := &mockAPIKeyRepository{}
	handler := NewAPIKeyHandler(repo)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create", http.MethodPost, "/api/keys", `{"name":"test"}`},
		{"list", http.MethodGet, "/api/keys", ""},
		{"revoke", http.MethodDelete, "/api/keys/" + uuid.NewString(), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			switch tt.method {
			case http.MethodPost:
				handler.Create(rec, req)
			case http.MethodGet:
				handler.List(rec, req)
			case http.MethodDelete:
				handler.Revoke(rec, req)
			}
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", rec.Code)
			}
		})
	}

	if repo.created || repo.listed || repo.revoked {
		t.Fatal("repository must not be called for unauthenticated requests")
	}
}

func TestAPIKeyHandlerCreate(t *testing.T) {
	userID := uuid.New()
	repo := &mockAPIKeyRepository{rawKey: "tb_a1b2c3d4_secret"}
	handler := NewAPIKeyHandler(repo)

	req := authenticatedRequest(
		http.MethodPost, "/api/keys",
		`{"name":"local dev"}`, userID,
	)
	rec := httptest.NewRecorder()
	handler.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if !repo.created || repo.userID != userID {
		t.Fatal("expected key creation for authenticated user")
	}

	var body struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Key != repo.rawKey {
		t.Fatal("creation response should return the raw key once")
	}
	if body.Name != "local dev" {
		t.Fatalf("unexpected key name %q", body.Name)
	}
}

func TestAPIKeyHandlerListDoesNotExposeSecrets(t *testing.T) {
	userID := uuid.New()
	repo := &mockAPIKeyRepository{
		keys: []identity.APIKey{{
			ID: uuid.New(), UserID: userID, Name: "dashboard",
			Prefix: "a1b2c3d4", KeyHash: [32]byte{1, 2, 3},
			CreatedAt: time.Now(),
		}},
	}
	handler := NewAPIKeyHandler(repo)

	req := authenticatedRequest(http.MethodGet, "/api/keys", "", userID)
	rec := httptest.NewRecorder()
	handler.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !repo.listed || repo.userID != userID {
		t.Fatal("expected keys to be listed for authenticated user")
	}

	body := rec.Body.String()
	if strings.Contains(body, "key_hash") || strings.Contains(body, "KeyHash") {
		t.Fatalf("response exposes key hash: %s", body)
	}
	if strings.Contains(body, "tb_a1b2c3d4_secret") {
		t.Fatalf("response exposes raw secret: %s", body)
	}
	if !strings.Contains(body, `"prefix":"a1b2c3d4"`) {
		t.Fatalf("response should contain key prefix: %s", body)
	}
}

func TestAPIKeyHandlerRevoke(t *testing.T) {
	userID := uuid.New()
	keyID := uuid.New()
	repo := &mockAPIKeyRepository{}
	handler := NewAPIKeyHandler(repo)

	req := authenticatedRequest(
		http.MethodDelete, "/api/keys/"+keyID.String(), "", userID,
	)
	req.SetPathValue("id", keyID.String())
	rec := httptest.NewRecorder()
	handler.Revoke(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if !repo.revoked || repo.userID != userID || repo.revokedID != keyID {
		t.Fatal("expected revocation scoped to authenticated user and key ID")
	}
}

func TestAPIKeyHandlerCreateInvalidExpiry(t *testing.T) {
	repo := &mockAPIKeyRepository{}
	handler := NewAPIKeyHandler(repo)
	userID := uuid.New()

	req := authenticatedRequest(
		http.MethodPost, "/api/keys",
		`{"name":"expired","expires_at":"2000-01-01T00:00:00Z"}`, userID,
	)
	rec := httptest.NewRecorder()
	handler.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if repo.created {
		t.Fatal("repository should not be called for an expired key")
	}
}

func TestAPIKeyHandlerRevokeNotFound(t *testing.T) {
	keyID := uuid.New()
	repo := &mockAPIKeyRepository{revokeErr: identity.ErrAPIKeyNotFound}
	handler := NewAPIKeyHandler(repo)

	req := authenticatedRequest(
		http.MethodDelete, "/api/keys/"+keyID.String(), "", uuid.New(),
	)
	req.SetPathValue("id", keyID.String())

	rec := httptest.NewRecorder()
	handler.Revoke(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestAPIKeyHandlerListRepositoryError(t *testing.T) {
	repo := &mockAPIKeyRepository{listErr: errors.New("database unavailable")}
	handler := NewAPIKeyHandler(repo)

	req := authenticatedRequest(http.MethodGet, "/api/keys", "", uuid.New())
	rec := httptest.NewRecorder()
	handler.List(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}
