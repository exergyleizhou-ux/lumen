package control

import (
	"testing"

	"lumen/internal/tool"
	_ "lumen/internal/tool/builtin" // register the built-ins so Builtins() is populated
)

func has(ts []tool.Tool, name string) bool {
	for _, t := range ts {
		if t.Name() == name {
			return true
		}
	}
	return false
}

func TestSelectToolsCoreProfile(t *testing.T) {
	all := tool.Builtins()
	if len(all) < 50 {
		t.Fatalf("expected the full built-in set to be large, got %d", len(all))
	}
	core := selectTools(all, "core")
	if len(core) >= len(all) {
		t.Errorf("core profile must be a strict subset: %d of %d", len(core), len(all))
	}
	// The coding essentials are present.
	for _, must := range []string{"read_file", "write_file", "edit_file", "bash", "grep", "glob", "lsp_diagnostics"} {
		if !has(core, must) {
			t.Errorf("core profile is missing essential tool %q", must)
		}
	}
	// The off-domain bloat the review flagged is gone.
	for _, mustNot := range []string{"blueprint_build", "screen_capture", "cron_parse", "seal_data", "topology_build_graph", "policy_evaluate"} {
		if has(core, mustNot) {
			t.Errorf("core profile must NOT include off-domain tool %q", mustNot)
		}
	}
}

func TestSelectToolsFullIsUnchanged(t *testing.T) {
	all := tool.Builtins()
	for _, p := range []string{"full", "", "weird-value"} {
		if got := selectTools(all, p); len(got) != len(all) {
			t.Errorf("profile %q must keep all %d tools (no silent shrink), got %d", p, len(all), len(got))
		}
	}
}

func TestSelectToolsFullScienceProfile(t *testing.T) {
	all := tool.Builtins()
	sci := selectTools(all, "full_science")
	if len(sci) >= len(all) {
		t.Fatalf("full_science must be a strict subset, got %d of %d", len(sci), len(all))
	}
	for _, must := range []string{"read_file", "bash", "web_search", "mcp_call_tool"} {
		if !has(sci, must) {
			t.Errorf("full_science missing %q", must)
		}
	}
	for _, mustNot := range []string{"blueprint_build", "lsp_diagnostics", "seal_data"} {
		if has(sci, mustNot) {
			t.Errorf("full_science must not include %q", mustNot)
		}
	}
}
