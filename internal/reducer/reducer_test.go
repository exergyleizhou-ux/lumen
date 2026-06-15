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
