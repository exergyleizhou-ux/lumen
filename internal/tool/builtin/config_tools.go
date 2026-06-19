package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"lumen/internal/configlive"
	"lumen/internal/env"
	"lumen/internal/blueprint"
	"lumen/internal/tool"
	"strings"
)

// globalConfigStore is the shared live-config store the config_* tools read and
// write. Without it each tool built a throwaway store, so config_set never
// persisted and config_get/config_history saw nothing.
var globalConfigStore atomic.Pointer[configlive.Store]

// SetConfigStore installs the shared store (call from the host to seed it from
// the loaded config). Optional — configStore lazily creates one otherwise.
func SetConfigStore(s *configlive.Store) { globalConfigStore.Store(s) }

// configStore returns the shared store, lazily creating one so the tools still
// function when the host never installed it.
func configStore() *configlive.Store {
	if s := globalConfigStore.Load(); s != nil {
		return s
	}
	globalConfigStore.CompareAndSwap(nil, configlive.NewStore())
	return globalConfigStore.Load()
}

func init() {
	tool.RegisterBuiltin(&ConfigGetTool{})
	tool.RegisterBuiltin(&ConfigSetTool{})
	tool.RegisterBuiltin(&ConfigHistoryTool{})
	tool.RegisterBuiltin(&EnvListTool{})
	tool.RegisterBuiltin(&BlueprintBuildTool{})
	tool.RegisterBuiltin(&BlueprintValidateTool{})
}

type ConfigGetTool struct{}
func (t *ConfigGetTool) Name() string { return "config_get" }
func (t *ConfigGetTool) ReadOnly() bool { return true }
func (t *ConfigGetTool) Description() string { return "Get a live configuration value by key." }
func (t *ConfigGetTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"}},"required":["key"]}`) }
func (t *ConfigGetTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Key string }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }
	s := configStore()
	v, ok := s.Get(p.Key)
	if !ok { return fmt.Sprintf("Key %q not found. Available keys: %v", p.Key, s.Keys()), nil }
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b), nil
}

type ConfigSetTool struct{}
func (t *ConfigSetTool) Name() string { return "config_set" }
func (t *ConfigSetTool) ReadOnly() bool { return false }
func (t *ConfigSetTool) Description() string { return "Set a live configuration value with hot-reload notification." }
func (t *ConfigSetTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"},"value":{},"source":{"type":"string","default":"tool"}},"required":["key","value"]}`)
}
func (t *ConfigSetTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Key string; Value any; Source string }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }
	if p.Source == "" { p.Source = "tool" }
	s := configStore()
	s.Set(p.Key, p.Value, p.Source)
	return fmt.Sprintf("Set %q = %v [source: %s]", p.Key, p.Value, p.Source), nil
}

type ConfigHistoryTool struct{}
func (t *ConfigHistoryTool) Name() string { return "config_history" }
func (t *ConfigHistoryTool) ReadOnly() bool { return true }
func (t *ConfigHistoryTool) Description() string { return "Show recent configuration changes." }
func (t *ConfigHistoryTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","default":20}}}`) }
func (t *ConfigHistoryTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	s := configStore()
	hist := s.History(20)
	b, _ := json.MarshalIndent(hist, "", "  ")
	return string(b), nil
}

type EnvListTool struct{}
func (t *EnvListTool) Name() string { return "env_list" }
func (t *EnvListTool) ReadOnly() bool { return true }
func (t *EnvListTool) Description() string { return "List all environment variables with secrets masked." }
func (t *EnvListTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{}}`) }
func (t *EnvListTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	s := env.NewStore()
	return s.FormatDump(), nil
}

type BlueprintBuildTool struct{}
func (t *BlueprintBuildTool) Name() string { return "blueprint_build" }
func (t *BlueprintBuildTool) ReadOnly() bool { return false }
func (t *BlueprintBuildTool) Description() string { return "Build and wire components from a blueprint definition." }
func (t *BlueprintBuildTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"blueprint":{"type":"string","description":"Blueprint name to build"}},"required":["blueprint"]}`)
}
func (t *BlueprintBuildTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Blueprint string }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }
	bp := blueprint.DefaultAgentBlueprint()
	if p.Blueprint != "default" { return "", fmt.Errorf("blueprint %q not found (available: default)", p.Blueprint) }
	_, cleanup, err := blueprint.NewResolver().Build([]string{"agent"})
	if err != nil { return "", err }
	cleanup()
	return fmt.Sprintf("Blueprint %q built successfully with %d components.", bp.Name, len(bp.Components)), nil
}

type BlueprintValidateTool struct{}
func (t *BlueprintValidateTool) Name() string { return "blueprint_validate" }
func (t *BlueprintValidateTool) ReadOnly() bool { return true }
func (t *BlueprintValidateTool) Description() string { return "Validate a blueprint for dependency and structural errors." }
func (t *BlueprintValidateTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"blueprint":{"type":"string"}},"required":["blueprint"]}`)
}
func (t *BlueprintValidateTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	bp := blueprint.DefaultAgentBlueprint()
	r := blueprint.NewResolver()
	errs := r.Validate(bp)
	if len(errs) == 0 { return "✅ Blueprint valid: no issues found.", nil }
	var msgs []string
	for _, e := range errs { msgs = append(msgs, e.Error()) }
	return fmt.Sprintf("❌ %d issues:\n%s", len(errs), strings.Join(msgs, "\n")), nil
}

