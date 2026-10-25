package server

import (
	"log/slog"
	"net/http"

	"github.com/CoderXAyush/Tollbooth/services/woo-mcp/internal/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServerHandler builds the top-level HTTP handler for woo-mcp.
func NewServerHandler(cfg *Config, woo client.Client) http.Handler {
	mux := http.NewServeMux()

	// /healthz – liveness probe (unauthenticated)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// /readyz – readiness probe (unauthenticated)
	// Must NOT flap or fail when the store is briefly down; liveness never depends on store.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})

	// MCP Server initialization
	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "woo-mcp",
		Version: "0.1.0",
	}, nil)

	// Register all WooCommerce tools
	RegisterTools(mcpServer, woo)

	// Streamable HTTP Handler in stateless mode
	// In stateless mode, each incoming request is handled in an ephemeral session,
	// preventing pod restarts or multi-pod replicas from breaking sessions.
	mcpStreamable := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return mcpServer
	}, &mcp.StreamableHTTPOptions{
		Stateless:                  true,
		DisableLocalhostProtection: true, // Safe for dev port-forward and Kubernetes cluster IP
	})

	// Mount /mcp behind Bearer authentication
	mux.Handle("/mcp", BearerAuthMiddleware(cfg.MCPAuthToken, mcpStreamable))

	return logMiddleware(mux)
}

// logMiddleware provides non-sensitive request logging.
// It explicitly never logs authorization tokens, credentials, or full request/response bodies.
func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := slog.Default()
		start.Debug("http request received",
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
		)
		next.ServeHTTP(w, r)
	})
}
