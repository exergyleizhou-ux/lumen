package reducer

import (
	"testing"
)

func TestWordCount(t *testing.T) {
	e := NewEngine()
	job := &Job{Name: "wc", Mapper: mapAdapter{WordCountMapper()}, Reducer: reduceAdapter{SumReducer()}, Partitions: 4, Workers: 2, Input: []Record{{Value: "hello world hello"}}}
	r := e.Run(job)
	t.Logf("reduce: %d", r.ReduceCount)
}
func TestIdentity(t *testing.T) {
	e := NewEngine()
	job := &Job{Name: "id", Mapper: mapAdapter{IdentityMapper()}, Reducer: reduceAdapter{GroupReducer()}, Partitions: 2, Workers: 1, Input: []Record{{Key: "k", Value: 1}}}
	r := e.Run(job)
	if r.MapCount != 1 {
		t.Error("map")
	}
}

func TestRunMapPhaseConcurrentAppendRace(t *testing.T) {
	// Many records across multiple workers all append into the shared shuffle
	// slice — must be synchronized. Run under -race.
	e := NewEngine()
	input := make([]Record, 300)
	for i := range input {
		input[i] = Record{Value: "alpha beta gamma delta epsilon"}
	}
	job := &Job{Name: "race", Mapper: mapAdapter{WordCountMapper()}, Reducer: reduceAdapter{SumReducer()}, Partitions: 4, Workers: 4, Input: input}
	r := e.Run(job)
	if r == nil {
		t.Fatal("nil result")
	}
	// Correctness, not just non-nil: each of the 5 words appears once per record
	// (300x). A lost/duplicated append from the shuffle race changes these counts.
	if r.ReduceCount != 5 {
		t.Fatalf("want 5 distinct words, got %d", r.ReduceCount)
	}
	for _, o := range r.Outputs {
		n, ok := o.Value.(int)
		if !ok {
			if f, isF := o.Value.(float64); isF {
				n, ok = int(f), true
			}
		}
		if !ok {
			t.Fatalf("word %q: unexpected value type %T", o.Key, o.Value)
		}
		if n != 300 {
			t.Errorf("word %q count = %d, want 300 (shuffle race lost/duplicated pairs)", o.Key, n)
		}
	}
}
