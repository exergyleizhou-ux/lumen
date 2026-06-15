// Package mcplife provides MCP server lifecycle management: health checks,
// auto-reconnect, tool registration/deregistration, and connection status
// tracking. Adapted from Reasonix's mcpdiag + claw-code's mcp_lifecycle_hardened.
package mcplife

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// State represents the connection state of an MCP server.
type State string

const (
	StateDisconnected State = "disconnected"
	StateConnecting   State = "connecting"
	StateConnected    State = "connected"
	StateDegraded     State = "degraded" // connected but health check failing
	StateFailed       State = "failed"
)

// Server tracks one MCP server's lifecycle.
type Server struct {
	Name      string    `json:"name"`
	Command   string    `json:"command"`
	Args      []string  `json:"args"`
	State     State     `json:"state"`
	Tools     []string  `json:"tools"` // registered tool names
	LastSeen  time.Time `json:"last_seen"`
	FailCount int       `json:"fail_count"`
	mu        sync.Mutex
	client    MCPClient
	cancel    context.CancelFunc
	onConnect func(name string, tools []string) // called when server connects
	onDisconn func(name string)                 // called when server disconnects
}

// MCPClient is the interface an MCP server client must satisfy.
type MCPClient interface {
	Connect() ([]string, error)
	CallToolRaw(tool string, args json.RawMessage) (string, error)
	Disconnect() error
	HealthCheck() error
}

// Manager manages the lifecycle of multiple MCP servers.
type Manager struct {
	mu      sync.Mutex
	servers map[string]*Server
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewManager creates an MCP lifecycle manager with background health checks.
func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		servers: map[string]*Server{},
		ctx:     ctx,
		cancel:  cancel,
	}
	go m.healthCheckLoop()
	return m
}

// Register adds a server to the lifecycle manager. It does not connect yet.
func (m *Manager) Register(name, command string, args []string, client MCPClient) *Server {
	m.mu.Lock()
	defer m.mu.Unlock()

	srv := &Server{
		Name:    name,
		Command: command,
		Args:    args,
		State:   StateDisconnected,
		client:  client,
	}
	m.servers[name] = srv
	return srv
}

// Connect establishes a connection to a registered server. Returns the tool
// names exposed by the server. Safe to call multiple times (idempotent).
func (m *Manager) Connect(name string) ([]string, error) {
	m.mu.Lock()
	srv, ok := m.servers[name]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("mcp server %q not registered", name)
	}

	srv.mu.Lock()
	if srv.State == StateConnected {
		srv.mu.Unlock()
		return srv.Tools, nil
	}
	srv.State = StateConnecting
	srv.mu.Unlock()

	tools, err := srv.client.Connect()
	if err != nil {
		srv.mu.Lock()
		srv.State = StateFailed
		srv.FailCount++
		srv.mu.Unlock()
		return nil, fmt.Errorf("connect %s: %w", name, err)
	}

	srv.mu.Lock()
	srv.State = StateConnected
	srv.Tools = tools
	srv.FailCount = 0
	srv.LastSeen = time.Now()
	srv.mu.Unlock()

	if srv.onConnect != nil {
		srv.onConnect(name, tools)
	}

	log.Printf("mcp %s: connected with %d tools", name, len(tools))
	return tools, nil
}

// Disconnect closes a server connection gracefully.
func (m *Manager) Disconnect(name string) error {
	m.mu.Lock()
	srv, ok := m.servers[name]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("mcp server %q not found", name)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	if srv.client != nil {
		if err := srv.client.Disconnect(); err != nil {
			log.Printf("mcp %s: disconnect error: %v", name, err)
		}
	}
	srv.State = StateDisconnected

	if srv.onDisconn != nil {
		srv.onDisconn(name)
	}
	return nil
}

// Reconnect attempts to re-establish a failed connection.
func (m *Manager) Reconnect(name string) error {
	m.Disconnect(name)
	_, err := m.Connect(name)
	return err
}

// List returns all registered servers and their states.
func (m *Manager) List() []*Server {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Server, 0, len(m.servers))
	for _, s := range m.servers {
		out = append(out, s)
	}
	return out
}

// CallTool invokes a named tool on a connected server.
func (m *Manager) CallTool(serverName, toolName string, args json.RawMessage) (string, error) {
	m.mu.Lock()
	srv, ok := m.servers[serverName]
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("mcp server %q not registered", serverName)
	}

	srv.mu.Lock()
	if srv.State != StateConnected && srv.State != StateDegraded {
		srv.mu.Unlock()
		return "", fmt.Errorf("mcp server %q is %s", serverName, srv.State)
	}
	srv.mu.Unlock()

	result, err := srv.client.CallToolRaw(toolName, args)
	srv.mu.Lock()
	srv.LastSeen = time.Now()
	if err != nil {
		srv.State = StateDegraded
	}
	srv.mu.Unlock()
	return result, err
}

// OnConnect registers a callback for when a server connects.
func (s *Server) OnConnect(fn func(name string, tools []string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onConnect = fn
}

// OnDisconnect registers a callback for when a server disconnects.
func (s *Server) OnDisconnect(fn func(name string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onDisconn = fn
}

// Shutdown disconnects all servers and stops health checks.
func (m *Manager) Shutdown() {
	m.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, srv := range m.servers {
		srv.mu.Lock()
		if srv.client != nil {
			srv.client.Disconnect()
		}
		srv.State = StateDisconnected
		srv.mu.Unlock()
		log.Printf("mcp %s: shutdown", name)
	}
}

// ── Health check loop ──────────────────────────────────────

func (m *Manager) healthCheckLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.runHealthChecks()
		}
	}
}

func (m *Manager) runHealthChecks() {
	m.mu.Lock()
	servers := make([]*Server, 0, len(m.servers))
	for _, s := range m.servers {
		servers = append(servers, s)
	}
	m.mu.Unlock()

	for _, srv := range servers {
		srv.mu.Lock()
		if srv.State != StateConnected && srv.State != StateDegraded {
			srv.mu.Unlock()
			continue
		}
		client := srv.client
		name := srv.Name
		srv.mu.Unlock()

		if client == nil {
			continue
		}

		if err := client.HealthCheck(); err != nil {
			srv.mu.Lock()
			srv.State = StateDegraded
			srv.FailCount++
			srv.mu.Unlock()
			log.Printf("mcp %s: health check failed: %v", name, err)

			// Auto-reconnect on repeated failures
			if srv.FailCount >= 3 {
				log.Printf("mcp %s: auto-reconnecting after %d failures", name, srv.FailCount)
				m.Reconnect(name)
			}
		} else {
			srv.mu.Lock()
			if srv.State == StateDegraded {
				srv.State = StateConnected
			}
			srv.FailCount = 0
			srv.LastSeen = time.Now()
			srv.mu.Unlock()
		}
	}
}

// ── Callbacks for use with the agent's tool registry ──────

// OnConnectHandler returns a function suitable for registering MCP tools
// into a tool registry when a server connects.
func (m *Manager) OnConnectHandler(register func(serverName, toolName string)) func(name string, tools []string) {
	return func(name string, tools []string) {
		for _, t := range tools {
			register(name, t)
		}
	}
}

// OnDisconnectHandler returns a function suitable for unregistering MCP tools.
func (m *Manager) OnDisconnectHandler(unregister func(serverName string)) func(name string) {
	return func(name string) {
		unregister(name)
	}
}
