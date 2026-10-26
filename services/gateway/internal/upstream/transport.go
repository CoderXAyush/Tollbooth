package upstream

import (
	"net/http"

	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/registry"
)

// UpstreamRoundTripper implements http.RoundTripper.
// It ensures:
// 1. Client credentials (e.g. client's Authorization header) are NEVER forwarded upstream.
// 2. Only the upstream server's configured credential (Bearer token or custom header) is injected.
type UpstreamRoundTripper struct {
	Base http.RoundTripper
	Cred *registry.UpstreamCredential
}

// NewUpstreamRoundTripper creates a new UpstreamRoundTripper.
func NewUpstreamRoundTripper(cred *registry.UpstreamCredential, base http.RoundTripper) *UpstreamRoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &UpstreamRoundTripper{
		Base: base,
		Cred: cred,
	}
}

// RoundTrip executes the HTTP request after sanitizing and injecting credentials.
func (t *UpstreamRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone request and header map to prevent modifying caller's request
	clonedReq := req.Clone(req.Context())

	// CRITICAL SECURITY REQUIREMENT:
	// The client's own Authorization header is NEVER forwarded upstream.
	clonedReq.Header.Del("Authorization")

	// Inject the server's own credentials if present
	if t.Cred != nil {
		if t.Cred.Type == "bearer" && t.Cred.Token != "" {
			clonedReq.Header.Set("Authorization", "Bearer "+t.Cred.Token)
		} else if t.Cred.Type == "header" && t.Cred.HeaderName != "" && t.Cred.HeaderValue != "" {
			clonedReq.Header.Set(t.Cred.HeaderName, t.Cred.HeaderValue)
		}
	}

	return t.Base.RoundTrip(clonedReq)
}
