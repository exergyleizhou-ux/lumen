// mcp_real_tools.go — Real MCP client tools. Connect to any MCP server
// and expose its tools/resources/prompts to the agent.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"lumen/internal/mcplife"
	"lumen/internal/tool"
)

// ── Shared MCP clients ─────────────────────────────────────────────

var mcpClients = map[string]*mcplife.Client{}

func init() {
	tool.RegisterBuiltin(&MCPConnectTool{})
	tool.RegisterBuiltin(&MCPListToolsTool{})
	tool.RegisterBuiltin(&MCPCallToolTool{})
	tool.RegisterBuiltin(&MCPListResourcesTool{})
	tool.RegisterBuiltin(&MCPListPromptsTool{})
}

type MCPConnectTool struct{}
func (t *MCPConnectTool) Name() string     { return "mcp_connect" }
func (t *MCPConnectTool) ReadOnly() bool   { return false }
func (t *MCPConnectTool) Description() string {
	return "Connect to an MCP (Model Context Protocol) server over stdio. Provide the command to launch the server. Returns available capabilities. Example: {\"command\":\"npx\",\"args\":[\"-y\",\"@github/mcp-server\"]}"
}
func (t *MCPConnectTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Server command to launch"},"args":{"type":"array","items":{"type":"string"},"description":"Arguments for the command"}},"required":["command"]}`)
}
func (t *MCPConnectTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Command string; Args []string }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }
	if p.Command == "" { return "", fmt.Errorf("command is required") }

	client, err := mcplife.NewMCPClient(p.Command, p.Args)
	if err != nil { return "", fmt.Errorf("mcp: %w", err) }

	if err := client.Initialize("lumen", "1.0"); err != nil {
		client.Close()
		return "", fmt.Errorf("initialize: %w", err)
	}

	key := p.Command
	if len(p.Args) > 0 { key = p.Command + " " + strings.Join(p.Args, " ") }
	mcpClients[key] = client

	// List tools/resources/prompts counts
	tools, _ := client.ListTools()
	resources, _ := client.ListResources()
	prompts, _ := client.ListPrompts()

	return fmt.Sprintf("Connected to MCP server %q.\nTools: %d, Resources: %d, Prompts: %d\nUse mcp_list_tools to see available tools.",
		key, len(tools), len(resources), len(prompts)), nil
}

type MCPListToolsTool struct{}
func (t *MCPListToolsTool) Name() string     { return "mcp_list_tools" }
func (t *MCPListToolsTool) ReadOnly() bool   { return true }
func (t *MCPListToolsTool) Description() string {
	return "List all tools exposed by a connected MCP server. Connect first with mcp_connect."
}
func (t *MCPListToolsTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"server":{"type":"string","description":"Server key from mcp_connect (command or command+args)"}}}`)
}
func (t *MCPListToolsTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Server string }
	json.Unmarshal(args, &p)
	client := mcpClients[p.Server]
	if client == nil {
		// Return all servers' tools
		if len(mcpClients) == 0 { return "No MCP servers connected. Use mcp_connect first.", nil }
		var sb strings.Builder
		for key, c := range mcpClients {
			tools, err := c.ListTools()
			if err != nil { fmt.Fprintf(&sb, "%s: error %v\n", key, err); continue }
			fmt.Fprintf(&sb, "%s: %d tools\n", key, len(tools))
			for _, t := range tools {
				fmt.Fprintf(&sb, "  • %s — %s\n", t.Name, truncDesc(t.Description, 60))
			}
		}
		return sb.String(), nil
	}
	tools, err := client.ListTools()
	if err != nil { return "", fmt.Errorf("list tools: %w", err) }
	if len(tools) == 0 { return "No tools exposed by this server.", nil }
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d tools:\n", len(tools))
	for _, t := range tools {
		fmt.Fprintf(&sb, "  • %s — %s\n", t.Name, truncDesc(t.Description, 80))
	}
	return sb.String(), nil
}

type MCPCallToolTool struct{}
func (t *MCPCallToolTool) Name() string     { return "mcp_call_tool" }
func (t *MCPCallToolTool) ReadOnly() bool   { return false }
func (t *MCPCallToolTool) Description() string {
	return "Call a tool on a connected MCP server. Use mcp_list_tools to see available tools first."
}
func (t *MCPCallToolTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"server":{"type":"string","description":"Server key from mcp_connect"},"tool":{"type":"string","description":"Tool name"},"args":{"type":"object","description":"Tool arguments"}},"required":["server","tool"]}`)
}
func (t *MCPCallToolTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Server, Tool string; Args map[string]any }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }
	client := mcpClients[p.Server]
	if client == nil { return "", fmt.Errorf("server %q not connected. Use mcp_connect first.", p.Server) }

	result, err := client.CallTool(p.Tool, p.Args)
	if err != nil { return "", fmt.Errorf("call tool: %w", err) }

	var sb strings.Builder
	if result.IsError { sb.WriteString("(error) ") }
	for _, c := range result.Content {
		if c.Type == "text" { sb.WriteString(c.Text) } else { fmt.Fprintf(&sb, "[%s: %s]\n", c.Type, c.Text) }
	}
	if sb.Len() == 0 { return "(no output)", nil }
	return sb.String(), nil
}

type MCPListResourcesTool struct{}
func (t *MCPListResourcesTool) Name() string     { return "mcp_list_resources" }
func (t *MCPListResourcesTool) ReadOnly() bool   { return true }
func (t *MCPListResourcesTool) Description() string { return "List resources exposed by a connected MCP server." }
func (t *MCPListResourcesTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"server":{"type":"string"}}}`)
}
func (t *MCPListResourcesTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Server string }
	json.Unmarshal(args, &p)
	client := getMCPClient(p.Server)
	if client == nil { return "No MCP servers connected.", nil }
	resources, err := client.ListResources()
	if err != nil { return "", err }
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d resources:\n", len(resources))
	for _, r := range resources { fmt.Fprintf(&sb, "  • %s — %s [%s]\n", r.Name, descOr(r.Description, r.URI), r.MIMEType) }
	return sb.String(), nil
}

type MCPListPromptsTool struct{}
func (t *MCPListPromptsTool) Name() string     { return "mcp_list_prompts" }
func (t *MCPListPromptsTool) ReadOnly() bool   { return true }
func (t *MCPListPromptsTool) Description() string { return "List prompt templates exposed by a connected MCP server." }
func (t *MCPListPromptsTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"server":{"type":"string"}}}`)
}
func (t *MCPListPromptsTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Server string }
	json.Unmarshal(args, &p)
	client := getMCPClient(p.Server)
	if client == nil { return "No MCP servers connected.", nil }
	prompts, err := client.ListPrompts()
	if err != nil { return "", err }
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d prompts:\n", len(prompts))
	for _, p := range prompts {
		fmt.Fprintf(&sb, "  • %s — %s\n", p.Name, descOr(p.Description, "no description"))
	}
	return sb.String(), nil
}

func getMCPClient(key string) *mcplife.Client {
	if key != "" { return mcpClients[key] }
	for _, c := range mcpClients { return c }
	return nil
}

func truncDesc(s string, n int) string {
	if len(s) <= n { return s }
	return s[:n-3] + "..."
}

func descOr(desc, fallback string) string {
	if desc != "" { return desc }
	return fallback
}
