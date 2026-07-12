package artifact

import (
	"context"
	"io"
	"lumen/internal/runstate"
	"testing"
)

func TestLocalBackendSurvivesRestartAndRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	b, _ := NewLocalBackend(dir)
	o := runstate.Owner{UserID: "u", WorkspaceID: "w"}
	data := []byte("durable")
	r, _ := NewRecord(o, "run", "x.txt", "text/plain", data)
	s := NewMemoryStore()
	if err := Persist(context.Background(), s, b, r, data); err != nil {
		t.Fatal(err)
	}
	restarted, _ := NewLocalBackend(dir)
	rc, err := restarted.Get(context.Background(), r.ObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	if string(got) != "durable" {
		t.Fatalf("got %q", got)
	}
	if err = restarted.Put(context.Background(), "../escape", nil, 0, ""); err == nil {
		t.Fatal("traversal accepted")
	}
}
