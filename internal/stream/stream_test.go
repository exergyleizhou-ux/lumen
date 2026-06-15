package stream

import (
	"context"
	"testing"
	"time"
)

func TestMetrics(t *testing.T) {
	m := NewMetrics()
	m.RecordIn(100)
	m.RecordOut(95)
	m.RecordDrop(5)
	m.RecordWindowClosed(10)
	m.RecordWindowClosed(20)
	m.RecordLatency(time.Millisecond)
	m.RecordLatency(2 * time.Millisecond)
	s := m.FormatMetrics()
	if s == "" {
		t.Error("format")
	}
}
func TestPartitioner(t *testing.T) {
	p := NewPartitioner(4)
	p1 := p.Partition(&Record{Key: "user-1"})
	p2 := p.Partition(&Record{Key: "user-1"})
	if p1 != p2 {
		t.Error("same key should go to same partition")
	}
}
func TestMockSource(t *testing.T) {
	records := []Record{{Key: "a", Value: 1}, {Key: "b", Value: 2}}
	src := NewMockSource("test", records)
	batch, _ := src.Poll(context.Background())
	if len(batch) != 2 {
		t.Error("poll")
	}
	batch, _ = src.Poll(context.Background())
	if len(batch) != 0 {
		t.Error("exhausted")
	}
}
func TestMockSink(t *testing.T) {
	sk := NewMockSink("out")
	sk.Write([]Record{{Key: "x"}})
	if sk.Count() != 1 {
		t.Error("sink count")
	}
}
