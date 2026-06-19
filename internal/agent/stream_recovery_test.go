package agent

import (
	"context"
	"strings"
	"testing"

	"lumen/internal/provider"
)

// On a mid-stream interruption, the partial assistant text streamed before the
// cut must be preserved in the session before the recovery prompt, so the model
// sees what it already said and doesn't repeat or lose work. (Reuses the
// interruptThenOKProvider from agent_test.go, which streams "partial" then
// interrupts.)
func TestStreamRecovery_PreservesPartialAssistantText(t *testing.T) {
	a := New(&interruptThenOKProvider{}, testRegistry(), NewSession(""), Options{MaxSteps: 5})
	if err := a.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, m := range a.Session().Snapshot() {
		if m.Role == provider.RoleAssistant && strings.Contains(m.Content, "partial") {
			return
		}
	}
	t.Error("partial assistant text before the interruption was dropped from the session")
}
