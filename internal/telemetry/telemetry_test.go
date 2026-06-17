package telemetry

import (
	"path/filepath"
	"testing"
)

func TestFeedbackSubmitReportsWriteError(t *testing.T) {
	// dir's parent does not exist, so the persistence WriteFile must fail. Submit
	// must report that error rather than swallow it and let the UI claim success.
	fs := &FeedbackStore{dir: filepath.Join(t.TempDir(), "missing", "deeper")}
	_, err := fs.Submit("text", "hello", "ctx", "")
	if err == nil {
		t.Fatal("Submit should report the write failure, not swallow it")
	}
}

func TestFeedbackSubmitOK(t *testing.T) {
	fs := &FeedbackStore{dir: t.TempDir()}
	fe, err := fs.Submit("text", "hello", "ctx", "")
	if err != nil {
		t.Fatalf("Submit to a writable dir should succeed, got %v", err)
	}
	if fe == nil || fe.Message != "hello" {
		t.Fatalf("Submit should return the stored entry, got %+v", fe)
	}
}
