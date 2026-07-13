package artifact

import (
	"context"
	"errors"
	"io"
	"lumen/internal/runstate"
	"os"
	"testing"
)

type failCreateStore struct{ Store }

func (f failCreateStore) Create(Record) error { return errors.New("metadata outage") }

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
func TestPersistCompensatesObjectOnMetadataFailure(t *testing.T) {
	dir := t.TempDir()
	b, _ := NewLocalBackend(dir)
	data := []byte("x")
	r, _ := NewRecord(runstate.LocalOwner, "run", "x", "text/plain", data)
	err := Persist(context.Background(), failCreateStore{}, b, r, data)
	if err == nil {
		t.Fatal("outage accepted")
	}
	restarted, restartErr := NewLocalBackend(dir)
	if restartErr != nil {
		t.Fatal(restartErr)
	}
	if _, err = restarted.Get(context.Background(), r.ObjectKey); !os.IsNotExist(err) {
		t.Fatalf("orphan object remains: %v", err)
	}
}
