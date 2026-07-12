package server

import (
	"strings"
	"testing"

	"lumen/internal/control"
	"lumen/internal/event"
)

func TestConfigureRuntimeRecoversPanic(t *testing.T) {
	s := &Server{}
	rt := &requestRuntime{ctrl: control.New(), configureTest: func() { panic("provider boom") }}
	err := s.configureRuntime(rt, event.Discard, "")
	if err == nil || !strings.Contains(err.Error(), "configure panic: provider boom") {
		t.Fatalf("unexpected error: %v", err)
	}
}
