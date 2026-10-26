package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/CoderXAyush/Tollbooth/services/gateway/internal/registry"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	DefaultCallTimeout       = 30 * time.Second
	DefaultHealthInterval    = 10 * time.Second
	DefaultMaxResponseBytes  = 4 * 1024 * 1024 // 4 MiB
	BaseBackoff              = 2 * time.Second
	MaxBackoff               = 30 * time.Second
	HealthCheckDeadline      = 5 * time.Second
)

// ExposedTool encapsulates an upstream tool with its namespaced exposed name.
type ExposedTool struct {
	ServerSlug  string
	ServerID    uuid.UUID
	OriginName  string
	ExposedName string
	Description string
	InputSchema any
	Annotations *mcp.ToolAnnotations
}

// ManagedServer represents in-memory state and connection for an upstream server.
type ManagedServer struct {
	mu               sync.RWMutex
	Server           *registry.Server
	ClientSession    *mcp.ClientSession
	Transport        *UpstreamRoundTripper
	CachedTools      []*ExposedTool
	Warnings         int
	ConsecutiveFails int
	NextCheck        time.Time
}

// Manager orchestrates upstream connections, background health checking,
// tool aggregation, failure isolation, and call routing.
type Manager struct {
	mu               sync.RWMutex
	repo             registry.Repository
	toolsRepo        ToolsRepository
	recorder         CallRecorder
	servers          map[string]*ManagedServer // keyed by slug
	healthInterval   time.Duration
	defaultTimeout   time.Duration
	maxResponseBytes int64
	clientFactory    func(s *registry.Server, cred *registry.UpstreamCredential) (*mcp.ClientSession, error)
	stopCh           chan struct{}
	stopped          bool
}

// ManagerOptions configures the upstream manager.
type ManagerOptions struct {
	Repo             registry.Repository
	ToolsRepo        ToolsRepository
	Recorder         CallRecorder
	HealthInterval   time.Duration
	DefaultTimeout   time.Duration
	MaxResponseBytes int64
	ClientFactory    func(s *registry.Server, cred *registry.UpstreamCredential) (*mcp.ClientSession, error)
}

// NewManager creates a new Upstream Manager.
func NewManager(opts ManagerOptions) *Manager {
	if opts.HealthInterval <= 0 {
		opts.HealthInterval = DefaultHealthInterval
	}
	if opts.DefaultTimeout <= 0 {
		opts.DefaultTimeout = DefaultCallTimeout
	}
	if opts.MaxResponseBytes <= 0 {
		opts.MaxResponseBytes = DefaultMaxResponseBytes
	}
	if opts.Recorder == nil {
		opts.Recorder = NewSlogCallRecorder(nil)
	}

	m := &Manager{
		repo:             opts.Repo,
		toolsRepo:        opts.ToolsRepo,
		recorder:         opts.Recorder,
		servers:          make(map[string]*ManagedServer),
		healthInterval:   opts.HealthInterval,
		defaultTimeout:   opts.DefaultTimeout,
		maxResponseBytes: opts.MaxResponseBytes,
		clientFactory:    opts.ClientFactory,
		stopCh:           make(chan struct{}),
	}

	if m.clientFactory == nil {
		m.clientFactory = m.defaultClientFactory
	}

	return m
}

func (m *Manager) defaultClientFactory(s *registry.Server, cred *registry.UpstreamCredential) (*mcp.ClientSession, error) {
	rt := NewUpstreamRoundTripper(cred, nil)
	client := &http.Client{
		Transport: rt,
		Timeout:   time.Duration(s.TimeoutMS) * time.Millisecond,
	}

	transport := &mcp.StreamableClientTransport{
		Endpoint:             s.URL,
		HTTPClient:           client,
		DisableStandaloneSSE: true, // Stateless / standard request-response
	}

	mcpClient := mcp.NewClient(&mcp.Implementation{
		Name:    "tollbooth-gateway",
		Version: "0.1.0",
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), HealthCheckDeadline)
	defer cancel()

	return mcpClient.Connect(ctx, transport, nil)
}

// Start loads all servers from repository, performs initial discovery, and starts the health loop.
func (m *Manager) Start(ctx context.Context) error {
	if err := m.SyncFromDB(ctx); err != nil {
		slog.Warn("initial sync from DB completed with errors", "err", err)
	}

	// Initial parallel refresh
	m.RefreshAll(ctx)

	go m.healthLoop()
	return nil
}

// Stop shuts down the health loop and closes all client sessions.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	close(m.stopCh)
	m.mu.Unlock()

	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ms := range m.servers {
		ms.mu.Lock()
		if ms.ClientSession != nil {
			_ = ms.ClientSession.Close()
		}
		ms.mu.Unlock()
	}
}

// SyncFromDB syncs the in-memory server list with the PostgreSQL database.
func (m *Manager) SyncFromDB(ctx context.Context) error {
	dbServers, err := m.repo.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list servers from DB: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	activeSlugs := make(map[string]bool)
	for _, s := range dbServers {
		activeSlugs[s.Slug] = true
		ms, exists := m.servers[s.Slug]
		if !exists {
			ms = &ManagedServer{
				Server:           s,
				ConsecutiveFails: s.ConsecutiveFails,
			}
			m.servers[s.Slug] = ms
		} else {
			ms.mu.Lock()
			ms.Server = s
			ms.mu.Unlock()
		}
	}

	// Remove deleted servers
	for slug, ms := range m.servers {
		if !activeSlugs[slug] {
			ms.mu.Lock()
			if ms.ClientSession != nil {
				_ = ms.ClientSession.Close()
			}
			ms.mu.Unlock()
			delete(m.servers, slug)
		}
	}

	return nil
}

// healthLoop periodically checks health and reconnects to upstreams.
func (m *Manager) healthLoop() {
	ticker := time.NewTicker(m.healthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			_ = m.SyncFromDB(ctx)
			m.RefreshAll(ctx)
			cancel()
		}
	}
}

// RefreshAll fans out in parallel across all registered servers with per-upstream deadlines.
// A slow or dead upstream never delays the others.
func (m *Manager) RefreshAll(ctx context.Context) {
	m.mu.RLock()
	servers := make([]*ManagedServer, 0, len(m.servers))
	for _, ms := range m.servers {
		servers = append(servers, ms)
	}
	m.mu.RUnlock()

	var wg sync.WaitGroup
	for _, ms := range servers {
		wg.Add(1)
		go func(target *ManagedServer) {
			defer wg.Done()
			m.checkServerHealth(ctx, target)
		}(ms)
	}
	wg.Wait()
}

// checkServerHealth checks a single server, updating state and cached tools.
func (m *Manager) checkServerHealth(ctx context.Context, ms *ManagedServer) {
	ms.mu.Lock()
	s := ms.Server
	if !s.Enabled {
		ms.Server.Status = "disabled"
		if ms.ClientSession != nil {
			_ = ms.ClientSession.Close()
			ms.ClientSession = nil
		}
		ms.mu.Unlock()
		_ = m.repo.UpdateStatus(ctx, s.ID, "disabled", nil, 0)
		return
	}

	// If unreachable, check if we should back off
	now := time.Now().UTC()
	if ms.Server.Status == "unreachable" && now.Before(ms.NextCheck) {
		ms.mu.Unlock()
		return
	}
	ms.mu.Unlock()

	// Get decrypted credential
	cred, err := m.repo.GetDecryptedCredential(ctx, s.ID)
	if err != nil {
		slog.Error("failed to get credential for server", "slug", s.Slug, "err", err)
	}

	// Per-upstream deadline
	checkTimeout := HealthCheckDeadline
	if s.TimeoutMS > 0 && time.Duration(s.TimeoutMS)*time.Millisecond < checkTimeout {
		checkTimeout = time.Duration(s.TimeoutMS) * time.Millisecond
	}

	checkCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	ms.mu.Lock()
	session := ms.ClientSession
	ms.mu.Unlock()

	if session == nil {
		newSession, err := m.clientFactory(s, cred)
		if err != nil {
			m.handleServerFailure(ctx, ms, err)
			return
		}
		session = newSession
		ms.mu.Lock()
		ms.ClientSession = session
		ms.mu.Unlock()
	}

	// List tools with deadline
	toolsRes, err := session.ListTools(checkCtx, nil)
	if err != nil {
		// If session rejected (e.g. 404 after restart), re-initialize once
		if errors.Is(err, mcp.ErrSessionMissing) || strings.Contains(err.Error(), "404") {
			slog.Info("session missing or 404, attempting reconnect", "slug", s.Slug)
			if session != nil {
				_ = session.Close()
			}
			newSession, retryErr := m.clientFactory(s, cred)
			if retryErr != nil {
				m.handleServerFailure(ctx, ms, retryErr)
				return
			}
			ms.mu.Lock()
			ms.ClientSession = newSession
			session = newSession
			ms.mu.Unlock()

			toolsRes, err = session.ListTools(checkCtx, nil)
		}

		if err != nil {
			m.handleServerFailure(ctx, ms, err)
			return
		}
	}

	// Server is connected and returned tools
	m.handleServerSuccess(ctx, ms, toolsRes.Tools)
}

func (m *Manager) handleServerSuccess(ctx context.Context, ms *ManagedServer, rawTools []*mcp.Tool) {
	ms.mu.Lock()
	slug := ms.Server.Slug
	serverID := ms.Server.ID
	ms.Server.Status = "connected"
	ms.Server.LastError = nil
	ms.ConsecutiveFails = 0
	ms.NextCheck = time.Time{}

	// Validate tools and build exposed list
	validExposedTools := make([]*ExposedTool, 0, len(rawTools))
	validRawTools := make([]*mcp.Tool, 0, len(rawTools))
	warnings := 0

	for _, rt := range rawTools {
		exposedName, err := BuildExposedName(slug, rt.Name)
		if err != nil {
			slog.Warn("skipping tool violating name constraints",
				"slug", slug,
				"raw_name", rt.Name,
				"err", err,
			)
			warnings++
			continue
		}

		cleanDesc := CleanDescription(rt.Description)
		schema := rt.InputSchema
		if schema == nil {
			schema = json.RawMessage(`{"type":"object"}`)
		}

		validExposedTools = append(validExposedTools, &ExposedTool{
			ServerSlug:  slug,
			ServerID:    serverID,
			OriginName:  rt.Name,
			ExposedName: exposedName,
			Description: cleanDesc,
			InputSchema: schema,
			Annotations: rt.Annotations,
		})

		validRawTools = append(validRawTools, &mcp.Tool{
			Name:        rt.Name,
			Description: cleanDesc,
			InputSchema: schema,
			Annotations: rt.Annotations,
		})
	}

	ms.CachedTools = validExposedTools
	ms.Warnings = warnings
	ms.mu.Unlock()

	// Persist status and tools in database
	_ = m.repo.UpdateStatus(ctx, serverID, "connected", nil, 0)
	if m.toolsRepo != nil {
		_ = m.toolsRepo.SaveServerTools(ctx, serverID, validRawTools)
	}
}

func (m *Manager) handleServerFailure(ctx context.Context, ms *ManagedServer, err error) {
	ms.mu.Lock()
	s := ms.Server
	ms.ConsecutiveFails++
	sanitisedErr := SanitiseError(err, s.Slug)
	ms.Server.Status = "unreachable"
	ms.Server.LastError = &sanitisedErr

	// Clean up session if dead
	if ms.ClientSession != nil {
		_ = ms.ClientSession.Close()
		ms.ClientSession = nil
	}

	// Exponential backoff with jitter
	backoff := BaseBackoff * time.Duration(1<<min(ms.ConsecutiveFails, 5))
	if backoff > MaxBackoff {
		backoff = MaxBackoff
	}
	// Add 20% jitter
	jitter := time.Duration(rand.Int63n(int64(backoff) / 5))
	ms.NextCheck = time.Now().UTC().Add(backoff + jitter)

	serverID := s.ID
	fails := ms.ConsecutiveFails
	ms.mu.Unlock()

	slog.Warn("upstream server unreachable", "slug", s.Slug, "consecutive_fails", fails, "error", sanitisedErr)
	_ = m.repo.UpdateStatus(ctx, serverID, "unreachable", &sanitisedErr, fails)
}

// GetToolSnapshot returns a thread-safe snapshot of exposed tools from all CONNECTED and enabled servers.
// Unreachable and Disabled servers' tools are omitted per specification.
func (m *Manager) GetToolSnapshot() []*ExposedTool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot := make([]*ExposedTool, 0)
	for _, ms := range m.servers {
		ms.mu.RLock()
		if ms.Server.Enabled && ms.Server.Status == "connected" {
			snapshot = append(snapshot, ms.CachedTools...)
		}
		ms.mu.RUnlock()
	}
	return snapshot
}

// GetAllKnownTools returns tools from all servers, including unreachable ones (for routing stubbing).
func (m *Manager) GetAllKnownTools() []*ExposedTool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot := make([]*ExposedTool, 0)
	for _, ms := range m.servers {
		ms.mu.RLock()
		if ms.Server.Enabled {
			snapshot = append(snapshot, ms.CachedTools...)
		}
		ms.mu.RUnlock()
	}
	return snapshot
}

// CallTool routes a tool call to the corresponding upstream server.
func (m *Manager) CallTool(ctx context.Context, user string, exposedName string, rawArgs json.RawMessage) (*mcp.CallToolResult, error) {
	start := time.Now()
	slug, originTool, err := SplitExposedName(exposedName)
	if err != nil {
		res := &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "unknown tool"}},
		}
		m.recorder.Record(ctx, CallRecord{
			User:       user,
			ServerSlug: "",
			Tool:       exposedName,
			StartedAt:  start,
			Duration:   time.Since(start),
			Outcome:    "error",
			Error:      "unknown tool",
		})
		return res, nil
	}

	m.mu.RLock()
	ms, exists := m.servers[slug]
	m.mu.RUnlock()

	if !exists {
		res := &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("server '%s' not found", slug)}},
		}
		m.recorder.Record(ctx, CallRecord{
			User:       user,
			ServerSlug: slug,
			Tool:       exposedName,
			StartedAt:  start,
			Duration:   time.Since(start),
			Outcome:    "error",
			Error:      "server not found",
		})
		return res, nil
	}

	ms.mu.RLock()
	enabled := ms.Server.Enabled
	status := ms.Server.Status
	timeoutMS := ms.Server.TimeoutMS
	ms.mu.RUnlock()

	if !enabled {
		msg := fmt.Sprintf("server '%s' is disabled", slug)
		res := &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		}
		m.recorder.Record(ctx, CallRecord{
			User:       user,
			ServerSlug: slug,
			Tool:       exposedName,
			StartedAt:  start,
			Duration:   time.Since(start),
			Outcome:    "error",
			Error:      msg,
		})
		return res, nil
	}

	if status == "unreachable" {
		msg := fmt.Sprintf("server '%s' is unreachable", slug)
		res := &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		}
		m.recorder.Record(ctx, CallRecord{
			User:       user,
			ServerSlug: slug,
			Tool:       exposedName,
			StartedAt:  start,
			Duration:   time.Since(start),
			Outcome:    "error",
			Error:      msg,
		})
		return res, nil
	}

	// Timeout configuration
	timeout := m.defaultTimeout
	if timeoutMS > 0 {
		timeout = time.Duration(timeoutMS) * time.Millisecond
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ms.mu.RLock()
	session := ms.ClientSession
	ms.mu.RUnlock()

	if session == nil {
		cred, _ := m.repo.GetDecryptedCredential(callCtx, ms.Server.ID)
		newSession, err := m.clientFactory(ms.Server, cred)
		if err != nil {
			msg := fmt.Sprintf("server '%s' is unreachable", slug)
			res := &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: msg}},
			}
			m.recorder.Record(ctx, CallRecord{
				User:       user,
				ServerSlug: slug,
				Tool:       exposedName,
				StartedAt:  start,
				Duration:   time.Since(start),
				Outcome:    "error",
				Error:      msg,
			})
			return res, nil
		}
		ms.mu.Lock()
		ms.ClientSession = newSession
		session = newSession
		ms.mu.Unlock()
	}

	// Prepare arguments
	var args any
	if len(rawArgs) > 0 {
		var m map[string]any
		if err := json.Unmarshal(rawArgs, &m); err == nil {
			args = m
		} else {
			args = rawArgs
		}
	} else {
		args = map[string]any{}
	}

	callParams := &mcp.CallToolParams{
		Name:      originTool,
		Arguments: args,
	}

	result, err := session.CallTool(callCtx, callParams)
	if err != nil {
		// Session 404 / missing -> retry once
		if errors.Is(err, mcp.ErrSessionMissing) || strings.Contains(err.Error(), "404") {
			slog.Info("session missing or 404 during call, retrying once", "slug", slug)
			_ = session.Close()
			cred, _ := m.repo.GetDecryptedCredential(callCtx, ms.Server.ID)
			newSession, retryErr := m.clientFactory(ms.Server, cred)
			if retryErr == nil {
				ms.mu.Lock()
				ms.ClientSession = newSession
				session = newSession
				ms.mu.Unlock()
				result, err = session.CallTool(callCtx, callParams)
			}
		}
	}

	duration := time.Since(start)

	if err != nil {
		var outcome string
		var errMsg string
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded") {
			outcome = "timeout"
			errMsg = fmt.Sprintf("call timed out after %ds", int(timeout.Seconds()))
		} else {
			outcome = "error"
			errMsg = SanitiseError(err, slug)
		}

		m.recorder.Record(ctx, CallRecord{
			User:       user,
			ServerSlug: slug,
			Tool:       exposedName,
			StartedAt:  start,
			Duration:   duration,
			Outcome:    outcome,
			Error:      errMsg,
		})

		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: errMsg}},
		}, nil
	}

	// Enforce response size cap
	if result != nil {
		data, marshalErr := json.Marshal(result)
		if marshalErr == nil && int64(len(data)) > m.maxResponseBytes {
			errMsg := "response size exceeded limit"
			m.recorder.Record(ctx, CallRecord{
				User:       user,
				ServerSlug: slug,
				Tool:       exposedName,
				StartedAt:  start,
				Duration:   duration,
				Outcome:    "error",
				Error:      errMsg,
			})
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: errMsg}},
			}, nil
		}
	}

	m.recorder.Record(ctx, CallRecord{
		User:       user,
		ServerSlug: slug,
		Tool:       exposedName,
		StartedAt:  start,
		Duration:   duration,
		Outcome:    "success",
	})

	return result, nil
}

// BuildServerForRequest builds a new stateless mcp.Server per request,
// dynamically populating the tool list from the current cache snapshot.
func (m *Manager) BuildServerForRequest(req *http.Request) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "tollbooth-gateway",
		Version: "0.1.0",
	}, nil)

	var isListTools bool
	var requestedToolName string

	if req != nil && req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err == nil {
			req.Body = io.NopCloser(bytes.NewBuffer(body))
			var rpcReq struct {
				Method string `json:"method"`
				Params struct {
					Name string `json:"name"`
				} `json:"params"`
			}
			if err := json.Unmarshal(body, &rpcReq); err == nil {
				if rpcReq.Method == "tools/list" {
					isListTools = true
				} else if rpcReq.Method == "tools/call" {
					requestedToolName = rpcReq.Params.Name
				}
			}
		}
	}

	// For tools/list, return ONLY connected & enabled tools.
	// For tools/call or other methods, register all tools (or requested tool) so our handler executes.
	var toolsToRegister []*ExposedTool
	if isListTools {
		toolsToRegister = m.GetToolSnapshot()
	} else {
		toolsToRegister = m.GetAllKnownTools()
		// If client calls a specific tool not in cache (e.g. server is unreachable before caching),
		// register a stub tool handler for it so our handler can return the clear unreachable/unknown error.
		if requestedToolName != "" {
			var found bool
			for _, t := range toolsToRegister {
				if t.ExposedName == requestedToolName {
					found = true
					break
				}
			}
			if !found {
				toolsToRegister = append(toolsToRegister, &ExposedTool{
					ExposedName: requestedToolName,
					Description: "Stub tool",
					InputSchema: json.RawMessage(`{"type":"object"}`),
				})
			}
		}
	}

	for _, tool := range toolsToRegister {
		toolName := tool.ExposedName
		server.AddTool(&mcp.Tool{
			Name:        toolName,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			Annotations: tool.Annotations,
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Chunk 3 uses placeholder user. Chunk 4 replaces this with authenticated user.
			user := "bootstrap-user"
			return m.CallTool(ctx, user, toolName, req.Params.Arguments)
		})
	}

	return server
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
