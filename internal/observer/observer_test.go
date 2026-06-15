package observer

import ("testing";"time")

func TestTracerStartEnd(t *testing.T) {
	tr := NewTracer()
	span := tr.StartSpan("trace-1", "", "test-op", SpanInternal)
	if span.Name != "test-op" { t.Error("name") }
	tr.EndSpan(span.ID, "ok")
	if tr.SpanCount() != 1 { t.Error("count") }
	trace := tr.GetTrace("trace-1")
	if len(trace) != 1 { t.Error("trace size") }
}
func TestTracerNested(t *testing.T) {
	tr := NewTracer()
	root := tr.StartSpan("t2", "", "root", SpanServer)
	child := tr.StartSpan("t2", root.ID, "child", SpanInternal)
	tr.EndSpan(child.ID, "ok")
	tr.EndSpan(root.ID, "ok")
	trace := tr.GetTrace("t2")
	if len(trace) != 2 { t.Error("nested") }
}
func TestTracerEvents(t *testing.T) {
	tr := NewTracer()
	span := tr.StartSpan("t3", "", "op", SpanInternal)
	tr.AddEvent(span.ID, "step1", map[string]string{"key": "val"})
	tr.SetAttribute(span.ID, "user", "admin")
	tr.EndSpan(span.ID, "ok")
	found := tr.GetTrace("t3")
	if len(found[0].Events) != 1 { t.Error("events") }
	if found[0].Attributes["user"] != "admin" { t.Error("attrs") }
}
func TestLogCorrelator(t *testing.T) {
	lc := NewLogCorrelator()
	lc.Log("trace-x", "span-1", "info", "hello")
	logs := lc.GetTraceLogs("trace-x")
	if len(logs) != 1 { t.Error("log count") }
	if logs[0].Message != "hello" { t.Error("log msg") }
}
func TestSampleCollector(t *testing.T) {
	sc := NewSampleCollector()
	sc.Record("latency", 1.0)
	sc.Record("latency", 2.0)
	sc.Record("latency", 3.0)
	min, max, mean, count := sc.Stats("latency")
	if count != 3 { t.Error("count") }
	if min != 1.0 { t.Error("min") }
	if max != 3.0 { t.Error("max") }
	if mean != 2.0 { t.Error("mean") }
}
func TestSpanKindString(t *testing.T) {
	if SpanServer.String() != "server" { t.Error("kind") }
}
