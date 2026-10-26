package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Handler handles HTTP requests for the upstream server registry API.
type Handler struct {
	repo       Repository
	httpClient *http.Client
}

// NewHandler creates a new registry API Handler.
func NewHandler(repo Repository) *Handler {
	return &Handler{
		repo: repo,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// RegisterRoutes registers all registry API routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/servers", h.handleCreate)
	mux.HandleFunc("GET /api/servers", h.handleList)
	mux.HandleFunc("GET /api/servers/{id}", h.handleGet)
	mux.HandleFunc("PUT /api/servers/{id}", h.handleUpdate)
	mux.HandleFunc("DELETE /api/servers/{id}", h.handleDelete)
	mux.HandleFunc("POST /api/servers/{id}/test", h.handleTest)
}

// APIError represents the standard error body per docs/API.md.
type APIError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// ErrorResponse wraps APIError.
type ErrorResponse struct {
	Error APIError `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorResponse{
		Error: APIError{
			Code:    code,
			Message: message,
		},
	})
}

func (h *Handler) resolveServer(ctx context.Context, idStr string) (*Server, error) {
	idStr = strings.TrimSpace(idStr)
	if idStr == "" {
		return nil, ErrServerNotFound
	}

	if id, err := uuid.Parse(idStr); err == nil {
		return h.repo.GetByID(ctx, id)
	}

	return h.repo.GetBySlug(ctx, idStr)
}

func (h *Handler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req CreateServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body: "+err.Error())
		return
	}

	if err := ValidateSlug(req.Slug); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	if err := ValidateURL(req.URL); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "name cannot be empty")
		return
	}

	cred, err := ParseCredentialInput(req.Credential)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	timeoutMS := req.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = 30000
	}

	serverToCreate := &Server{
		Slug:      req.Slug,
		Name:      name,
		URL:       strings.TrimSpace(req.URL),
		Enabled:   enabled,
		TimeoutMS: timeoutMS,
	}

	created, err := h.repo.Create(r.Context(), serverToCreate, cred)
	if err != nil {
		if errors.Is(err, ErrSlugAlreadyExists) {
			writeError(w, http.StatusConflict, "CONFLICT", "server with this slug already exists")
			return
		}
		if errors.Is(err, ErrInvalidSlug) || errors.Is(err, ErrInvalidURL) {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			return
		}
		slog.Error("failed to create server", "error", err, "slug", req.Slug)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create server")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"server": created,
	})
}

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	servers, err := h.repo.List(r.Context())
	if err != nil {
		slog.Error("failed to list servers", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list servers")
		return
	}

	if servers == nil {
		servers = make([]*Server, 0)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": servers,
	})
}

func (h *Handler) handleGet(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	server, err := h.resolveServer(r.Context(), idStr)
	if err != nil {
		if errors.Is(err, ErrServerNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "server not found")
			return
		}
		slog.Error("failed to get server", "error", err, "id", idStr)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get server")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"server": server,
	})
}

func (h *Handler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	server, err := h.resolveServer(r.Context(), idStr)
	if err != nil {
		if errors.Is(err, ErrServerNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "server not found")
			return
		}
		slog.Error("failed to resolve server for update", "error", err, "id", idStr)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to resolve server")
		return
	}

	var req UpdateServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body: "+err.Error())
		return
	}

	if req.URL != nil {
		if err := ValidateURL(*req.URL); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			return
		}
	}

	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "name cannot be empty")
		return
	}

	var cred *UpstreamCredential
	credProvided := req.Credential != nil
	if credProvided {
		parsedCred, err := ParseCredentialInput(*req.Credential)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			return
		}
		cred = parsedCred
	}

	updated, err := h.repo.Update(r.Context(), server.ID, req, cred, credProvided)
	if err != nil {
		if errors.Is(err, ErrServerNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "server not found")
			return
		}
		slog.Error("failed to update server", "error", err, "id", server.ID)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to update server")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"server": updated,
	})
}

func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	server, err := h.resolveServer(r.Context(), idStr)
	if err != nil {
		if errors.Is(err, ErrServerNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "server not found")
			return
		}
		slog.Error("failed to resolve server for delete", "error", err, "id", idStr)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to resolve server")
		return
	}

	if err := h.repo.Delete(r.Context(), server.ID); err != nil {
		if errors.Is(err, ErrServerNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "server not found")
			return
		}
		slog.Error("failed to delete server", "error", err, "id", server.ID)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete server")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleTest(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	server, err := h.resolveServer(r.Context(), idStr)
	if err != nil {
		if errors.Is(err, ErrServerNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "server not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to resolve server")
		return
	}

	cred, err := h.repo.GetDecryptedCredential(r.Context(), server.ID)
	if err != nil {
		slog.Error("failed to get server credential for test", "error", err, "id", server.ID)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to retrieve server credentials")
		return
	}

	start := time.Now()
	testPayload := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"tollbooth-test","version":"1.0.0"}}}`)

	testReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, server.URL, bytes.NewReader(testPayload))
	if err != nil {
		writeError(w, http.StatusBadGateway, "UPSTREAM_ERROR", "failed to prepare upstream test request")
		return
	}

	testReq.Header.Set("Content-Type", "application/json")
	testReq.Header.Set("Accept", "application/json, text/event-stream")
	if cred != nil {
		if cred.Type == "bearer" && cred.Token != "" {
			testReq.Header.Set("Authorization", "Bearer "+cred.Token)
		} else if cred.Type == "header" && cred.HeaderName != "" {
			testReq.Header.Set(cred.HeaderName, cred.HeaderValue)
		}
	}

	resp, err := h.httpClient.Do(testReq)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		slog.Warn("upstream test connection failed", "server", server.Slug, "error", err)
		if errors.Is(r.Context().Err(), context.DeadlineExceeded) || strings.Contains(err.Error(), "timeout") {
			writeError(w, http.StatusGatewayTimeout, "UPSTREAM_TIMEOUT", "upstream server request timed out")
			return
		}
		writeError(w, http.StatusBadGateway, "UPSTREAM_ERROR", "failed to connect to upstream server")
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("upstream test returned non-2xx status", "server", server.Slug, "status", resp.StatusCode)
		writeError(w, http.StatusBadGateway, "UPSTREAM_ERROR", fmt.Sprintf("upstream server returned HTTP %d", resp.StatusCode))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "connected",
		"latency_ms": latency,
	})
}
