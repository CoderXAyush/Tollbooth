package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	// SlugRegex enforces: 1 to 20 lowercase alphanumeric characters and hyphens,
	// cannot start or end with a hyphen, NO underscores.
	SlugRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,18}[a-z0-9])?$`)

	ErrInvalidSlug       = errors.New("slug must match ^[a-z0-9]([a-z0-9-]{0,18}[a-z0-9])?$ (1-20 chars, lowercase alphanumeric and hyphens, no underscores)")
	ErrInvalidURL        = errors.New("url must be a valid http or https URL")
	ErrServerNotFound    = errors.New("server not found")
	ErrSlugAlreadyExists = errors.New("server with this slug already exists")
)

// UpstreamCredential represents decrypted credential details for connecting to an upstream MCP server.
type UpstreamCredential struct {
	Type        string `json:"type"`                  // "bearer" or "header"
	Token       string `json:"token,omitempty"`       // for bearer
	HeaderName  string `json:"header_name,omitempty"` // for custom header
	HeaderValue string `json:"header_value,omitempty"`
}

// Server represents an upstream MCP server registered in the gateway.
type Server struct {
	ID               uuid.UUID  `json:"id"`
	Slug             string     `json:"slug"`
	Name             string     `json:"name"`
	URL              string     `json:"url"`
	Enabled          bool       `json:"enabled"`
	HasCredential    bool       `json:"has_credential"`
	CredentialEnc    []byte     `json:"-"` // Never serialized in JSON responses
	CredentialNonce  []byte     `json:"-"` // Never serialized in JSON responses
	Status           string     `json:"status"`
	LastError        *string    `json:"last_error"`
	LastCheckedAt    *time.Time `json:"last_checked_at"`
	ConsecutiveFails int        `json:"consecutive_fails"`
	TimeoutMS        int        `json:"timeout_ms"`
	ToolCount        int        `json:"tool_count,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// CreateServerRequest is the payload for registering a new upstream server.
type CreateServerRequest struct {
	Slug       string          `json:"slug"`
	Name       string          `json:"name"`
	URL        string          `json:"url"`
	Credential json.RawMessage `json:"credential,omitempty"` // String or object
	Enabled    *bool           `json:"enabled,omitempty"`
	TimeoutMS  int             `json:"timeout_ms,omitempty"`
}

// UpdateServerRequest is the payload for updating an existing server.
type UpdateServerRequest struct {
	Name       *string          `json:"name,omitempty"`
	URL        *string          `json:"url,omitempty"`
	Credential *json.RawMessage `json:"credential,omitempty"`
	Enabled    *bool            `json:"enabled,omitempty"`
	TimeoutMS  *int             `json:"timeout_ms,omitempty"`
}

// ValidateSlug validates that a slug complies with the Tollbooth specification.
func ValidateSlug(slug string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" || len(slug) > 20 || !SlugRegex.MatchString(slug) {
		return ErrInvalidSlug
	}
	return nil
}

// ValidateURL validates that a URL has scheme http or https and a valid host.
func ValidateURL(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ErrInvalidURL
	}
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme must be http or https (got %q)", ErrInvalidURL, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: host cannot be empty", ErrInvalidURL)
	}
	return nil
}

// ParseCredentialInput parses either a raw string or a JSON object into UpstreamCredential.
func ParseCredentialInput(raw json.RawMessage) (*UpstreamCredential, error) {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == `""` {
		return nil, nil
	}

	// Try parsing as simple string first (e.g. "my-token")
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		str = strings.TrimSpace(str)
		if str == "" {
			return nil, nil
		}
		return &UpstreamCredential{
			Type:  "bearer",
			Token: str,
		}, nil
	}

	// Try parsing as structured object
	var cred UpstreamCredential
	if err := json.Unmarshal(raw, &cred); err != nil {
		return nil, fmt.Errorf("invalid credential format: %w", err)
	}

	cred.Type = strings.ToLower(strings.TrimSpace(cred.Type))
	if cred.Type == "" || cred.Type == "bearer" {
		cred.Type = "bearer"
		cred.Token = strings.TrimSpace(cred.Token)
		if cred.Token == "" {
			return nil, errors.New("bearer credential requires non-empty token")
		}
	} else if cred.Type == "header" {
		cred.HeaderName = strings.TrimSpace(cred.HeaderName)
		cred.HeaderValue = strings.TrimSpace(cred.HeaderValue)
		if cred.HeaderName == "" || cred.HeaderValue == "" {
			return nil, errors.New("header credential requires non-empty header_name and header_value")
		}
	} else {
		return nil, fmt.Errorf("unsupported credential type %q (must be 'bearer' or 'header')", cred.Type)
	}

	return &cred, nil
}
