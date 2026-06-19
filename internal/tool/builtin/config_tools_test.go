package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"lumen/internal/configlive"
)

// The config_* tools must share ONE store: a value written by config_set has to
// be readable by config_get and appear in config_history. Previously each
// Execute built a throwaway configlive.NewStore(), so set was a no-op and get
// always reported "not found".
func TestConfigTools_SharedStoreRoundTrips(t *testing.T) {
	SetConfigStore(configlive.NewStore())
	ctx := context.Background()
	set := &ConfigSetTool{}
	get := &ConfigGetTool{}
	hist := &ConfigHistoryTool{}

	if _, err := set.Execute(ctx, json.RawMessage(`{"key":"agent.temperature","value":"0.3","source":"test"}`)); err != nil {
		t.Fatal(err)
	}
	out, err := get.Execute(ctx, json.RawMessage(`{"key":"agent.temperature"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "0.3") {
		t.Errorf("config_get should return the value config_set wrote, got %q", out)
	}
	h, err := hist.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h, "agent.temperature") {
		t.Errorf("config_history should include the set key, got %q", h)
	}
}
