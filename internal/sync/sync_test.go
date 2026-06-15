package sync

import (
	"encoding/json"
	"testing"
	"time"
)

func makeRecord(key string, value string) *Record {
	v, _ := json.Marshal(value)
	return &Record{
		Key:       key,
		Value:     v,
		UpdatedAt: time.Now(),
	}
}

func TestEngine_PutAndGet(t *testing.T) {
	e := NewEngine(StrategyLastWriteWins)

	rec := makeRecord("k1", "v1")
	e.Put(rec)

	got, ok := e.Get("k1")
	if !ok {
		t.Fatal("expected to find record")
	}
	if string(got.Value) != `"v1"` {
		t.Fatalf("expected v1, got %s", got.Value)
	}
}

func TestEngine_Delete(t *testing.T) {
	e := NewEngine(StrategyLastWriteWins)

	e.Put(makeRecord("k1", "v1"))
	if !e.Delete("k1", "tester") {
		t.Fatal("expected delete to succeed")
	}

	_, ok := e.Get("k1")
	if ok {
		t.Fatal("expected record to be gone after delete")
	}
}

func TestEngine_MergeLastWriteWins(t *testing.T) {
	e := NewEngine(StrategyLastWriteWins)

	now := time.Now()
	base := []*Record{
		{Key: "a", Value: json.RawMessage(`"base"`), UpdatedAt: now},
	}
	local := []*Record{
		{Key: "a", Value: json.RawMessage(`"local"`), UpdatedAt: now.Add(time.Hour)},
	}
	remote := []*Record{
		{Key: "a", Value: json.RawMessage(`"remote"`), UpdatedAt: now.Add(-time.Hour)},
	}

	result := e.Merge(base, local, remote)
	if result.Applied != 1 {
		t.Fatalf("expected 1 applied, got %d", result.Applied)
	}
	// Even with LWW, conflicts are recorded (just auto-resolved).
	if len(result.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict recorded, got %d", len(result.Conflicts))
	}

	rec, _ := e.Get("a")
	if string(rec.Value) != `"local"` {
		t.Fatalf("expected local to win, got %s", rec.Value)
	}
}

func TestEngine_MergeCRDTLike(t *testing.T) {
	e := NewEngine(StrategyCRDTLike)

	local := []*Record{
		{Key: "obj", Value: json.RawMessage(`{"x":1,"y":2}`), UpdatedAt: time.Now()},
	}
	remote := []*Record{
		{Key: "obj", Value: json.RawMessage(`{"y":20,"z":3}`), UpdatedAt: time.Now()},
	}

	result := e.Merge(nil, local, remote)
	rec, _ := e.Get("obj")

	var m map[string]json.RawMessage
	json.Unmarshal(rec.Value, &m)
	if _, ok := m["z"]; !ok {
		t.Fatalf("expected merged field 'z', got %s", rec.Value)
	}
	_ = result
}

func TestEngine_Journal(t *testing.T) {
	e := NewEngine(StrategyLastWriteWins)

	e.Put(makeRecord("j1", "v1"))
	e.Put(makeRecord("j2", "v2"))

	// Merge triggers journal entries.
	e.Merge(nil, []*Record{makeRecord("j3", "v3")}, nil)

	journal := e.Journal()
	if len(journal) == 0 {
		t.Fatal("expected journal entries")
	}
}

func TestEngine_Changes(t *testing.T) {
	e := NewEngine(StrategyLastWriteWins)

	e.Put(makeRecord("c1", "v1"))
	e.Put(makeRecord("c1", "v2")) // Update.

	changes := e.Changes()
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}
	if changes[0].Type != ChangeCreate {
		t.Fatalf("expected first change to be create, got %v", changes[0].Type)
	}
	if changes[1].Type != ChangeUpdate {
		t.Fatalf("expected second change to be update, got %v", changes[1].Type)
	}
}

func TestEngine_Conflicts(t *testing.T) {
	e := NewEngine(StrategyManual)

	base := []*Record{
		{Key: "conflict", Value: json.RawMessage(`"base"`), UpdatedAt: time.Now()},
	}
	local := []*Record{
		{Key: "conflict", Value: json.RawMessage(`"local"`), UpdatedAt: time.Now()},
	}
	remote := []*Record{
		{Key: "conflict", Value: json.RawMessage(`"remote"`), UpdatedAt: time.Now()},
	}

	result := e.Merge(base, local, remote)
	if len(result.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(result.Conflicts))
	}
}

func TestEngine_Stats(t *testing.T) {
	e := NewEngine(StrategyLastWriteWins)
	e.Put(makeRecord("s1", "v1"))
	e.Put(makeRecord("s2", "v2"))
	e.Delete("s2", "tester")

	st := e.Stats()
	if st["total_records"] != 2 {
		t.Fatalf("expected 2 total, got %d", st["total_records"])
	}
	if st["deleted"] != 1 {
		t.Fatalf("expected 1 deleted, got %d", st["deleted"])
	}
}
