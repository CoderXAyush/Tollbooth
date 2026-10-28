package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/identity"
	"golang.org/x/crypto/bcrypt"
)

const sessionLifetime = 24 * time.Hour
const dummyPasswordHash = "$2a$12$K5s7jy4BJWpzs.XYfpAWMOdku07e0knIApPyMb37uFqjwCyxPdplm"

type loginAttempt struct {
	count       int
	windowStart time.Time
}

type SessionHandler struct {
	repo     *identity.Repository
	mu       sync.Mutex
	attempts map[string]loginAttempt
	now      func() time.Time
}

func NewSessionHandler(repo *identity.Repository) *SessionHandler {
	return &SessionHandler{
		repo:     repo,
		attempts: make(map[string]loginAttempt),
		now:      time.Now,
	}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type sessionUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (h *SessionHandler) Login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r.RemoteAddr)
	if !h.allowAttempt(ip) {
		writeSessionError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many login attempts; try again later")
		return
	}

	var request loginRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeSessionError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body")
		return
	}

	user, err := h.repo.FindUserByEmail(r.Context(), request.Email)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			_ = bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte(request.Password))
			writeSessionError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password")
			return
		}

		writeSessionError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}

	if user.Disabled ||
		bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(request.Password)) != nil {
		writeSessionError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password")
		return
	}

	token, err := newSessionToken()
	if err != nil {
		writeSessionError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not create session")
		return
	}
	hash := sha256.Sum256([]byte(token))
	expiresAt := h.now().UTC().Add(sessionLifetime)
	if err := h.repo.CreateSession(r.Context(), hash, user.ID, expiresAt); err != nil {
		writeSessionError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not create session")
		return
	}

	h.resetAttempts(ip)
	writeSessionJSON(w, http.StatusOK, map[string]any{
		"token":      token,
		"expires_at": expiresAt.Format(time.RFC3339),
		"user":       userResponse(user),
	})
}

func (h *SessionHandler) Logout(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeSessionError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing or invalid authentication credentials")
		return
	}
	hash := sha256.Sum256([]byte(token))
	if err := h.repo.DeleteSession(r.Context(), hash); err != nil {
		writeSessionError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not end session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SessionHandler) Me(w http.ResponseWriter, r *http.Request) {
	principal, ok := IdentityFromContext(r.Context())
	if !ok {
		writeSessionError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing or invalid authentication credentials")
		return
	}
	writeSessionJSON(w, http.StatusOK, map[string]any{
		"user": sessionUser{
			ID: principal.UserID.String(), Email: principal.Email, Role: principal.Role,
		},
	})
}

func (h *SessionHandler) allowAttempt(ip string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := h.now()

	// Remove expired entries to prevent unbounded map growth.
	for addr, attempt := range h.attempts {
		if now.Sub(attempt.windowStart) >= time.Minute {
			delete(h.attempts, addr)
		}
	}

	attempt, exists := h.attempts[ip]
	if !exists {
		attempt = loginAttempt{windowStart: now}
	}

	if attempt.count >= 10 {
		h.attempts[ip] = attempt
		return false
	}

	attempt.count++
	h.attempts[ip] = attempt
	return true
}

func (h *SessionHandler) resetAttempts(ip string) {
	h.mu.Lock()
	delete(h.attempts, ip)
	h.mu.Unlock()
}

func newSessionToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func clientIP(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		return host
	}
	return strings.TrimSpace(remoteAddress)
}

func userResponse(user *identity.User) sessionUser {
	return sessionUser{ID: user.ID.String(), Email: user.Email, Role: user.Role}
}

func writeSessionJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeSessionError(w http.ResponseWriter, status int, code, message string) {
	writeSessionJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}
