package tool

import (
	"context"
	"encoding/json"
	"testing"
)

// testTool is a simple tool for registry tests.
type testTool struct {
	name     string
	desc     string
	schema   json.RawMessage
	readOnly bool
	output   string
}

func (t *testTool) Name() string            { return t.name }
func (t *testTool) Description() string     { return t.desc }
func (t *testTool) ReadOnly() bool          { return t.readOnly }
func (t *testTool) Schema() json.RawMessage { return t.schema }
func (t *testTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return t.output, nil
}

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r.Len() != 0 {
		t.Error("new registry should be empty")
	}
	if len(r.Names()) != 0 {
		t.Error("new registry names should be empty")
	}
}

func TestAddAndGet(t *testing.T) {
	r := NewRegistry()
	tool := &testTool{name: "bash", readOnly: false, schema: json.RawMessage(`{"type":"object"}`)}

	r.Add(tool)

	if r.Len() != 1 {
		t.Errorf("Len: expected 1, got %d", r.Len())
	}

	got, ok := r.Get("bash")
	if !ok {
		t.Fatal("Get('bash') should find the tool")
	}
	if got.Name() != "bash" {
		t.Errorf("name mismatch: %s", got.Name())
	}
}

func TestGetMissing(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("Get should return false for missing tool")
	}
}

func TestSchemas(t *testing.T) {
	r := NewRegistry()
	r.Add(&testTool{name: "read", readOnly: true, schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)})
	r.Add(&testTool{name: "write", readOnly: false, schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}}}`)})

	schemas := r.Schemas()
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas, got %d", len(schemas))
	}
	// Schemas should be sorted by name
	if schemas[0].Name != "read" {
		t.Errorf("first should be 'read' (sorted), got %s", schemas[0].Name)
	}
	if schemas[1].Name != "write" {
		t.Errorf("second should be 'write', got %s", schemas[1].Name)
	}
}

func TestSchemasAreStable(t *testing.T) {
	r := NewRegistry()
	r.Add(&testTool{name: "x", schema: json.RawMessage(`{"type":"object"}`)})

	s1 := r.Schemas()
	s2 := r.Schemas()

	// Multiple calls should produce identical output (for cache stability)
	bytes1, _ := json.Marshal(s1)
	bytes2, _ := json.Marshal(s2)
	if string(bytes1) != string(bytes2) {
		t.Error("Schemas() should be stable across calls")
	}
}

func TestRemovePrefix(t *testing.T) {
	r := NewRegistry()
	r.Add(&testTool{name: "mcp__gh__search", desc: "MCP tool", schema: json.RawMessage(`{}`)})
	r.Add(&testTool{name: "mcp__gh__list", desc: "MCP tool", schema: json.RawMessage(`{}`)})
	r.Add(&testTool{name: "bash", desc: "builtin", schema: json.RawMessage(`{}`)})

	removed := r.RemovePrefix("mcp__gh__")
	if removed != 2 {
		t.Errorf("expected 2 removed, got %d", removed)
	}
	if r.Len() != 1 {
		t.Errorf("expected 1 remaining tool, got %d", r.Len())
	}
	if _, ok := r.Get("bash"); !ok {
		t.Error("bash should still be registered")
	}
}

func TestSplitMCPName(t *testing.T) {
	server, tool, ok := SplitMCPName("mcp__github__search_code")
	if !ok {
		t.Fatal("Should parse valid MCP name")
	}
	if server != "github" {
		t.Errorf("server: want github, got %s", server)
	}
	if tool != "search_code" {
		t.Errorf("tool: want search_code, got %s", tool)
	}
}

func TestSplitMCPNameNonMCP(t *testing.T) {
	_, _, ok := SplitMCPName("bash")
	if ok {
		t.Error("non-MCP name should not parse")
	}
	_, _, ok = SplitMCPName("mcp__")
	if ok {
		t.Error("malformed MCP name should not parse")
	}
}

func TestNames(t *testing.T) {
	r := NewRegistry()
	r.Add(&testTool{name: "b"})
	r.Add(&testTool{name: "a"})
	r.Add(&testTool{name: "c"})

	names := r.Names()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	// Names should preserve insertion order
	if names[0] != "b" || names[1] != "a" || names[2] != "c" {
		t.Errorf("names should preserve insertion order, got %v", names)
	}
}

func TestBuiltins(t *testing.T) {
	// builtins should be populated by init() from builtin package
	// We can't guarantee which tools are registered without importing builtin
	// but at minimum, the map should exist
	_ = Builtins()
}

type effectTool struct {
	testTool
	effects Effects
}

func (t *effectTool) Effects() Effects { return t.effects }

func TestEffectsOfUsesExplicitContractAndSafeFallback(t *testing.T) {
	cases := []struct {
		name string
		tool Tool
		want Effects
	}{
		{name: "explicit command", tool: &effectTool{testTool: testTool{name: "bash"}, effects: Effects{RunsCommands: true}}, want: Effects{RunsCommands: true}},
		{name: "legacy read", tool: &testTool{name: "read", readOnly: true}, want: Effects{}},
		{name: "legacy writer remains conservative", tool: &testTool{name: "write", readOnly: false}, want: Effects{WritesFiles: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectsOf(tc.tool); got != tc.want {
				t.Fatalf("EffectsOf()=%+v want %+v", got, tc.want)
			}
		})
	}
}
