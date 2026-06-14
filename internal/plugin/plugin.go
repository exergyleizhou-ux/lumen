// Package plugin manages MCP (Model Context Protocol) tool servers as
// subprocesses communicating over stdio JSON-RPC.
package plugin

import (
	"encoding/json"
	"fmt"
	"sync"
)

// ServerConfig describes one MCP server to launch.
type ServerConfig struct {
	Name    string   `json:"name"`    // unique identifier for the server
	Command string   `json:"command"` // executable to launch
	Args    []string `json:"args"`    // arguments
	Env     []string `json:"env"`     // additional environment (KEY=VAL)
}

// ToolDef is the tool description returned by a server's tools/list.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"inputSchema"`
}

// Manager manages one or more MCP server connections. It is safe for concurrent
// use (servers can be started/stopped while the agent runs).
type Manager struct {
	mu      sync.Mutex
	servers map[string]*Client
}

// NewManager creates an empty MCP server manager.
func NewManager() *Manager {
	return &Manager{servers: map[string]*Client{}}
}

// Connect starts an MCP server subprocess and performs the initialize +
// tools/list handshake. Returns the discovered tools.
func (m *Manager) Connect(cfg ServerConfig) ([]ToolDef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, dup := m.servers[cfg.Name]; dup {
		return nil, fmt.Errorf("mcp server %q is already connected", cfg.Name)
	}

	client, err := startClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("mcp %s: %w", cfg.Name, err)
	}

	tools, err := client.listTools()
	if err != nil {
		client.close()
		return nil, fmt.Errorf("mcp %s tools/list: %w", cfg.Name, err)
	}

	m.servers[cfg.Name] = client
	return tools, nil
}

// CallTool invokes a named tool on the given MCP server. serverName and
// toolName are the bare names (without the mcp__ prefix).
func (m *Manager) CallTool(serverName, toolName string, args json.RawMessage) (string, error) {
	m.mu.Lock()
	client, ok := m.servers[serverName]
	m.mu.Unlock()

	if !ok {
		return "", fmt.Errorf("mcp server %q is not connected", serverName)
	}

	return client.callTool(toolName, args)
}

// Disconnect stops an MCP server subprocess by name. Returns the number of
// tools that were registered for it.
func (m *Manager) Disconnect(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	client, ok := m.servers[name]
	if !ok {
		return fmt.Errorf("mcp server %q is not connected", name)
	}
	delete(m.servers, name)
	return client.close()
}

// ListServers returns the names of connected servers.
func (m *Manager) ListServers() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	return names
}

// Shutdown disconnects all servers.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, client := range m.servers {
		client.close()
		delete(m.servers, name)
	}
}
