package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/registry"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mockRepo struct {
	mu      sync.Mutex
	servers map[string]*registry.Server
	creds   map[uuid.UUID]*registry.UpstreamCredential
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		servers: make(map[string]*registry.Server),
		creds:   make(map[uuid.UUID]*registry.UpstreamCredential),
	}
}

func (m *mockRepo) Create(_ context.Context, s *registry.Server, cred *registry.UpstreamCredential) (*registry.Server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s.ID = uuid.New()
	s.CreatedAt = time.Now().UTC()
	s.UpdatedAt = time.Now().UTC()
	m.servers[s.Slug] = s
	if cred != nil {
		m.creds[s.ID] = cred
	}
	return s, nil
}

func (m *mockRepo) GetByID(_ context.Context, id uuid.UUID) (*registry.Server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.servers {
		if s.ID == id {
			return s, nil
		}
	}
	return nil, registry.ErrServerNotFound
}

func (m *mockRepo) GetBySlug(_ context.Context, slug string) (*registry.Server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.servers[slug]
	if !ok {
		return nil, registry.ErrServerNotFound
	}
	return s, nil
}

func (m *mockRepo) List(_ context.Context) ([]*registry.Server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make([]*registry.Server, 0, len(m.servers))
	for _, s := range m.servers {
		res = append(res, s)
	}
	return res, nil
}

func (m *mockRepo) Update(_ context.Context, id uuid.UUID, _ registry.UpdateServerRequest, _ *registry.UpstreamCredential, _ bool) (*registry.Server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.servers {
		if s.ID == id {
			return s, nil
		}
	}
	return nil, registry.ErrServerNotFound
}

func (m *mockRepo) Delete(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for slug, s := range m.servers {
		if s.ID == id {
			delete(m.servers, slug)
			delete(m.creds, id)
			return nil
		}
	}
	return registry.ErrServerNotFound
}

func (m *mockRepo) GetDecryptedCredential(_ context.Context, id uuid.UUID) (*registry.UpstreamCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.creds[id], nil
}

func (m *mockRepo) UpdateStatus(_ context.Context, id uuid.UUID, status string, lastErr *string, consecutiveFails int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.servers {
		if s.ID == id {
			s.Status = status
			s.LastError = lastErr
			s.ConsecutiveFails = consecutiveFails
			return nil
		}
	}
	return nil
}

// helper to start a fake MCP server
func startFakeMCPServer(t *testing.T, setupFn func(s *mcp.Server)) *httptest.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "fake-mcp", Version: "1.0"}, nil)
	if setupFn != nil {
		setupFn(s)
	}
	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return s
	}, &mcp.StreamableHTTPOptions{
		Stateless:                  true,
		DisableLocalhostProtection: true,
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(func() { ts.Close() })
	return ts
}

func TestManager_NormalAndCollision(t *testing.T) {
	ctx := context.Background()

	// Upstream 1: "woo"
	ts1 := startFakeMCPServer(t, func(s *mcp.Server) {
		s.AddTool(&mcp.Tool{
			Name:        "list_products",
			Description: "List products from Woo",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: `[{"id": 1, "name": "Shirt"}]`}},
			}, nil
		})
	})

	// Upstream 2: "woo-b" - exposes same tool name "list_products" to test collisions!
	ts2 := startFakeMCPServer(t, func(s *mcp.Server) {
		s.AddTool(&mcp.Tool{
			Name:        "list_products",
			Description: "List products from Store B",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: `[{"id": 2, "name": "Cap"}]`}},
			}, nil
		})
	})

	repo := newMockRepo()
	_, _ = repo.Create(ctx, &registry.Server{
		Slug:      "woo",
		Name:      "WooCommerce Main",
		URL:       ts1.URL,
		Enabled:   true,
		TimeoutMS: 5000,
	}, nil)

	_, _ = repo.Create(ctx, &registry.Server{
		Slug:      "woo-b",
		Name:      "WooCommerce Secondary",
		URL:       ts2.URL,
		Enabled:   true,
		TimeoutMS: 5000,
	}, nil)

	mgr := NewManager(ManagerOptions{
		Repo: repo,
	})
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("mgr.Start error: %v", err)
	}
	defer mgr.Stop()

	// Verify tools snapshot contains both tools without collision: woo__list_products and woo-b__list_products
	snapshot := mgr.GetToolSnapshot()
	if len(snapshot) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(snapshot))
	}

	names := make(map[string]bool)
	for _, tool := range snapshot {
		names[tool.ExposedName] = true
	}

	if !names["woo__list_products"] || !names["woo-b__list_products"] {
		t.Errorf("expected both woo__list_products and woo-b__list_products in snapshot, got: %+v", names)
	}

	// Call woo__list_products
	res1, err := mgr.CallTool(ctx, "test-user", "woo__list_products", nil)
	if err != nil {
		t.Fatalf("CallTool woo__list_products failed: %v", err)
	}
	if res1.IsError || len(res1.Content) == 0 {
		t.Fatalf("expected success, got: %+v", res1)
	}
	txt1 := res1.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(txt1, "Shirt") {
		t.Errorf("expected 'Shirt', got %q", txt1)
	}

	// Call woo-b__list_products
	res2, err := mgr.CallTool(ctx, "test-user", "woo-b__list_products", nil)
	if err != nil {
		t.Fatalf("CallTool woo-b__list_products failed: %v", err)
	}
	if res2.IsError || len(res2.Content) == 0 {
		t.Fatalf("expected success, got: %+v", res2)
	}
	txt2 := res2.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(txt2, "Cap") {
		t.Errorf("expected 'Cap', got %q", txt2)
	}
}

func TestManager_SlowUpstream_Timeout(t *testing.T) {
	ctx := context.Background()

	// Slow upstream that sleeps 3 seconds
	tsSlow := startFakeMCPServer(t, func(s *mcp.Server) {
		s.AddTool(&mcp.Tool{
			Name:        "slow_action",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			time.Sleep(3 * time.Second)
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "done"}},
			}, nil
		})
	})

	repo := newMockRepo()
	_, _ = repo.Create(ctx, &registry.Server{
		Slug:      "slow-srv",
		Name:      "Slow Server",
		URL:       tsSlow.URL,
		Enabled:   true,
		TimeoutMS: 500, // 500ms timeout
	}, nil)

	mgr := NewManager(ManagerOptions{
		Repo: repo,
	})
	_ = mgr.Start(ctx)
	defer mgr.Stop()

	start := time.Now()
	res, err := mgr.CallTool(ctx, "user", "slow-srv__slow_action", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected call error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected isError=true for timeout, got: %+v", res)
	}
	errMsg := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(errMsg, "timed out") {
		t.Errorf("expected timeout message, got %q", errMsg)
	}
	if elapsed > 2*time.Second {
		t.Errorf("call took too long to time out: %v", elapsed)
	}
}

func TestManager_DeadUpstream_Isolation(t *testing.T) {
	ctx := context.Background()

	// Dead server: point to a closed listener
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	deadURL := ts.URL
	ts.Close() // Immediately closed!

	repo := newMockRepo()
	_, _ = repo.Create(ctx, &registry.Server{
		Slug:      "dead-srv",
		Name:      "Dead Server",
		URL:       deadURL,
		Enabled:   true,
		TimeoutMS: 1000,
	}, nil)

	mgr := NewManager(ManagerOptions{
		Repo: repo,
	})
	_ = mgr.Start(ctx)
	defer mgr.Stop()

	// Tool snapshot must omit dead server tools
	snapshot := mgr.GetToolSnapshot()
	for _, tool := range snapshot {
		if tool.ServerSlug == "dead-srv" {
			t.Errorf("expected dead server tools to be omitted from snapshot!")
		}
	}

	// Direct call to dead server returns clear unreachable error
	res, err := mgr.CallTool(ctx, "user", "dead-srv__any_tool", nil)
	if err != nil {
		t.Fatalf("unexpected call error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected isError=true, got: %+v", res)
	}
	errMsg := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(errMsg, "is unreachable") {
		t.Errorf("expected 'server 'dead-srv' is unreachable', got %q", errMsg)
	}
}

func TestManager_InvalidToolNamesSkipped(t *testing.T) {
	ctx := context.Background()

	ts := startFakeMCPServer(t, func(s *mcp.Server) {
		// Valid tool
		s.AddTool(&mcp.Tool{
			Name:        "valid_tool",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
		// Invalid tool name (special characters)
		s.AddTool(&mcp.Tool{
			Name:        "invalid#tool$name",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
	})

	repo := newMockRepo()
	_, _ = repo.Create(ctx, &registry.Server{
		Slug:      "test-srv",
		Name:      "Test Server",
		URL:       ts.URL,
		Enabled:   true,
		TimeoutMS: 2000,
	}, nil)

	mgr := NewManager(ManagerOptions{
		Repo: repo,
	})
	_ = mgr.Start(ctx)
	defer mgr.Stop()

	snapshot := mgr.GetToolSnapshot()
	if len(snapshot) != 1 {
		t.Fatalf("expected exactly 1 valid tool in snapshot, got %d", len(snapshot))
	}
	if snapshot[0].ExposedName != "test-srv__valid_tool" {
		t.Errorf("expected test-srv__valid_tool, got %q", snapshot[0].ExposedName)
	}

	// Verify server warnings count was incremented
	mgr.mu.RLock()
	warnings := mgr.servers["test-srv"].Warnings
	mgr.mu.RUnlock()
	if warnings != 1 {
		t.Errorf("expected 1 warning for skipped tool, got %d", warnings)
	}
}

func TestManager_LargeResponseCap(t *testing.T) {
	ctx := context.Background()

	largePayload := strings.Repeat("A", 2*1024*1024) // 2MB string
	ts := startFakeMCPServer(t, func(s *mcp.Server) {
		s.AddTool(&mcp.Tool{
			Name:        "large_tool",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: largePayload}},
			}, nil
		})
	})

	repo := newMockRepo()
	_, _ = repo.Create(ctx, &registry.Server{
		Slug:      "big-srv",
		Name:      "Big Server",
		URL:       ts.URL,
		Enabled:   true,
		TimeoutMS: 5000,
	}, nil)

	// Set max response bytes to 1MB (smaller than the 2MB payload)
	mgr := NewManager(ManagerOptions{
		Repo:             repo,
		MaxResponseBytes: 1 * 1024 * 1024,
	})
	_ = mgr.Start(ctx)
	defer mgr.Stop()

	res, err := mgr.CallTool(ctx, "user", "big-srv__large_tool", nil)
	if err != nil {
		t.Fatalf("unexpected call error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected isError=true for exceeding cap, got: %+v", res)
	}
	errMsg := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(errMsg, "response size exceeded limit") {
		t.Errorf("expected 'response size exceeded limit', got %q", errMsg)
	}
}

func TestManager_ParallelToolsListSpeed(t *testing.T) {
	ctx := context.Background()

	// Fast server: responds in 10ms
	tsFast := startFakeMCPServer(t, func(s *mcp.Server) {
		s.AddTool(&mcp.Tool{
			Name:        "fast_tool",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
	})

	// Slow server: hangs
	tsHanging := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer tsHanging.Close()

	repo := newMockRepo()
	_, _ = repo.Create(ctx, &registry.Server{
		Slug:      "fast-srv",
		Name:      "Fast Server",
		URL:       tsFast.URL,
		Enabled:   true,
		TimeoutMS: 2000,
	}, nil)

	_, _ = repo.Create(ctx, &registry.Server{
		Slug:      "hanging-srv",
		Name:      "Hanging Server",
		URL:       tsHanging.URL,
		Enabled:   true,
		TimeoutMS: 2000,
	}, nil)

	mgr := NewManager(ManagerOptions{
		Repo: repo,
	})
	_ = mgr.Start(ctx)
	defer mgr.Stop()

	// Taking the snapshot should be instant (< 50ms) even though one upstream is hanging
	start := time.Now()
	snapshot := mgr.GetToolSnapshot()
	duration := time.Since(start)

	if duration > 100*time.Millisecond {
		t.Errorf("GetToolSnapshot took too long: %v (expected < 100ms)", duration)
	}

	var foundFast bool
	for _, tool := range snapshot {
		if tool.ExposedName == "fast-srv__fast_tool" {
			foundFast = true
		}
	}
	if !foundFast {
		t.Errorf("expected fast-srv__fast_tool to be present in snapshot")
	}
}
