package mcpendpoint

import (
	"net/http"

	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/upstream"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewHandler creates the streamable HTTP handler for the gateway MCP endpoint (/mcp).
// It runs in stateless mode, constructing a new mcp.Server per request using the upstream manager's tool snapshot.
func NewHandler(mgr *upstream.Manager) http.Handler {
	opts := &mcp.StreamableHTTPOptions{
		Stateless:                  true,
		DisableLocalhostProtection: true, // Safe for internal k8s networking and ingress
	}

	return mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return mgr.BuildServerForRequest(req)
	}, opts)
}
