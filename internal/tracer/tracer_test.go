package tracer

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// TraceID / SpanID tests
// ---------------------------------------------------------------------------

func TestTraceID_String(t *testing.T) {
	id := TraceID{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	s := id.String()
	if len(s) != 32 {
		t.Fatalf("expected 32 hex chars, got %d: %s", len(s), s)
	}
	if s != "000102030405060708090a0b0c0d0e0f" {
		t.Fatalf("unexpected hex: %s", s)
	}
}

func TestTraceID_IsZero(t *testing.T) {
	var zero TraceID
	if !zero.IsZero() {
		t.Fatal("expected zero")
	}
	nonZero := generateTraceID()
	if nonZero.IsZero() {
		t.Fatal("expected non-zero")
	}
}

func TestSpanID_String(t *testing.T) {
	id := SpanID{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00, 0x11}
	s := id.String()
	if s != "aabbccddeeff0011" {
		t.Fatalf("unexpected hex: %s", s)
	}
}

func TestParseTraceID(t *testing.T) {
	id, err := ParseTraceID("abcdef1234567890abcdef1234567890")
	if err != nil {
		t.Fatal(err)
	}
	if id.String() != "abcdef1234567890abcdef1234567890" {
		t.Fatalf("roundtrip failed: %s", id)
	}

	_, err = ParseTraceID("too-short")
	if err == nil {
		t.Fatal("expected error for short string")
	}

	_, err = ParseTraceID("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	if err == nil {
		t.Fatal("expected error for non-hex")
	}
}

func TestParseSpanID(t *testing.T) {
	id, err := ParseSpanID("aabbccddeeff0011")
	if err != nil {
		t.Fatal(err)
	}
	if id.String() != "aabbccddeeff0011" {
		t.Fatalf("roundtrip failed: %s", id)
	}

	_, err = ParseSpanID("short")
	if err == nil {
		t.Fatal("expected error for short")
	}
}

// ---------------------------------------------------------------------------
// TraceFlags tests
// ---------------------------------------------------------------------------

func TestTraceFlags(t *testing.T) {
	f := TraceFlags(0)
	if f.IsSampled() {
		t.Fatal("expected not sampled")
	}

	f = f.WithSampled(true)
	if !f.IsSampled() {
		t.Fatal("expected sampled")
	}

	if f.String() != "01" {
		t.Fatalf("expected '01', got %q", f.String())
	}

	f = f.WithSampled(false)
	if f.IsSampled() {
		t.Fatal("expected not sampled after clear")
	}
}

func TestParseTraceFlags(t *testing.T) {
	f, err := ParseTraceFlags("01")
	if err != nil {
		t.Fatal(err)
	}
	if !f.IsSampled() {
		t.Fatal("expected sampled")
	}

	f, err = ParseTraceFlags("ff")
	if err != nil {
		t.Fatal(err)
	}
	if !f.IsSampled() {
		t.Fatal("expected sampled when bit 0 set")
	}

	_, err = ParseTraceFlags("xyz")
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// SpanContext tests
// ---------------------------------------------------------------------------

func TestSpanContext_Clone(t *testing.T) {
	sc := NewSpanContext(SpanContextConfig{
		TraceFlags: TraceFlags(0).WithSampled(true),
	})
	sc.Baggage["key"] = "value"

	clone := sc.Clone()
	if clone.TraceID != sc.TraceID {
		t.Fatal("trace id mismatch")
	}
	if clone.Baggage["key"] != "value" {
		t.Fatal("baggage not cloned")
	}

	// Mutate clone, verify original unchanged.
	clone.Baggage["key"] = "changed"
	if sc.Baggage["key"] != "value" {
		t.Fatal("original baggage mutated")
	}
}

func TestSpanContext_WithBaggageItem(t *testing.T) {
	sc := NewSpanContext(SpanContextConfig{})
	sc2 := sc.WithBaggageItem("foo", "bar")
	if sc.BaggageItem("foo") != "" {
		t.Fatal("original should not have baggage")
	}
	if sc2.BaggageItem("foo") != "bar" {
		t.Fatal("new context should have baggage")
	}
}

func TestSpanContext_WithSpanID(t *testing.T) {
	sc := NewSpanContext(SpanContextConfig{})
	newID := generateSpanID()
	sc2 := sc.WithSpanID(newID)
	if sc2.SpanID != newID {
		t.Fatal("span id not updated")
	}
}

// ---------------------------------------------------------------------------
// Span basics
// ---------------------------------------------------------------------------

func TestSpan_StartAndFinish(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{
		ServiceName: "test",
		Reporter:    rep,
	})

	span := tracer.StartSpan("test-op")
	if span.IsFinished() {
		t.Fatal("span should not be finished yet")
	}
	if span.Name() != "test-op" {
		t.Fatalf("unexpected name: %s", span.Name())
	}

	span.Finish()
	if !span.IsFinished() {
		t.Fatal("span should be finished")
	}
	if span.Duration() <= 0 {
		t.Fatal("duration should be > 0")
	}

	if rep.Count() != 1 {
		t.Fatalf("expected 1 reported span, got %d", rep.Count())
	}
}

func TestSpan_DoubleFinish(t *testing.T) {
	tracer := NewTracer(DefaultConfig())
	span := tracer.StartSpan("op")
	span.Finish()
	t1 := span.Duration()
	time.Sleep(1 * time.Millisecond)
	span.Finish() // Should be a no-op.
	t2 := span.Duration()
	if t1 != t2 {
		t.Fatal("double finish changed duration")
	}
}

func TestSpan_SetTag(t *testing.T) {
	tracer := NewTracer(DefaultConfig())
	span := tracer.StartSpan("op")
	span.SetTag("http.method", "GET")
	span.SetTag("http.url", "/api/v1")
	span.SetTag("http.method", "POST") // Overwrite.

	if span.Tag("http.method") != "POST" {
		t.Fatalf("expected POST, got %s", span.Tag("http.method"))
	}
	if span.Tag("http.url") != "/api/v1" {
		t.Fatalf("unexpected url tag")
	}
	if span.Tag("nonexistent") != "" {
		t.Fatal("expected empty for missing tag")
	}

	tags := span.Tags()
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
}

func TestSpan_LogKV(t *testing.T) {
	tracer := NewTracer(DefaultConfig())
	span := tracer.StartSpan("op")
	span.LogKV("event", "start", "count", "5")
	span.LogKV("event", "end")

	logs := span.Logs()
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(logs))
	}
	if logs[0].Fields["event"] != "start" {
		t.Fatalf("unexpected field: %v", logs[0].Fields)
	}
	if logs[0].Fields["count"] != "5" {
		t.Fatalf("unexpected count")
	}
	if logs[1].Fields["event"] != "end" {
		t.Fatalf("unexpected second log")
	}
}

func TestSpan_LogFields(t *testing.T) {
	tracer := NewTracer(DefaultConfig())
	span := tracer.StartSpan("op")
	span.LogFields(map[string]string{"key": "value"})
	logs := span.Logs()
	if len(logs) != 1 {
		t.Fatal("expected 1 log")
	}
	if logs[0].Fields["key"] != "value" {
		t.Fatal("unexpected field")
	}
}

func TestSpan_SetStatus(t *testing.T) {
	tracer := NewTracer(DefaultConfig())
	span := tracer.StartSpan("op")
	span.SetStatus(true, "all good")
	snap := span.Snapshot()
	if snap.Status != "OK" {
		t.Fatalf("expected OK, got %s", snap.Status)
	}

	span2 := tracer.StartSpan("err-op")
	span2.SetStatus(false, "something broke")
	snap2 := span2.Snapshot()
	if snap2.Status != "ERROR" {
		t.Fatalf("expected ERROR, got %s", snap2.Status)
	}
	if snap2.ErrorMsg != "something broke" {
		t.Fatalf("unexpected error msg: %s", snap2.ErrorMsg)
	}
}

func TestSpan_SetError(t *testing.T) {
	tracer := NewTracer(DefaultConfig())
	span := tracer.StartSpan("op")
	span.SetError(fmt.Errorf("test error"))
	snap := span.Snapshot()
	if snap.Status != "ERROR" {
		t.Fatalf("expected ERROR, got %s", snap.Status)
	}
}

func TestSpan_FinishWithError(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{Reporter: rep})
	span := tracer.StartSpan("op")
	span.FinishWithError(fmt.Errorf("fail"))
	if !span.IsFinished() {
		t.Fatal("expected finished")
	}
	snap := span.Snapshot()
	if snap.Status != "ERROR" {
		t.Fatalf("expected ERROR status")
	}
}

func TestSpan_IsSampled(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{
		SampleRate: 1.0,
		Reporter:   rep,
	})
	span := tracer.StartSpan("op")
	if !span.IsSampled() {
		t.Fatal("span should be sampled at 100%")
	}

	// Test with zero sample rate (still sampled because rate=0 means
	// shouldSample will always return false after the counter wraps).
	tracer2 := NewTracer(TracerConfig{SampleRate: 0.0, Reporter: rep})
	span2 := tracer2.StartSpan("op")
	// With rate 0, no sampled flag is set.
	if span2.IsSampled() {
		t.Fatal("span should not be sampled at 0%")
	}
}

// ---------------------------------------------------------------------------
// Span parent/child relationships
// ---------------------------------------------------------------------------

func TestSpan_ParentChild(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{Reporter: rep})

	parent := tracer.StartSpan("parent")
	child := tracer.StartSpan("child", WithParent(parent))

	if child.ParentID() != parent.SpanID() {
		t.Fatal("child parent id mismatch")
	}
	if child.TraceID() != parent.TraceID() {
		t.Fatal("trace id should be inherited")
	}
}

func TestSpan_BaggageInherited(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{Reporter: rep})

	parent := tracer.StartSpan("parent")
	parent.SetBaggageItem("user-id", "42")

	child := tracer.StartSpan("child", WithParent(parent))
	if child.BaggageItem("user-id") != "42" {
		t.Fatal("baggage not inherited")
	}
}

func TestSpan_SpanContextParent(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{Reporter: rep})

	sc := NewSpanContext(SpanContextConfig{
		TraceID:    generateTraceID(),
		SpanID:     generateSpanID(),
		TraceFlags: TraceFlags(0).WithSampled(true),
	})
	sc.Baggage["region"] = "us-east-1"

	span := tracer.StartSpan("remote-child", WithSpanContext(sc))
	if span.ParentID() != sc.SpanID {
		t.Fatal("parent id mismatch")
	}
	if span.BaggageItem("region") != "us-east-1" {
		t.Fatal("baggage not inherited from context")
	}
}

// ---------------------------------------------------------------------------
// SpanKind tests
// ---------------------------------------------------------------------------

func TestSpanKind_String(t *testing.T) {
	if KindServer.String() != "server" {
		t.Fatalf("unexpected: %s", KindServer)
	}
	if KindClient.String() != "client" {
		t.Fatalf("unexpected: %s", KindClient)
	}
	if KindInternal.String() != "internal" {
		t.Fatalf("unexpected: %s", KindInternal)
	}
	if SpanKind(99).String() != "SpanKind(99)" {
		t.Fatalf("unexpected unknown kind")
	}
}

func TestSpan_WithKind(t *testing.T) {
	tracer := NewTracer(DefaultConfig())
	span := tracer.StartSpan("op", WithKind(KindServer))
	if span.Kind() != KindServer {
		t.Fatalf("expected server kind, got %s", span.Kind())
	}
}

// ---------------------------------------------------------------------------
// Span links
// ---------------------------------------------------------------------------

func TestSpan_Links(t *testing.T) {
	tracer := NewTracer(DefaultConfig())
	sc := NewSpanContext(SpanContextConfig{})

	link := NewLink(sc, map[string]string{"rel": "follows-from"})
	span := tracer.StartSpan("op", WithLinks(link))
	links := span.Links()
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].Attributes["rel"] != "follows-from" {
		t.Fatalf("unexpected link attribute")
	}

	// Add another link.
	span.AddLink(NewLink(sc, map[string]string{"rel": "child-of"}))
	if len(span.Links()) != 2 {
		t.Fatalf("expected 2 links")
	}
}

// ---------------------------------------------------------------------------
// Span options
// ---------------------------------------------------------------------------

func TestSpanOptions(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{Reporter: rep})

	now := time.Now()
	span := tracer.StartSpan("op",
		WithKind(KindClient),
		WithTag("env", "prod"),
		WithStartTime(now),
	)
	if span.Kind() != KindClient {
		t.Fatal("kind not set")
	}
	if span.Tag("env") != "prod" {
		t.Fatal("tag not set")
	}
	if !span.StartTime().Equal(now) {
		t.Fatal("start time not set")
	}
}

// ---------------------------------------------------------------------------
// TextMapPropagator tests
// ---------------------------------------------------------------------------

func TestTextMapPropagator_InjectExtract(t *testing.T) {
	prop := DefaultPropagator()
	carrier := NewMapCarrier()

	sc := NewSpanContext(SpanContextConfig{
		TraceID:    generateTraceID(),
		SpanID:     generateSpanID(),
		TraceFlags: TraceFlags(0).WithSampled(true),
		TraceState: "vendor=test",
	})
	sc.Baggage["user"] = "alice"

	prop.Inject(sc, carrier)

	// Verify traceparent header.
	tp := carrier.Get("traceparent")
	if tp == "" {
		t.Fatal("traceparent not set")
	}
	parts := strings.Split(tp, "-")
	if len(parts) != 4 || parts[0] != "00" {
		t.Fatalf("invalid traceparent: %s", tp)
	}

	// Extract and verify.
	extracted := prop.Extract(carrier)
	if extracted.TraceID != sc.TraceID {
		t.Fatal("trace id mismatch")
	}
	if extracted.SpanID != sc.SpanID {
		t.Fatal("span id mismatch")
	}
	if !extracted.TraceFlags.IsSampled() {
		t.Fatal("sampled flag not preserved")
	}
	if extracted.TraceState != "vendor=test" {
		t.Fatalf("tracestate mismatch: %s", extracted.TraceState)
	}
	if extracted.Baggage["user"] != "alice" {
		t.Fatal("baggage not preserved")
	}
	if !extracted.Remote {
		t.Fatal("extracted context should be remote")
	}
}

func TestTextMapPropagator_ExtractInvalid(t *testing.T) {
	prop := DefaultPropagator()
	carrier := NewMapCarrier()

	// No traceparent.
	sc := prop.Extract(carrier)
	if !sc.TraceID.IsZero() {
		t.Fatal("expected zero trace id when no header")
	}

	// Malformed traceparent.
	carrier.Set("traceparent", "bad-value")
	sc = prop.Extract(carrier)
	if !sc.TraceID.IsZero() {
		t.Fatal("expected zero trace id for malformed header")
	}

	// Wrong version.
	carrier.Set("traceparent", "ff-00000000000000000000000000000000-0000000000000000-00")
	sc = prop.Extract(carrier)
	if !sc.TraceID.IsZero() {
		t.Fatal("expected zero for unsupported version")
	}
}

func TestTextMapPropagator_CustomHeaders(t *testing.T) {
	prop := TextMapPropagator{
		TraceParentHeader: "x-trace-parent",
		TraceStateHeader:  "x-trace-state",
		BaggageHeader:     "x-baggage",
	}
	carrier := NewMapCarrier()

	sc := NewSpanContext(SpanContextConfig{
		TraceFlags: TraceFlags(0).WithSampled(true),
	})
	sc.Baggage["key"] = "val"
	prop.Inject(sc, carrier)

	if carrier.Get("x-trace-parent") == "" {
		t.Fatal("custom header not set")
	}
	if carrier.Get("x-baggage") == "" {
		t.Fatal("custom baggage header not set")
	}

	extracted := prop.Extract(carrier)
	if extracted.Baggage["key"] != "val" {
		t.Fatal("baggage not extracted with custom header")
	}
}

func TestTextMapPropagator_BaggageRoundtrip(t *testing.T) {
	prop := DefaultPropagator()
	carrier := NewMapCarrier()

	sc := NewSpanContext(SpanContextConfig{})
	sc.Baggage["user-name"] = "Alice Johnson"
	sc.Baggage["session-id"] = "abc-123"

	prop.Inject(sc, carrier)
	extracted := prop.Extract(carrier)

	if extracted.Baggage["user-name"] != "Alice Johnson" {
		t.Fatalf("baggage key 'user-name' mismatch: got %q", extracted.Baggage["user-name"])
	}
	if extracted.Baggage["session-id"] != "abc-123" {
		t.Fatalf("baggage key 'session-id' mismatch")
	}
}

// ---------------------------------------------------------------------------
// FormatTraceTree tests
// ---------------------------------------------------------------------------

func TestFormatTraceTree_Empty(t *testing.T) {
	result := FormatTraceTree(nil, DefaultFormatTraceTreeOptions())
	if result != "(empty trace)\n" {
		t.Fatalf("unexpected: %q", result)
	}
}

func TestFormatTraceTree_SingleSpan(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{Reporter: rep})

	span := tracer.StartSpan("root")
	span.Finish()

	tree := FormatTraceTree([]*Span{span}, DefaultFormatTraceTreeOptions())
	if !strings.Contains(tree, "root") {
		t.Fatalf("tree should contain span name: %s", tree)
	}
}

func TestFormatTraceTree_ParentChild(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{Reporter: rep})

	root := tracer.StartSpan("root")
	child := tracer.StartSpan("child", WithParent(root))
	grandchild := tracer.StartSpan("grandchild", WithParent(child))
	child.Finish()
	grandchild.Finish()
	root.Finish()

	spans := []*Span{root, child, grandchild}
	tree := FormatTraceTree(spans, DefaultFormatTraceTreeOptions())

	if !strings.Contains(tree, "root") {
		t.Fatal("missing root")
	}
	if !strings.Contains(tree, "child") {
		t.Fatal("missing child")
	}
	if !strings.Contains(tree, "grandchild") {
		t.Fatal("missing grandchild")
	}
}

func TestFormatTraceTree_WithOptions(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{Reporter: rep})

	root := tracer.StartSpan("root", WithTag("env", "prod"))
	root.Finish()

	opts := DefaultFormatTraceTreeOptions()
	opts.ShowTags = true
	opts.ShowTiming = true

	tree := FormatTraceTree([]*Span{root}, opts)
	if !strings.Contains(tree, "env=prod") {
		t.Fatalf("tree should contain tags: %s", tree)
	}
}

// ---------------------------------------------------------------------------
// TracerProvider tests
// ---------------------------------------------------------------------------

func TestTracerProvider(t *testing.T) {
	rep := NewInMemoryReporter()
	provider := NewTracerProvider(TracerConfig{Reporter: rep})

	t1 := provider.Tracer("service-a")
	t2 := provider.Tracer("service-b")
	t3 := provider.Tracer("service-a") // Should return same instance.

	if t1 == t2 {
		t.Fatal("different names should return different tracers")
	}
	if t1 != t3 {
		t.Fatal("same name should return same tracer")
	}
}

// ---------------------------------------------------------------------------
// CompositeSpanReporter tests
// ---------------------------------------------------------------------------

func TestCompositeSpanReporter(t *testing.T) {
	r1 := NewInMemoryReporter()
	r2 := NewInMemoryReporter()
	composite := NewCompositeSpanReporter(r1, r2)

	tracer := NewTracer(TracerConfig{Reporter: composite})
	span := tracer.StartSpan("op")
	span.Finish()

	if r1.Count() != 1 || r2.Count() != 1 {
		t.Fatalf("both reporters should have received the span: r1=%d r2=%d",
			r1.Count(), r2.Count())
	}

	// Add a third reporter after creation.
	r3 := NewInMemoryReporter()
	composite.AddReporter(r3)

	span2 := tracer.StartSpan("op2")
	span2.Finish()

	if r3.Count() != 1 {
		t.Fatal("third reporter should receive spans after being added")
	}
}

// ---------------------------------------------------------------------------
// SpanBuilder tests
// ---------------------------------------------------------------------------

func TestSpanBuilder(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{Reporter: rep})

	parent := tracer.StartSpan("parent")
	span := NewSpanBuilder(tracer, "child").
		WithParentSpan(parent).
		WithKind(KindServer).
		WithTag("key", "value").
		Start()

	if span.ParentID() != parent.SpanID() {
		t.Fatal("parent not set via builder")
	}
	if span.Kind() != KindServer {
		t.Fatal("kind not set via builder")
	}
	if span.Tag("key") != "value" {
		t.Fatal("tag not set via builder")
	}
}

// ---------------------------------------------------------------------------
// TraceContext tests
// ---------------------------------------------------------------------------

func TestTraceContext(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{Reporter: rep})

	parent := tracer.StartSpan("parent")
	tc := NewTraceContext(parent, tracer)

	child := tc.StartSpan("child")
	if child.TraceID() != parent.TraceID() {
		t.Fatal("trace context child should inherit trace id")
	}
	if child.ParentID() != parent.SpanID() {
		t.Fatal("trace context child should inherit parent id")
	}
}

// ---------------------------------------------------------------------------
// Snapshot tests
// ---------------------------------------------------------------------------

func TestSpanSnapshot(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{Reporter: rep, SampleRate: 1.0})

	span := tracer.StartSpan("snapshot-test",
		WithKind(KindClient),
		WithTag("component", "test"),
	)
	span.SetBaggageItem("user", "bob")
	span.LogKV("event", "start")
	span.Finish()

	snap := span.Snapshot()
	if snap.Name != "snapshot-test" {
		t.Fatalf("name mismatch: %s", snap.Name)
	}
	if snap.Kind != "client" {
		t.Fatalf("kind mismatch: %s", snap.Kind)
	}
	if snap.Tags["component"] != "test" {
		t.Fatal("tags mismatch")
	}
	if snap.Baggage["user"] != "bob" {
		t.Fatal("baggage mismatch")
	}
	if snap.Duration <= 0 {
		t.Fatal("duration not set")
	}
	if !snap.Sampled {
		t.Fatal("should be sampled")
	}
}

// ---------------------------------------------------------------------------
// Concurrency tests
// ---------------------------------------------------------------------------

func TestSpan_ConcurrentTags(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{Reporter: rep})

	span := tracer.StartSpan("concurrent")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			span.SetTag(fmt.Sprintf("key-%d", n), "value")
			span.LogKV("event", fmt.Sprintf("log-%d", n))
		}(i)
	}
	wg.Wait()
	span.Finish()

	// Should not panic and should have all tags.
	tags := span.Tags()
	if len(tags) < 50 {
		t.Fatalf("expected many tags, got %d", len(tags))
	}
}

func TestInMemoryReporter_Concurrent(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{Reporter: rep})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			span := tracer.StartSpan(fmt.Sprintf("op-%d", n))
			span.Finish()
		}(i)
	}
	wg.Wait()

	if rep.Count() != 100 {
		t.Fatalf("expected 100 spans, got %d", rep.Count())
	}
}

// ---------------------------------------------------------------------------
// Span setName
// ---------------------------------------------------------------------------

func TestSpan_SetName(t *testing.T) {
	tracer := NewTracer(DefaultConfig())
	span := tracer.StartSpan("initial")
	if span.Name() != "initial" {
		t.Fatal("initial name mismatch")
	}
	span.SetName("updated")
	if span.Name() != "updated" {
		t.Fatal("name not updated")
	}
}

// ---------------------------------------------------------------------------
// CollectSpanTree
// ---------------------------------------------------------------------------

func TestCollectSpanTree(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{Reporter: rep})

	root := tracer.StartSpan("root")
	child1 := tracer.StartSpan("child1", WithParent(root))
	child2 := tracer.StartSpan("child2", WithParent(root))
	grandchild := tracer.StartSpan("grandchild", WithParent(child1))

	all := CollectSpanTree(root)
	if len(all) != 4 {
		t.Fatalf("expected 4 spans, got %d", len(all))
	}

	// Verify all are present.
	names := make(map[string]bool)
	for _, s := range all {
		names[s.Name()] = true
	}
	for _, name := range []string{"root", "child1", "child2", "grandchild"} {
		if !names[name] {
			t.Fatalf("missing span: %s", name)
		}
	}

	// Cleanup.
	child1.Finish()
	child2.Finish()
	grandchild.Finish()
	root.Finish()
}

// ---------------------------------------------------------------------------
// Encode helpers
// ---------------------------------------------------------------------------

func TestEncodeTraceIDBase64(t *testing.T) {
	id := TraceID{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	enc := EncodeTraceIDBase64(id)
	if enc == "" {
		t.Fatal("expected non-empty encoding")
	}
}

func TestEncodeSpanIDBase64(t *testing.T) {
	id := SpanID{0, 1, 2, 3, 4, 5, 6, 7}
	enc := EncodeSpanIDBase64(id)
	if enc == "" {
		t.Fatal("expected non-empty encoding")
	}
}

// ---------------------------------------------------------------------------
// NoopReporter
// ---------------------------------------------------------------------------

func TestNoopReporter(t *testing.T) {
	var r NoopReporter
	r.ReportSpan(nil) // Should not panic.
}

// ---------------------------------------------------------------------------
// MapCarrier
// ---------------------------------------------------------------------------

func TestMapCarrier(t *testing.T) {
	c := NewMapCarrier()
	c.Set("key1", "val1")
	c.Set("key2", "val2")

	if c.Get("key1") != "val1" {
		t.Fatal("get mismatch")
	}
	if c.Get("key3") != "" {
		t.Fatal("expected empty")
	}

	keys := c.Keys()
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

// ---------------------------------------------------------------------------
// Edge case: LogKV with odd args
// ---------------------------------------------------------------------------

func TestSpan_LogKV_Odd(t *testing.T) {
	tracer := NewTracer(DefaultConfig())
	span := tracer.StartSpan("op")
	span.LogKV("only-key") // Odd count, should append empty value.
	logs := span.Logs()
	if len(logs) != 1 {
		t.Fatal("expected 1 log")
	}
	if logs[0].Fields["only-key"] != "" {
		t.Fatalf("expected empty value, got %q", logs[0].Fields["only-key"])
	}
}

// ---------------------------------------------------------------------------
// Span start time override
// ---------------------------------------------------------------------------

func TestSpan_StartTime(t *testing.T) {
	tracer := NewTracer(DefaultConfig())
	fixed := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	span := tracer.StartSpan("op", WithStartTime(fixed))
	if !span.StartTime().Equal(fixed) {
		t.Fatalf("expected %v, got %v", fixed, span.StartTime())
	}
}

// ---------------------------------------------------------------------------
// InMemoryReporter reset
// ---------------------------------------------------------------------------

func TestInMemoryReporter_Reset(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{Reporter: rep})

	tracer.StartSpan("op").Finish()
	if rep.Count() != 1 {
		t.Fatal("expected 1")
	}
	rep.Reset()
	if rep.Count() != 0 {
		t.Fatal("expected 0 after reset")
	}
}

// ---------------------------------------------------------------------------
// FormatTraceTree with MaxDepth
// ---------------------------------------------------------------------------

func TestFormatTraceTree_MaxDepth(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{Reporter: rep})

	root := tracer.StartSpan("root")
	child := tracer.StartSpan("child", WithParent(root))
	grandchild := tracer.StartSpan("grandchild", WithParent(child))
	child.Finish()
	grandchild.Finish()
	root.Finish()

	opts := DefaultFormatTraceTreeOptions()
	opts.MaxDepth = 1

	tree := FormatTraceTree([]*Span{root, child, grandchild}, opts)
	if !strings.Contains(tree, "root") {
		t.Fatal("should contain root")
	}
	if strings.Contains(tree, "grandchild") {
		t.Fatal("should not contain grandchild at depth 1")
	}
}

// ---------------------------------------------------------------------------
// SpanSetError with nil
// ---------------------------------------------------------------------------

func TestSpan_SetError_Nil(t *testing.T) {
	tracer := NewTracer(DefaultConfig())
	span := tracer.StartSpan("op")
	span.SetError(nil) // Should be a no-op.
	snap := span.Snapshot()
	if snap.Status != "UNSET" {
		t.Fatalf("expected UNSET, got %s", snap.Status)
	}
}

// ---------------------------------------------------------------------------
// Multiple root spans in FormatTraceTree
// ---------------------------------------------------------------------------

func TestFormatTraceTree_MultipleRoots(t *testing.T) {
	rep := NewInMemoryReporter()
	tracer := NewTracer(TracerConfig{Reporter: rep})

	root1 := tracer.StartSpan("root1")
	root2 := tracer.StartSpan("root2")
	root1.Finish()
	root2.Finish()

	tree := FormatTraceTree([]*Span{root1, root2}, DefaultFormatTraceTreeOptions())
	if !strings.Contains(tree, "root1") || !strings.Contains(tree, "root2") {
		t.Fatalf("should contain both roots: %s", tree)
	}
}
