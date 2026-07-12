package server

import (
	"context"
	"testing"
	"time"
)

func TestDetachedRunSurvivesRequestCancellation(t *testing.T) {
	s := &Server{}
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	runCtx, cleanup := s.beginActiveRun(requestCtx, "run_detached", time.Minute)
	defer cleanup()

	cancelRequest()
	select {
	case <-runCtx.Done():
		t.Fatalf("request cancellation leaked into detached run: %v", runCtx.Err())
	case <-time.After(20 * time.Millisecond):
	}

	if !s.cancelActiveRun("run_detached") {
		t.Fatal("active run was not canceled")
	}
	select {
	case <-runCtx.Done():
		if runCtx.Err() != context.Canceled {
			t.Fatalf("run error=%v", runCtx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("explicit cancel did not stop run")
	}
}

func TestActiveRunCleanupRemovesCancellationEntry(t *testing.T) {
	s := &Server{}
	_, cleanup := s.beginActiveRun(context.Background(), "run_cleanup", time.Minute)
	cleanup()
	cleanup()
	if s.cancelActiveRun("run_cleanup") {
		t.Fatal("cleaned run remained active")
	}
}
