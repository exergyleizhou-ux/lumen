package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	labruntime "lumen/internal/science/lab/runtime"
)

func TestListDomainsTool(t *testing.T) {
	home := t.TempDir()
	sci := filepath.Join(home, ".lumen", "science")
	fleet, err := labruntime.NewFleetManager(sci)
	if err != nil {
		t.Fatal(err)
	}
	tool := &ListDomainsTool{Fleet: fleet}
	out, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Fatal("empty output")
	}
}
