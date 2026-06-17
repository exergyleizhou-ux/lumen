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
	if r := e.Run(job); r == nil {
		t.Fatal("nil result")
	}
}
