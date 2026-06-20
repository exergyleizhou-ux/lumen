package control

import "lumen/internal/tool"

// coreToolNames is the coding-focused built-in set offered under
// [tools] profile = "core". It deliberately omits the off-domain tools that
// otherwise bloat every turn's context (blueprint/topology/policy/schema/
// seal+notary/csv+json+base64 converters/computer-use/llm-meta/telemetry/cron/
// workflow). Those remain compiled in and reachable as opt-in MCP servers; they
// just aren't handed to the model by default. The skill and task tools are added
// separately by the controller and are always available.
var coreToolNames = map[string]bool{
	// files & editing
	"read_file": true, "write_file": true, "edit_file": true, "multi_edit": true,
	"notebook_edit": true, "delete_range": true,
	// search & code navigation
	"ls": true, "glob": true, "grep": true, "code_search": true,
	"find_symbol": true, "find_callers": true, "find_callees": true,
	"get_call_graph": true, "list_package_symbols": true,
	// execution
	"bash": true, "bash_output": true, "wait": true, "kill_shell": true,
	// flow control
	"todo_write": true, "complete_step": true, "ask": true,
	// web
	"web_search": true, "web_fetch": true,
	// git (read-only inspection; mutations go through bash)
	"git_diff": true, "git_diff_files": true, "git_log": true,
	// LSP
	"lsp_diagnostics": true, "lsp_definition": true, "lsp_references": true,
	"lsp_hover": true, "lsp_completion": true,
	// memory
	"remember": true, "forget": true, "memories": true,
	// MCP (the extension mechanism for everything not in core)
	"mcp_connect": true, "mcp_call_tool": true, "mcp_list_tools": true,
	"mcp_list_resources": true, "mcp_list_prompts": true,
	// model switching
	"model_list": true, "model_preset": true,
}

// selectTools returns the built-ins to register for the given profile. "core"
// keeps only the coding set; "full" (the default, and any unknown value) keeps
// everything — so the change is opt-in and can't silently shrink a user's tools.
func selectTools(all []tool.Tool, profile string) []tool.Tool {
	if profile != "core" {
		return all
	}
	out := make([]tool.Tool, 0, len(all))
	for _, t := range all {
		if coreToolNames[t.Name()] {
			out = append(out, t)
		}
	}
	return out
}
