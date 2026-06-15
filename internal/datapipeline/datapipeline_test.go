package datapipeline

import (
	"strings"
	"testing"
)

func TestPipelineFilter(t *testing.T) {
	p := NewPipeline("test")
	p.AddStage(NewFilterStage("f", func(r Record) bool { return r["val"].(int) > 5 }))
	var records []Record
	for i := 0; i < 10; i++ { records = append(records, Record{"id": i, "val": i}) }
	out, err := p.Run(records)
	if err != nil { t.Fatal(err) }
	if len(out) != 4 { t.Errorf("want 4, got %d", len(out)) }
}

func TestPipelineMap(t *testing.T) {
	p := NewPipeline("test")
	p.AddStage(NewMapStage("m", func(r Record) Record { return Record{"squared": r["val"].(int) * r["val"].(int)} }))
	out, _ := p.Run([]Record{{"val": 3}})
	if out[0]["squared"].(int) != 9 { t.Error("map failed") }
}

func TestPipelineAggregate(t *testing.T) {
	p := NewPipeline("test")
	p.AddStage(NewAggregateStage("agg", "grp", func(rs []Record) Record {
		sum := 0
		for _, r := range rs { sum += r["val"].(int) }
		return Record{"grp": rs[0]["grp"], "sum": sum}
	}))
	out, _ := p.Run([]Record{{"grp": "a", "val": 1}, {"grp": "a", "val": 2}, {"grp": "b", "val": 3}})
	if len(out) != 2 { t.Error("agg count") }
}

func TestCSVSourceSink(t *testing.T) {
	src := NewCSVSource("test.csv"); _, err := src.Read()
	if err == nil { t.Log("no file — expected error") }
}

func TestFormatRecords(t *testing.T) {
	s := FormatRecords([]Record{{"a": 1}})
	if !strings.Contains(s, "1 record") { t.Error("format") }
}

func TestPipelineStages(t *testing.T) {
	p := NewPipeline("s")
	p.AddStage(NewSortStage("s1", "k", false))
	p.AddStage(NewLimitStage("s2", 5))
	p.AddStage(NewCountStage("s3"))
	if p.Stages() != 3 { t.Error("stage count") }
}

func TestJSONSource(t *testing.T) {
	src := NewJSONSource("nonexistent.json")
	_, err := src.Read()
	if err == nil { t.Log("no file expected") }
}
