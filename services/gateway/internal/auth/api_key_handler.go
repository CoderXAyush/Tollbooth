package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/identity"
	"github.com/google/uuid"
)

type APIKeyRepository interface {
	CreateAPIKey(
		ctx context.Context,
		userID uuid.UUID,
		name string,
		expiresAt *time.Time,
	) (string, *identity.APIKey, error)

	ListAPIKeys(ctx context.Context, userID uuid.UUID) ([]identity.APIKey, error)

	RevokeAPIKey(ctx context.Context, userID, keyID uuid.UUID) error
}

type APIKeyHandler struct {
	repo APIKeyRepository
}

func NewAPIKeyHandler(repo APIKeyRepository) *APIKeyHandler {
	return &APIKeyHandler{repo: repo}
}

type createAPIKeyRequest struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type apiKeyResponse struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

func toAPIKeyResponse(key identity.APIKey) apiKeyResponse {
	return apiKeyResponse{
		ID:         key.ID,
		Name:       key.Name,
		Prefix:     key.Prefix,
		CreatedAt:  key.CreatedAt,
		ExpiresAt:  key.ExpiresAt,
		LastUsedAt: key.LastUsedAt,
		RevokedAt:  key.RevokedAt,
	}
}

func (h *APIKeyHandler) Create(w http.ResponseWriter, r *http.Request) {
	principal, ok := IdentityFromContext(r.Context())
	if !ok || principal == nil {
		http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
		return
	}

	var req createAPIKeyRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		http.Error(w, `{"error":"expires_at must be in the future"}`, http.StatusBadRequest)
		return
	}

	rawKey, key, err := h.repo.CreateAPIKey(r.Context(), principal.UserID, req.Name, req.ExpiresAt)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			http.Error(w, `{"error":"user not found or disabled"}`, http.StatusForbidden)
			return
		}
		http.Error(w, `{"error":"could not create API key"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(struct {
		apiKeyResponse
		Key string `json:"key"`
	}{
		apiKeyResponse: toAPIKeyResponse(*key),
		Key:            rawKey,
	})
}

func (h *APIKeyHandler) List(w http.ResponseWriter, r *http.Request) {
	principal, ok := IdentityFromContext(r.Context())
	if !ok || principal == nil {
		http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
		return
	}

	keys, err := h.repo.ListAPIKeys(r.Context(), principal.UserID)
	if err != nil {
		http.Error(w, `{"error":"could not list API keys"}`, http.StatusInternalServerError)
		return
	}

	responses := make([]apiKeyResponse, 0, len(keys))
	for _, key := range keys {
		responses = append(responses, toAPIKeyResponse(key))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Keys []apiKeyResponse `json:"keys"`
	}{Keys: responses})
}

func (h *APIKeyHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	principal, ok := IdentityFromContext(r.Context())
	if !ok || principal == nil {
		http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
		return
	}

	keyID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, `{"error":"invalid API key ID"}`, http.StatusBadRequest)
		return
	}

	err = h.repo.RevokeAPIKey(r.Context(), principal.UserID, keyID)
	if errors.Is(err, identity.ErrAPIKeyNotFound) {
		http.Error(w, `{"error":"API key not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, `{"error":"could not revoke API key"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
