// Package builtin registers all compile-time tools via init().
// Each tool file self-registers into the global builtin set; the agent creates
// a per-run Registry with a subset of enabled builtins.
package builtin

import (
	"lumen/internal/tool"
)

// RegisterAll calls each tool's init-time registration. Call it when the
// builtin package is non-trivially imported (e.g. from cmd/agent).
func RegisterAll() {
	// Each tool file uses init() to call tool.RegisterBuiltin.
	// This function exists as an explicit anchor so the compiler
	// doesn't prune the package when only side-effect imports are used.
	_ = tool.Builtins()
}
