package builtin

import (
	"sync"
	"testing"

	"lumen/internal/mcplife"
)

// The shared MCP client registry is read/ranged by the read-only mcp_list_*
// tools (which run in parallel batches) while mcp_connect writes to it (e.g. a
// background sub-agent connecting). Concurrent map access must not race/panic.
// Run with -race to catch regressions.
func TestMCPClientsConcurrentAccess(t *testing.T) {
	// Isolate from any other state in the global map.
	mcpMu.Lock()
	saved := mcpClients
	mcpClients = map[string]*mcplife.Client{}
	mcpMu.Unlock()
	defer func() {
		mcpMu.Lock()
		mcpClients = saved
		mcpMu.Unlock()
	}()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); setMCPClient("server", nil) }() // writer (mcp_connect)
		go func() { defer wg.Done(); _ = mcpClientsSnapshot() }()    // ranger (mcp_list_tools all-servers)
		go func() { defer wg.Done(); _ = getMCPClient("") }()        // reader (mcp_list_resources)
	}
	wg.Wait()
}
