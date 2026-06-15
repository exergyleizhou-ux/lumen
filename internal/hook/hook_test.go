package hook

import (
	"os"
	"testing"
)

func TestNewRegistryEmpty(t *testing.T) {
	os.Unsetenv("LUMEN_HOOK_PRE_TOOL")
	os.Unsetenv("LUMEN_HOOK_POST_TOOL")
	r := NewRegistry()
	if r.HasAny() {
		t.Error("new registry with no env vars should be empty")
	}
}

func TestNewRegistryWithEnv(t *testing.T) {
	os.Setenv("LUMEN_HOOK_PRE_TOOL", "echo before")
	defer os.Unsetenv("LUMEN_HOOK_PRE_TOOL")
	r := NewRegistry()
	if !r.HasAny() {
		t.Error("registry with env var should report HasAny")
	}
	if r.PreToolUseCmd != "echo before" {
		t.Errorf("PreToolUseCmd: want 'echo before', got %q", r.PreToolUseCmd)
	}
}

func TestRegistryNoHooks(t *testing.T) {
	r := &Registry{}
	if r.HasPostLLMCall() {
		t.Error("HasPostLLMCall should be false when no hook configured")
	}
	block, msg := r.PreToolUse(nil, "bash", nil)
	if block || msg != "" {
		t.Error("empty registry should not block")
	}
}

func TestRegistryPostLLMCall(t *testing.T) {
	r := &Registry{PostLLMCallCmd: "echo transformed"}
	result := r.PostLLMCall(nil, "original", 1)
	// Hook may fail in test environment (no shell), but should not panic
	t.Logf("PostLLMCall result: %q", result)
}

func TestRegistryHasPostLLMCall(t *testing.T) {
	r := &Registry{PostLLMCallCmd: "echo"}
	if !r.HasPostLLMCall() {
		t.Error("HasPostLLMCall should be true")
	}
	r2 := &Registry{}
	if r2.HasPostLLMCall() {
		t.Error("HasPostLLMCall should be false when empty")
	}
}

func TestAllEnvVars(t *testing.T) {
	os.Setenv("LUMEN_HOOK_PRE_TOOL", "echo pre")
	os.Setenv("LUMEN_HOOK_POST_TOOL", "echo post")
	os.Setenv("LUMEN_HOOK_POST_LLM", "echo llm")
	os.Setenv("LUMEN_HOOK_SUBAGENT", "echo sub")
	os.Setenv("LUMEN_HOOK_PRE_COMPACT", "echo compact")
	defer func() {
		for _, k := range []string{"LUMEN_HOOK_PRE_TOOL", "LUMEN_HOOK_POST_TOOL", "LUMEN_HOOK_POST_LLM", "LUMEN_HOOK_SUBAGENT", "LUMEN_HOOK_PRE_COMPACT"} {
			os.Unsetenv(k)
		}
	}()
	r := NewRegistry()
	if !r.HasAny() {
		t.Error("should detect hooks")
	}
}
