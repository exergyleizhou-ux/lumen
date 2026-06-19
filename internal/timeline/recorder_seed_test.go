package timeline

import (
	"path/filepath"
	"testing"
)

// The timeline file is opened in append mode and "survives process restarts",
// but NewRecorder left the turn counter at 0 — so a resumed session restarted
// at turn 1, colliding with the turn 1 already in the file (duplicate "Turn 1"
// headers). A new recorder on an existing file must continue from its max turn.
func TestNewRecorder_SeedsTurnFromExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timeline.jsonl")

	r1, err := NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	r1.NewTurn()
	r1.Record(Entry{Kind: "a"}) // turn 1
	r1.NewTurn()
	r1.Record(Entry{Kind: "b"}) // turn 2
	r1.Close()

	r2, err := NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	r2.NewTurn() // must be turn 3, not a reset-to-1
	r2.Record(Entry{Kind: "c"})
	r2.Close()

	entries, err := LoadTimeline(path)
	if err != nil {
		t.Fatal(err)
	}
	last := entries[len(entries)-1]
	if last.Turn != 3 {
		t.Errorf("resumed turn should continue from 2 → 3, got %d (counter reset on restart)", last.Turn)
	}
}
