# Lumen Tool Groups — K3 Grouped Top-K Inspired

Inspired by Kimi K3's MoE grouped top-K routing (select group first, then expert within group).
Same idea: pre-filter tool groups by context, then dispatch within selected groups.

## Architecture

```
Context → GroupGate::select_groups(top_k=3) → [coding, file_ops, search]
                       ↓
           GroupDispatch::select_tools(top_k=5) → [read_file, grep, smart_explore, ...]
```

## Tool Groups

| Group | Context Trigger | Tool Count |
|-------|----------------|------------|
| `coding::edit` | "edit", "fix", "write", "create" | search_replace, write, sed |
| `coding::explore` | "find", "search", "look", "check" | grep, read_file, list_dir, smart_explore |
| `coding::build` | "build", "compile", "run", "test" | cargo_build, cargo_test, cargo_check |
| `coding::git` | "commit", "push", "merge", "branch" | git_commit, git_push, git_diff |
| `file::read` | "read", "open", "show", "cat" | read_file, read_image, read_pdf |
| `file::write` | "write", "save", "create" | write, search_replace, mkdir |
| `file::delete` | "delete", "remove", "clean" | delete_file, trash |
| `shell::unix` | unix-only commands | bash, sh, zsh runner |
| `shell::windows` | windows-only commands | powershell, cmd runner |
| `shell::universal` | cross-platform commands | echo, mkdir, ls equivalent |
| `network::http` | "download", "fetch", "curl" | web_request, web_search |
| `network::api` | "github", "api", "endpoint" | github_api, gh_cli |
| `mcp::server` | MCP tool server operations | mcp_list_tools, mcp_call |
| `mcp::client` | MCP client operations | mcp_connect, mcp_discover |
| `computer::vision` | "screenshot", "image", "ocr" | screenshot, image_analyze |
| `computer::interact` | "click", "type", "scroll" | mouse_click, keyboard_type |
| `science::compute` | "calculate", "analyze", "bench" | benchmark, profile, measure |

## Priority Load

Always load (like K3's BF16 attention layers):
- `coding::explore` — needed for context gathering
- `file::read` — needed for first file access
- `shell::universal` — needed for basic commands

Lazy load (like K3's MXFP4 Linear weights):
- `computer::*` — only when GUI interaction requested
- `science::*` — only when computation/batch work requested
- `network::*` — only when external API call requested
- `mcp::*` — only when MCP server detected

## Future Implementation

```rust
// TODO(windows-fix-ps-scripts): implement lazy tool loading
struct GroupGate {
    groups: HashMap<GroupId, Vec<ToolId>>,
    scores: Vec<(GroupId, f32)>,
}

impl GroupGate {
    fn select(&self, context: &AgentContext, top_k: usize) -> Vec<ToolId> {
        // Step 1: score groups by context relevance
        // Step 2: select top_k groups
        // Step 3: return tools from selected groups
        // Avoids loading all 208 files at startup
        todo!("K3 grouped top-K lazy tool dispatch")
    }
}
```
