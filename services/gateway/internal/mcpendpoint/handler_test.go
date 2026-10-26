package mcpendpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/auth"
	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/registry"
	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/upstream"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mockRepo struct {
	servers map[string]*registry.Server
}

func (m *mockRepo) Create(_ context.Context, s *registry.Server, _ *registry.UpstreamCredential) (*registry.Server, error) {
	s.ID = uuid.New()
	m.servers[s.Slug] = s
	return s, nil
}
func (m *mockRepo) GetByID(_ context.Context, _ uuid.UUID) (*registry.Server, error) {
	return nil, nil
}
func (m *mockRepo) GetBySlug(_ context.Context, slug string) (*registry.Server, error) {
	return m.servers[slug], nil
}
func (m *mockRepo) List(_ context.Context) ([]*registry.Server, error) {
	res := make([]*registry.Server, 0, len(m.servers))
	for _, s := range m.servers {
		res = append(res, s)
	}
	return res, nil
}
func (m *mockRepo) Update(_ context.Context, _ uuid.UUID, _ registry.UpdateServerRequest, _ *registry.UpstreamCredential, _ bool) (*registry.Server, error) {
	return nil, nil
}
func (m *mockRepo) Delete(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockRepo) GetDecryptedCredential(_ context.Context, _ uuid.UUID) (*registry.UpstreamCredential, error) {
	return nil, nil
}
func (m *mockRepo) UpdateStatus(_ context.Context, _ uuid.UUID, _ string, _ *string, _ int) error {
	return nil
}

func TestMCPEndpoint_AuthAndToolsList(t *testing.T) {
	ctx := context.Background()

	// Fake upstream
	fakeSrv := mcp.NewServer(&mcp.Implementation{Name: "upstream-1", Version: "1.0"}, nil)
	fakeSrv.AddTool(&mcp.Tool{
		Name:        "get_stock",
		Description: "Get stock level",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: `{"stock": 42}`}},
		}, nil
	})
	fakeHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return fakeSrv
	}, &mcp.StreamableHTTPOptions{
		Stateless:                  true,
		DisableLocalhostProtection: true,
	})
	ts := httptest.NewServer(fakeHandler)
	defer ts.Close()

	repo := &mockRepo{servers: make(map[string]*registry.Server)}
	_, _ = repo.Create(ctx, &registry.Server{
		Slug:      "store1",
		Name:      "Store One",
		URL:       ts.URL,
		Enabled:   true,
		TimeoutMS: 2000,
	}, nil)

	mgr := upstream.NewManager(upstream.ManagerOptions{
		Repo: repo,
	})
	_ = mgr.Start(ctx)
	defer mgr.Stop()

	// Bootstrap token auth
	bootstrapToken := "test-secret-token"
	authenticator := auth.NewBootstrapAuthenticator(bootstrapToken)
	authMiddleware := auth.Middleware(authenticator)

	mcpHandler := NewHandler(mgr)

	mux := http.NewServeMux()
	mux.Handle("/mcp", authMiddleware(mcpHandler))
	gwServer := httptest.NewServer(mux)
	defer gwServer.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	// 1. Unauthenticated request -> 401
	req401, _ := http.NewRequest("POST", gwServer.URL+"/mcp", strings.NewReader(`{}`))
	req401.Header.Set("Content-Type", "application/json")
	req401.Header.Set("Accept", "application/json, text/event-stream")
	resp401, err := client.Do(req401)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp401.Body.Close()
	if resp401.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", resp401.StatusCode)
	}

	// 2. Authenticated tools/list request
	listPayload := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	reqList, _ := http.NewRequest("POST", gwServer.URL+"/mcp", strings.NewReader(listPayload))
	reqList.Header.Set("Authorization", "Bearer "+bootstrapToken)
	reqList.Header.Set("Content-Type", "application/json")
	reqList.Header.Set("Accept", "application/json, text/event-stream")

	respList, err := client.Do(reqList)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer respList.Body.Close()
	if respList.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(respList.Body)
		t.Fatalf("expected 200 OK, got %d: %s", respList.StatusCode, string(body))
	}

	bodyBytes, _ := io.ReadAll(respList.Body)
	var listResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}

	// Streamable HTTP may return SSE or JSON
	var jsonStr string
	if strings.Contains(string(bodyBytes), "data: ") {
		lines := strings.Split(string(bodyBytes), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "data: ") {
				jsonStr = strings.TrimPrefix(line, "data: ")
				break
			}
		}
	} else {
		jsonStr = string(bodyBytes)
	}

	if err := json.Unmarshal([]byte(jsonStr), &listResp); err != nil {
		t.Fatalf("failed to decode list response: %v, raw: %s", err, string(bodyBytes))
	}

	if len(listResp.Result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d in response: %s", len(listResp.Result.Tools), jsonStr)
	}
	if listResp.Result.Tools[0].Name != "store1__get_stock" {
		t.Errorf("expected tool 'store1__get_stock', got %q", listResp.Result.Tools[0].Name)
	}

	// 3. Authenticated tools/call request
	callPayload := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"store1__get_stock","arguments":{}}}`
	reqCall, _ := http.NewRequest("POST", gwServer.URL+"/mcp", bytes.NewBufferString(callPayload))
	reqCall.Header.Set("Authorization", "Bearer "+bootstrapToken)
	reqCall.Header.Set("Content-Type", "application/json")
	reqCall.Header.Set("Accept", "application/json, text/event-stream")

	respCall, err := client.Do(reqCall)
	if err != nil {
		t.Fatalf("call request failed: %v", err)
	}
	defer respCall.Body.Close()
	if respCall.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(respCall.Body)
		t.Fatalf("expected 200 OK, got %d: %s", respCall.StatusCode, string(body))
	}

	callBody, _ := io.ReadAll(respCall.Body)
	if !strings.Contains(string(callBody), "42") {
		t.Errorf("expected stock 42 in call response, got: %s", string(callBody))
	}
}
