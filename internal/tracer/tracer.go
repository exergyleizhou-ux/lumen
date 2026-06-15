// Package tracer implements a distributed tracing library compatible with
// OpenTracing/W3C Trace Context propagation. It supports traceparent/tracestate
// headers, sampled spans, baggage items, span-to-span links, and span context
// propagation via text maps.
//
// Key concepts:
//
//   - Tracer: factory for spans, holds global configuration.
//   - Span: a named, timed operation with tags, logs, baggage, and links.
//   - SpanContext: immutable trace/span identity carried across process boundaries.
//   - TextMapPropagator: inject/extract span context from HTTP headers and other
//     key-value carriers, using the W3C traceparent and tracestate formats.
//   - FormatTraceTree: pretty-print an ASCII tree of spans for debugging.
//
// The implementation follows the W3C Trace Context Level 2 specification for
// traceparent (version, trace-id, parent-id, trace-flags) and tracestate
// (vendor-specific key-value pairs), while also supporting the older
// OpenTracing baggage format for compatibility.
package tracer

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Core types
// ---------------------------------------------------------------------------

// TraceID is a 16-byte (32 hex) globally unique trace identifier.
type TraceID [16]byte

// SpanID is an 8-byte (16 hex) span identifier, unique within a trace.
type SpanID [8]byte

// IsZero reports whether id is the zero value.
func (id TraceID) IsZero() bool { return id == TraceID{} }

// IsZero reports whether id is the zero value.
func (id SpanID) IsZero() bool { return id == SpanID{} }

// String returns the hex encoding of id.
func (id TraceID) String() string { return hex.EncodeToString(id[:]) }

// String returns the hex encoding of id.
func (id SpanID) String() string { return hex.EncodeToString(id[:]) }

// ParseTraceID parses a 32-character hex string into a TraceID.
func ParseTraceID(s string) (TraceID, error) {
	var id TraceID
	b, err := hex.DecodeString(s)
	if err != nil {
		return id, fmt.Errorf("tracer: invalid TraceID %q: %w", s, err)
	}
	if len(b) != 16 {
		return id, fmt.Errorf("tracer: TraceID must be 32 hex chars, got %d", len(s))
	}
	copy(id[:], b)
	return id, nil
}

// ParseSpanID parses a 16-character hex string into a SpanID.
func ParseSpanID(s string) (SpanID, error) {
	var id SpanID
	b, err := hex.DecodeString(s)
	if err != nil {
		return id, fmt.Errorf("tracer: invalid SpanID %q: %w", s, err)
	}
	if len(b) != 8 {
		return id, fmt.Errorf("tracer: SpanID must be 16 hex chars, got %d", len(s))
	}
	copy(id[:], b)
	return id, nil
}

// generateTraceID creates a random TraceID.
func generateTraceID() TraceID {
	var id TraceID
	_, _ = rand.Read(id[:])
	return id
}

// generateSpanID creates a random SpanID.
func generateSpanID() SpanID {
	var id SpanID
	_, _ = rand.Read(id[:])
	return id
}

// ---------------------------------------------------------------------------
// TraceFlags
// ---------------------------------------------------------------------------

// TraceFlags is a bitmask for W3C trace flags. Currently only bit 0 (sampled)
// is defined by the specification; bits 1-7 are reserved.
type TraceFlags uint8

const (
	// FlagSampled indicates the trace is sampled. When set, the trace data
	// SHOULD be recorded and exported.
	FlagSampled TraceFlags = 1 << 0
)

// IsSampled reports whether the sampled flag is set.
func (f TraceFlags) IsSampled() bool { return f&FlagSampled != 0 }

// WithSampled returns a copy of f with the sampled flag set or cleared.
func (f TraceFlags) WithSampled(v bool) TraceFlags {
	if v {
		return f | FlagSampled
	}
	return f &^ FlagSampled
}

// String returns the two-hex-digit trace-flags form required by traceparent.
func (f TraceFlags) String() string { return fmt.Sprintf("%02x", uint8(f)) }

// ParseTraceFlags parses a two-hex-digit trace-flags string.
func ParseTraceFlags(s string) (TraceFlags, error) {
	var v uint8
	_, err := fmt.Sscanf(s, "%02x", &v)
	if err != nil {
		return 0, fmt.Errorf("tracer: invalid trace-flags %q: %w", s, err)
	}
	return TraceFlags(v), nil
}

// ---------------------------------------------------------------------------
// SpanContext — immutable identity carried across process boundaries
// ---------------------------------------------------------------------------

// SpanContext holds the trace identity and per-span metadata that propagates
// across service boundaries. It is immutable once constructed.
type SpanContext struct {
	TraceID    TraceID
	SpanID     SpanID
	TraceFlags TraceFlags
	// TraceState is the W3C tracestate value: a comma-separated list of
	// vendor-specific key=value pairs (opaque to the propagation layer).
	TraceState string
	// Baggage holds key-value annotations that propagate across the whole trace.
	// Unlike tracestate, baggage is meant for business data, not sampling decisions.
	Baggage map[string]string
	// Remote indicates whether this context was extracted from a remote carrier.
	Remote bool
}

// SpanContextConfig is used to construct a SpanContext.
type SpanContextConfig struct {
	TraceID    TraceID
	SpanID     SpanID
	TraceFlags TraceFlags
	TraceState string
	Baggage    map[string]string
	Remote     bool
}

// NewSpanContext creates a SpanContext from the given config.
// Zero-value IDs are replaced with new random ones.
func NewSpanContext(cfg SpanContextConfig) SpanContext {
	sc := SpanContext{
		TraceID:    cfg.TraceID,
		SpanID:     cfg.SpanID,
		TraceFlags: cfg.TraceFlags,
		TraceState: cfg.TraceState,
		Remote:     cfg.Remote,
	}
	if sc.TraceID.IsZero() {
		sc.TraceID = generateTraceID()
	}
	if sc.SpanID.IsZero() {
		sc.SpanID = generateSpanID()
	}
	if cfg.Baggage != nil {
		sc.Baggage = make(map[string]string, len(cfg.Baggage))
		for k, v := range cfg.Baggage {
			sc.Baggage[k] = v
		}
	} else {
		sc.Baggage = make(map[string]string)
	}
	return sc
}

// Clone returns a deep copy of sc.
func (sc SpanContext) Clone() SpanContext {
	return NewSpanContext(SpanContextConfig{
		TraceID:    sc.TraceID,
		SpanID:     sc.SpanID,
		TraceFlags: sc.TraceFlags,
		TraceState: sc.TraceState,
		Baggage:    sc.Baggage,
		Remote:     sc.Remote,
	})
}

// WithSpanID returns a copy with a new SpanID.
func (sc SpanContext) WithSpanID(sid SpanID) SpanContext {
	out := sc.Clone()
	out.SpanID = sid
	return out
}

// WithTraceFlags returns a copy with new trace flags.
func (sc SpanContext) WithTraceFlags(f TraceFlags) SpanContext {
	out := sc.Clone()
	out.TraceFlags = f
	return out
}

// WithBaggageItem adds or updates a baggage item.
func (sc SpanContext) WithBaggageItem(key, value string) SpanContext {
	out := sc.Clone()
	out.Baggage[key] = value
	return out
}

// BaggageItem returns the value for key, or "".
func (sc SpanContext) BaggageItem(key string) string {
	return sc.Baggage[key]
}

// ---------------------------------------------------------------------------
// SpanLink — a reference to another span
// ---------------------------------------------------------------------------

// SpanLink represents a causal or informational link to another span.
// It carries the linked span's context and optional attributes describing
// the relationship.
type SpanLink struct {
	SpanContext SpanContext
	Attributes  map[string]string
}

// NewLink creates a SpanLink to the given SpanContext.
func NewLink(sc SpanContext, attrs map[string]string) SpanLink {
	link := SpanLink{SpanContext: sc.Clone()}
	if attrs != nil {
		link.Attributes = make(map[string]string, len(attrs))
		for k, v := range attrs {
			link.Attributes[k] = v
		}
	} else {
		link.Attributes = make(map[string]string)
	}
	return link
}

// ---------------------------------------------------------------------------
// SpanKind
// ---------------------------------------------------------------------------

// SpanKind describes the role of a span: server, client, producer, consumer,
// or internal (unspecified).
type SpanKind int

const (
	KindUnspecified SpanKind = iota
	KindServer
	KindClient
	KindProducer
	KindConsumer
	KindInternal
)

var kindNames = map[SpanKind]string{
	KindUnspecified: "unspecified",
	KindServer:      "server",
	KindClient:      "client",
	KindProducer:    "producer",
	KindConsumer:    "consumer",
	KindInternal:    "internal",
}

func (k SpanKind) String() string {
	if s, ok := kindNames[k]; ok {
		return s
	}
	return fmt.Sprintf("SpanKind(%d)", k)
}

// ---------------------------------------------------------------------------
// Span — the core unit of work
// ---------------------------------------------------------------------------

// spanStatus represents the final status of a span.
type spanStatus int

const (
	statusUnset spanStatus = iota
	statusOK
	statusError
)

// LogRecord is a timestamped key-value event recorded on a span.
type LogRecord struct {
	Timestamp time.Time
	Fields    map[string]string
}

// Span represents a single unit of work within a trace. It is created by a
// Tracer and must be finished via Span.Finish (or FinishWithError) to
// record its duration.
//
// Usage:
//
//	span := tracer.StartSpan("operation")
//	defer span.Finish()
//	span.SetTag("component", "cache")
//	sub := tracer.StartSpan("sub-op", WithParent(span))
//	sub.Finish()
type Span struct {
	mu sync.Mutex

	// Identity
	ctx        SpanContext
	parentSpan *Span
	parentID   SpanID
	name       string
	kind       SpanKind
	tracer     *Tracer

	// Timing
	startTime time.Time
	endTime   time.Time
	finished  bool

	// Attributes
	tags    map[string]string
	logs    []LogRecord
	links   []SpanLink
	status  spanStatus
	errMsg  string

	// Child tracking
	children []*Span
}

// SpanOption customizes span creation.
type SpanOption func(*Span)

// WithParent sets the parent span. The child inherits the parent's TraceID
// and its SpanID becomes the child's parentID.
func WithParent(parent *Span) SpanOption {
	return func(s *Span) {
		if parent != nil {
			s.parentSpan = parent
			s.ctx.TraceID = parent.ctx.TraceID
			s.parentID = parent.ctx.SpanID
			s.ctx.TraceFlags = parent.ctx.TraceFlags
			s.ctx.TraceState = parent.ctx.TraceState
			s.ctx.Baggage = make(map[string]string, len(parent.ctx.Baggage))
			for k, v := range parent.ctx.Baggage {
				s.ctx.Baggage[k] = v
			}
		}
	}
}

// WithSpanContext sets an explicit parent context (e.g. extracted from headers).
func WithSpanContext(ctx SpanContext) SpanOption {
	return func(s *Span) {
		s.ctx.TraceID = ctx.TraceID
		s.parentID = ctx.SpanID
		s.ctx.TraceFlags = ctx.TraceFlags
		s.ctx.TraceState = ctx.TraceState
		s.ctx.Baggage = make(map[string]string, len(ctx.Baggage))
		for k, v := range ctx.Baggage {
			s.ctx.Baggage[k] = v
		}
	}
}

// WithKind sets the span kind.
func WithKind(k SpanKind) SpanOption {
	return func(s *Span) { s.kind = k }
}

// WithTag adds an initial tag.
func WithTag(key, value string) SpanOption {
	return func(s *Span) { s.tags[key] = value }
}

// WithStartTime overrides the start time.
func WithStartTime(t time.Time) SpanOption {
	return func(s *Span) { s.startTime = t }
}

// WithLinks attaches SpanLinks at creation time.
func WithLinks(links ...SpanLink) SpanOption {
	return func(s *Span) { s.links = append(s.links, links...) }
}

// newSpan constructs a Span. Callers should use Tracer.StartSpan.
func newSpan(tracer *Tracer, name string, opts ...SpanOption) *Span {
	s := &Span{
		ctx:       NewSpanContext(SpanContextConfig{}),
		name:      name,
		kind:      KindInternal,
		tracer:    tracer,
		startTime: time.Now(),
		tags:      make(map[string]string),
		logs:      make([]LogRecord, 0),
		links:     make([]SpanLink, 0),
		children:  make([]*Span, 0),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// SpanContext returns the span's immutable context (can be propagated).
func (s *Span) SpanContext() SpanContext {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ctx.Clone()
}

// Name returns the span's operation name.
func (s *Span) Name() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.name
}

// SetName changes the operation name.
func (s *Span) SetName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.name = name
}

// Kind returns the span kind.
func (s *Span) Kind() SpanKind {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kind
}

// TraceID returns the trace id.
func (s *Span) TraceID() TraceID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ctx.TraceID
}

// SpanID returns this span's id.
func (s *Span) SpanID() SpanID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ctx.SpanID
}

// ParentID returns the parent span's id, or zero if this is a root.
func (s *Span) ParentID() SpanID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.parentID
}

// SetTag adds or updates a key-value tag on the span. Tags are meant for
// searchable, indexable metadata (e.g. "http.method", "db.instance").
func (s *Span) SetTag(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tags[key] = value
}

// Tag returns a tag value, or "".
func (s *Span) Tag(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tags[key]
}

// Tags returns a copy of all tags.
func (s *Span) Tags() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.tags))
	for k, v := range s.tags {
		out[k] = v
	}
	return out
}

// LogFields records a timestamped event with key-value fields.
func (s *Span) LogFields(fields map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fc := make(map[string]string, len(fields))
	for k, v := range fields {
		fc[k] = v
	}
	s.logs = append(s.logs, LogRecord{Timestamp: time.Now(), Fields: fc})
}

// LogKV records a flat key-value event (args must come in pairs).
func (s *Span) LogKV(kvs ...string) {
	if len(kvs)%2 != 0 {
		kvs = append(kvs, "")
	}
	fields := make(map[string]string, len(kvs)/2)
	for i := 0; i < len(kvs); i += 2 {
		fields[kvs[i]] = kvs[i+1]
	}
	s.LogFields(fields)
}

// Logs returns a copy of all log records.
func (s *Span) Logs() []LogRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]LogRecord, len(s.logs))
	for i, lr := range s.logs {
		out[i] = lr
		out[i].Fields = make(map[string]string, len(lr.Fields))
		for k, v := range lr.Fields {
			out[i].Fields[k] = v
		}
	}
	return out
}

// AddLink adds a causal or informational link to another span.
func (s *Span) AddLink(link SpanLink) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.links = append(s.links, link)
}

// Links returns a copy of all links.
func (s *Span) Links() []SpanLink {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SpanLink, len(s.links))
	copy(out, s.links)
	return out
}

// SetStatus records the span's final status.
func (s *Span) SetStatus(ok bool, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ok {
		s.status = statusOK
	} else {
		s.status = statusError
	}
	s.errMsg = msg
}

// SetError is a shorthand for SetStatus(false, err.Error()).
func (s *Span) SetError(err error) {
	if err == nil {
		return
	}
	s.SetStatus(false, err.Error())
}

// SetBaggageItem sets a baggage key-value on the span context.
func (s *Span) SetBaggageItem(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctx.Baggage[key] = value
}

// BaggageItem returns a baggage value.
func (s *Span) BaggageItem(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ctx.Baggage[key]
}

// Finish marks the span as complete. It records the end time and, if the
// tracer has a reporter, reports the finished span.
func (s *Span) Finish() {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return
	}
	s.finished = true
	s.endTime = time.Now()
	tracer := s.tracer
	s.mu.Unlock()

	if tracer != nil && tracer.reporter != nil {
		tracer.reporter.ReportSpan(s)
	}
}

// FinishWithError is a convenience that calls SetError then Finish.
func (s *Span) FinishWithError(err error) {
	s.SetError(err)
	s.Finish()
}

// IsFinished reports whether Finish has been called.
func (s *Span) IsFinished() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finished
}

// Duration returns the wall-clock duration. Returns 0 if not finished.
func (s *Span) Duration() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.finished {
		return 0
	}
	return s.endTime.Sub(s.startTime)
}

// StartTime returns when the span was started.
func (s *Span) StartTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startTime
}

// IsSampled returns whether the span's trace has the sampled flag set.
func (s *Span) IsSampled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ctx.TraceFlags.IsSampled()
}

// addChild records a child span (internal use).
func (s *Span) addChild(child *Span) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.children = append(s.children, child)
}

// Children returns direct child spans.
func (s *Span) Children() []*Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Span, len(s.children))
	copy(out, s.children)
	return out
}

// ---------------------------------------------------------------------------
// SpanReporter — interface for exporting finished spans
// ---------------------------------------------------------------------------

// SpanReporter receives finished spans for export to a collector or logger.
type SpanReporter interface {
	ReportSpan(span *Span)
}

// NoopReporter is a reporter that drops all spans.
type NoopReporter struct{}

// ReportSpan does nothing.
func (NoopReporter) ReportSpan(*Span) {}

// LogReporter prints spans to stdout in a simple format.
type LogReporter struct {
	mu sync.Mutex
}

// ReportSpan prints the span.
func (lr *LogReporter) ReportSpan(span *Span) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	fmt.Printf("[trace] %s/%s %s %s %v\n",
		span.TraceID(), span.SpanID(), span.Name(),
		span.Kind(), span.Duration())
}

// ---------------------------------------------------------------------------
// Tracer — the entry point for span creation
// ---------------------------------------------------------------------------

// TracerConfig holds configuration for a Tracer.
type TracerConfig struct {
	// ServiceName identifies this service in the trace ecosystem.
	ServiceName string
	// SampleRate is the fraction of traces to sample, between 0 and 1.
	SampleRate float64
	// Reporter receives finished spans; nil means spans are discarded.
	Reporter SpanReporter
	// IDGenerator allows custom ID generation; nil uses crypto/rand.
	IDGenerator func() (TraceID, SpanID)
}

// DefaultConfig returns a sane default configuration.
func DefaultConfig() TracerConfig {
	return TracerConfig{
		ServiceName: "unknown",
		SampleRate:  1.0,
		Reporter:    NoopReporter{},
	}
}

// Tracer is the entry point for creating spans. It holds configuration and a
// reporter; all span creation goes through it so that sampling, ID generation,
// and reporting are centralized.
type Tracer struct {
	cfg       TracerConfig
	reporter  SpanReporter
	idCounter uint64
}

// NewTracer creates a Tracer from the given config.
func NewTracer(cfg TracerConfig) *Tracer {
	rep := cfg.Reporter
	if rep == nil {
		rep = NoopReporter{}
	}
	return &Tracer{
		cfg:      cfg,
		reporter: rep,
	}
}

// StartSpan creates a new span as a child of any parent provided through opts.
// Use WithParent or WithSpanContext to attach the span to an existing trace.
func (t *Tracer) StartSpan(name string, opts ...SpanOption) *Span {
	s := newSpan(t, name, opts...)

	// Apply sampling decision
	if t.cfg.SampleRate < 1.0 {
		if !t.shouldSample() {
			s.ctx.TraceFlags = s.ctx.TraceFlags.WithSampled(false)
		} else {
			s.ctx.TraceFlags = s.ctx.TraceFlags.WithSampled(true)
		}
	} else {
		s.ctx.TraceFlags = s.ctx.TraceFlags.WithSampled(true)
	}

	// If a custom ID generator is configured, use it.
	if t.cfg.IDGenerator != nil {
		tid, sid := t.cfg.IDGenerator()
		if !tid.IsZero() {
			s.ctx.TraceID = tid
		}
		if !sid.IsZero() {
			s.ctx.SpanID = sid
		}
	}

	// Register with parent span if one was provided.
	if s.parentSpan != nil {
		s.parentSpan.addChild(s)
	}

	return s
}

// shouldSample returns true if the trace should be sampled based on the rate.
func (t *Tracer) shouldSample() bool {
	n := atomic.AddUint64(&t.idCounter, 1)
	return (float64(n%10000) / 10000.0) < t.cfg.SampleRate
}

// ServiceName returns the configured service name.
func (t *Tracer) ServiceName() string { return t.cfg.ServiceName }

// SampleRate returns the configured sample rate.
func (t *Tracer) SampleRate() float64 { return t.cfg.SampleRate }

// Reporter returns the span reporter.
func (t *Tracer) Reporter() SpanReporter { return t.reporter }

// ---------------------------------------------------------------------------
// TextMapPropagator — inject/extract span context from text maps
// ---------------------------------------------------------------------------

// TextMapCarrier is the interface that propagation adapters must satisfy.
// It is a simple key-value store that can be read and written.
// Typical implementations wrap http.Header, a map[string]string, or gRPC metadata.
type TextMapCarrier interface {
	Get(key string) string
	Set(key string, value string)
	Keys() []string
}

// TextMapPropagator injects and extracts SpanContext values from a
// TextMapCarrier (typically HTTP headers). It implements the W3C Trace Context
// propagation format:
//
//	traceparent: 00-{trace-id}-{parent-id}-{trace-flags}
//	tracestate:  {vendor-specific list}
//
// and optionally baggage in a separate header.
type TextMapPropagator struct {
	// TraceParentHeader is the header name for traceparent (default "traceparent").
	TraceParentHeader string
	// TraceStateHeader is the header name for tracestate (default "tracestate").
	TraceStateHeader string
	// BaggageHeader is the header name for baggage (default "baggage").
	BaggageHeader string
	// BaggagePrefix is used when extracting baggage from a prefixed header.
	BaggagePrefix string
}

// DefaultPropagator returns a TextMapPropagator with W3C-standard header names.
func DefaultPropagator() TextMapPropagator {
	return TextMapPropagator{
		TraceParentHeader: "traceparent",
		TraceStateHeader:  "tracestate",
		BaggageHeader:     "baggage",
		BaggagePrefix:     "",
	}
}

// Inject encodes a SpanContext into the carrier for outbound propagation.
// It sets the traceparent, tracestate, and baggage headers.
func (p TextMapPropagator) Inject(sc SpanContext, carrier TextMapCarrier) {
	tp := p.formatTraceParent(sc)
	carrier.Set(p.TraceParentHeader, tp)

	if sc.TraceState != "" {
		carrier.Set(p.TraceStateHeader, sc.TraceState)
	}

	if len(sc.Baggage) > 0 {
		carrier.Set(p.BaggageHeader, p.formatBaggage(sc.Baggage))
	}
}

// Extract reads a SpanContext from the carrier. On failure (missing or
// malformed traceparent) it returns an empty (invalid) SpanContext.
func (p TextMapPropagator) Extract(carrier TextMapCarrier) SpanContext {
	tp := carrier.Get(p.TraceParentHeader)
	if tp == "" {
		return SpanContext{}
	}

	traceID, spanID, flags, err := p.parseTraceParent(tp)
	if err != nil {
		return SpanContext{}
	}

	ts := carrier.Get(p.TraceStateHeader)
	baggage := p.parseBaggage(carrier.Get(p.BaggageHeader))

	return NewSpanContext(SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: flags,
		TraceState: ts,
		Baggage:    baggage,
		Remote:     true,
	})
}

// formatTraceParent builds a traceparent header value.
// Format: {version}-{trace-id}-{parent-id}-{trace-flags}
// version is always "00" for this implementation.
func (p TextMapPropagator) formatTraceParent(sc SpanContext) string {
	return fmt.Sprintf("00-%s-%s-%s", sc.TraceID, sc.SpanID, sc.TraceFlags)
}

// parseTraceParent parses a traceparent header value.
func (p TextMapPropagator) parseTraceParent(s string) (TraceID, SpanID, TraceFlags, error) {
	var traceID TraceID
	var spanID SpanID
	var flags TraceFlags

	parts := strings.Split(s, "-")
	if len(parts) != 4 {
		return traceID, spanID, flags, errors.New("tracer: traceparent must have 4 parts")
	}
	if parts[0] != "00" {
		return traceID, spanID, flags, fmt.Errorf("tracer: unsupported traceparent version %q", parts[0])
	}
	if len(parts[1]) != 32 {
		return traceID, spanID, flags, fmt.Errorf("tracer: trace-id must be 32 hex chars, got %d", len(parts[1]))
	}
	if len(parts[2]) != 16 {
		return traceID, spanID, flags, fmt.Errorf("tracer: parent-id must be 16 hex chars, got %d", len(parts[2]))
	}
	if len(parts[3]) != 2 {
		return traceID, spanID, flags, fmt.Errorf("tracer: trace-flags must be 2 hex chars, got %d", len(parts[3]))
	}

	var err error
	traceID, err = ParseTraceID(parts[1])
	if err != nil {
		return traceID, spanID, flags, err
	}
	spanID, err = ParseSpanID(parts[2])
	if err != nil {
		return traceID, spanID, flags, err
	}
	flags, err = ParseTraceFlags(parts[3])
	if err != nil {
		return traceID, spanID, flags, err
	}
	return traceID, spanID, flags, nil
}

// formatBaggage encodes baggage as a comma-separated key=value string.
// Keys and values are URL-encoded to handle special characters.
func (p TextMapPropagator) formatBaggage(baggage map[string]string) string {
	if len(baggage) == 0 {
		return ""
	}
	// Sort keys for determinism.
	keys := make([]string, 0, len(baggage))
	for k := range baggage {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	for i, k := range keys {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(urlEncode(k))
		buf.WriteByte('=')
		buf.WriteString(urlEncode(baggage[k]))
	}
	return buf.String()
}

// parseBaggage decodes a baggage header value into a map.
func (p TextMapPropagator) parseBaggage(s string) map[string]string {
	out := make(map[string]string)
	if s == "" {
		return out
	}
	// Split by comma, handling URL-encoded values.
	pairs := splitBaggage(s)
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := urlDecode(strings.TrimSpace(kv[0]))
		v := urlDecode(strings.TrimSpace(kv[1]))
		if k != "" {
			out[k] = v
		}
	}
	return out
}

// splitBaggage splits a baggage string by commas, respecting URL encoding.
func splitBaggage(s string) []string {
	var parts []string
	var current strings.Builder
	inEscape := false
	for _, ch := range s {
		if inEscape {
			current.WriteRune(ch)
			inEscape = false
			continue
		}
		if ch == '%' {
			// Simple heuristic: treat % as the start of an escape sequence.
			current.WriteRune(ch)
			inEscape = true
			continue
		}
		if ch == ',' {
			parts = append(parts, current.String())
			current.Reset()
			continue
		}
		current.WriteRune(ch)
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

// urlEncode performs simple percent-encoding for baggage keys and values.
func urlEncode(s string) string {
	var buf bytes.Buffer
	for _, b := range []byte(s) {
		if isBaggageSafe(b) {
			buf.WriteByte(b)
		} else {
			fmt.Fprintf(&buf, "%%%02X", b)
		}
	}
	return buf.String()
}

// urlDecode performs simple percent-decoding.
func urlDecode(s string) string {
	var buf bytes.Buffer
	i := 0
	for i < len(s) {
		if s[i] == '%' && i+2 < len(s) {
			var v byte
			fmt.Sscanf(s[i+1:i+3], "%02X", &v)
			buf.WriteByte(v)
			i += 3
		} else {
			buf.WriteByte(s[i])
			i++
		}
	}
	return buf.String()
}

// isBaggageSafe returns true for characters that don't need encoding in baggage.
func isBaggageSafe(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') || b == '-' || b == '.' || b == '_' || b == '~'
}

// ---------------------------------------------------------------------------
// MapCarrier — a simple in-memory TextMapCarrier
// ---------------------------------------------------------------------------

// MapCarrier implements TextMapCarrier on a plain map[string]string.
type MapCarrier struct {
	vals map[string]string
}

// NewMapCarrier creates a MapCarrier.
func NewMapCarrier() *MapCarrier {
	return &MapCarrier{vals: make(map[string]string)}
}

// Get returns the value for key.
func (c *MapCarrier) Get(key string) string { return c.vals[key] }

// Set stores the value for key.
func (c *MapCarrier) Set(key, value string) { c.vals[key] = value }

// Keys returns all stored keys.
func (c *MapCarrier) Keys() []string {
	keys := make([]string, 0, len(c.vals))
	for k := range c.vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---------------------------------------------------------------------------
// FormatTraceTree — ASCII tree representation of spans
// ---------------------------------------------------------------------------

// FormatTraceTreeOptions controls the output of FormatTraceTree.
type FormatTraceTreeOptions struct {
	// ShowTags includes span tags in the output.
	ShowTags bool
	// ShowTiming includes duration in the output.
	ShowTiming bool
	// ShowLinks includes span links in the output.
	ShowLinks bool
	// MaxDepth limits tree depth (0 = unlimited).
	MaxDepth int
}

// DefaultFormatTraceTreeOptions returns sensible defaults.
func DefaultFormatTraceTreeOptions() FormatTraceTreeOptions {
	return FormatTraceTreeOptions{
		ShowTags:   true,
		ShowTiming: true,
		ShowLinks:  false,
		MaxDepth:   0,
	}
}

// FormatTraceTree renders a span tree as an ASCII-formatted string.
// Root spans (those with zero parent ID) are used as tree roots.
func FormatTraceTree(spans []*Span, opts FormatTraceTreeOptions) string {
	if len(spans) == 0 {
		return "(empty trace)\n"
	}

	// Find roots: spans with no parent or parent that is not in the set.
	spanSet := make(map[SpanID]*Span, len(spans))
	for _, s := range spans {
		spanSet[s.SpanID()] = s
	}

	var roots []*Span
	for _, s := range spans {
		if s.ParentID().IsZero() || spanSet[s.ParentID()] == nil {
			roots = append(roots, s)
		}
	}

	// Build children index.
	children := make(map[SpanID][]*Span)
	for _, s := range spans {
		pid := s.ParentID()
		if !pid.IsZero() {
			children[pid] = append(children[pid], s)
		}
	}

	var buf bytes.Buffer
	for i, root := range roots {
		if i > 0 {
			buf.WriteByte('\n')
		}
		formatSpanTree(&buf, root, children, "", true, opts, 0)
	}
	return buf.String()
}

func formatSpanTree(
	buf *bytes.Buffer,
	span *Span,
	children map[SpanID][]*Span,
	prefix string,
	isLast bool,
	opts FormatTraceTreeOptions,
	depth int,
) {
	if opts.MaxDepth > 0 && depth >= opts.MaxDepth {
		return
	}

	// Choose connector.
	connector := "├── "
	if isLast {
		connector = "└── "
	}
	if depth == 0 {
		connector = ""
	}

	buf.WriteString(prefix)
	buf.WriteString(connector)
	buf.WriteString(span.Name())

	if opts.ShowTiming {
		dur := span.Duration()
		if dur > 0 {
			fmt.Fprintf(buf, " (%v)", dur)
		} else {
			buf.WriteString(" (running)")
		}
	}

	fmt.Fprintf(buf, " [%s]", span.SpanID().String()[:8])

	if opts.ShowTags && len(span.Tags()) > 0 {
		buf.WriteString(" {")
		tags := span.Tags()
		keys := make([]string, 0, len(tags))
		for k := range tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			if i > 0 {
				buf.WriteString(", ")
			}
			fmt.Fprintf(buf, "%s=%s", k, tags[k])
		}
		buf.WriteByte('}')
	}

	buf.WriteByte('\n')

	// Children
	childList := children[span.SpanID()]
	// Sort for determinism.
	sort.Slice(childList, func(i, j int) bool {
		return childList[i].StartTime().Before(childList[j].StartTime())
	})

	for i, child := range childList {
		childPrefix := prefix
		if depth > 0 {
			if isLast {
				childPrefix += "    "
			} else {
				childPrefix += "│   "
			}
		}
		lastChild := i == len(childList)-1
		formatSpanTree(buf, child, children, childPrefix, lastChild, opts, depth+1)
	}
}

// ---------------------------------------------------------------------------
// Helper: collect a span tree
// ---------------------------------------------------------------------------

// CollectSpanTree gathers a span and all its descendants into a flat slice.
func CollectSpanTree(root *Span) []*Span {
	var result []*Span
	collectSpan(root, &result)
	return result
}

func collectSpan(s *Span, out *[]*Span) {
	*out = append(*out, s)
	for _, child := range s.Children() {
		collectSpan(child, out)
	}
}

// ---------------------------------------------------------------------------
// TracerProvider — global tracer management (optional singleton pattern)
// ---------------------------------------------------------------------------

// TracerProvider manages named Tracer instances, similar to the OpenTelemetry
// TracerProvider concept. A single provider can vend multiple tracers (one per
// instrumentation library).
type TracerProvider struct {
	mu       sync.RWMutex
	tracers  map[string]*Tracer
	cfg      TracerConfig
	reporter SpanReporter
}

// NewTracerProvider creates a TracerProvider with the given config.
func NewTracerProvider(cfg TracerConfig) *TracerProvider {
	rep := cfg.Reporter
	if rep == nil {
		rep = NoopReporter{}
	}
	return &TracerProvider{
		tracers:  make(map[string]*Tracer),
		cfg:      cfg,
		reporter: rep,
	}
}

// Tracer returns a named Tracer, creating one if necessary.
func (tp *TracerProvider) Tracer(name string) *Tracer {
	tp.mu.RLock()
	t, ok := tp.tracers[name]
	tp.mu.RUnlock()
	if ok {
		return t
	}

	tp.mu.Lock()
	defer tp.mu.Unlock()
	// Double-check.
	if t, ok = tp.tracers[name]; ok {
		return t
	}
	cfg := tp.cfg
	cfg.Reporter = tp.reporter
	cfg.ServiceName = name
	t = NewTracer(cfg)
	tp.tracers[name] = t
	return t
}

// ---------------------------------------------------------------------------
// CompositeSpanReporter — fan-out to multiple reporters
// ---------------------------------------------------------------------------

// CompositeSpanReporter fans out span reports to multiple reporters.
type CompositeSpanReporter struct {
	mu        sync.RWMutex
	reporters []SpanReporter
}

// NewCompositeSpanReporter creates a fan-out reporter.
func NewCompositeSpanReporter(reporters ...SpanReporter) *CompositeSpanReporter {
	return &CompositeSpanReporter{reporters: append([]SpanReporter{}, reporters...)}
}

// AddReporter appends a reporter.
func (cr *CompositeSpanReporter) AddReporter(r SpanReporter) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	cr.reporters = append(cr.reporters, r)
}

// ReportSpan forwards the span to all registered reporters.
func (cr *CompositeSpanReporter) ReportSpan(span *Span) {
	cr.mu.RLock()
	reporters := make([]SpanReporter, len(cr.reporters))
	copy(reporters, cr.reporters)
	cr.mu.RUnlock()
	for _, r := range reporters {
		r.ReportSpan(span)
	}
}

// ---------------------------------------------------------------------------
// InMemoryReporter — collects spans for testing
// ---------------------------------------------------------------------------

// InMemoryReporter stores reported spans in memory for inspection.
type InMemoryReporter struct {
	mu    sync.Mutex
	spans []*Span
}

// NewInMemoryReporter creates an InMemoryReporter.
func NewInMemoryReporter() *InMemoryReporter {
	return &InMemoryReporter{spans: make([]*Span, 0)}
}

// ReportSpan records the span.
func (r *InMemoryReporter) ReportSpan(span *Span) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans = append(r.spans, span)
}

// Spans returns a copy of all recorded spans.
func (r *InMemoryReporter) Spans() []*Span {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Span, len(r.spans))
	copy(out, r.spans)
	return out
}

// Reset clears all recorded spans.
func (r *InMemoryReporter) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans = r.spans[:0]
}

// Count returns the number of recorded spans.
func (r *InMemoryReporter) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.spans)
}

// ---------------------------------------------------------------------------
// SpanBuilder — fluent API for constructing spans
// ---------------------------------------------------------------------------

// SpanBuilder provides a fluent interface for building and starting spans.
type SpanBuilder struct {
	tracer *Tracer
	name   string
	opts   []SpanOption
}

// NewSpanBuilder starts a builder for the given tracer and name.
func NewSpanBuilder(t *Tracer, name string) *SpanBuilder {
	return &SpanBuilder{tracer: t, name: name}
}

// WithParentSpan adds a parent span.
func (sb *SpanBuilder) WithParentSpan(parent *Span) *SpanBuilder {
	sb.opts = append(sb.opts, WithParent(parent))
	return sb
}

// WithParentContext adds a parent context.
func (sb *SpanBuilder) WithParentContext(ctx SpanContext) *SpanBuilder {
	sb.opts = append(sb.opts, WithSpanContext(ctx))
	return sb
}

// WithKind sets the span kind.
func (sb *SpanBuilder) WithKind(k SpanKind) *SpanBuilder {
	sb.opts = append(sb.opts, WithKind(k))
	return sb
}

// WithTag adds an initial tag.
func (sb *SpanBuilder) WithTag(key, value string) *SpanBuilder {
	sb.opts = append(sb.opts, WithTag(key, value))
	return sb
}

// WithLinks adds links.
func (sb *SpanBuilder) WithLinks(links ...SpanLink) *SpanBuilder {
	sb.opts = append(sb.opts, WithLinks(links...))
	return sb
}

// Start creates and starts the span.
func (sb *SpanBuilder) Start() *Span {
	return sb.tracer.StartSpan(sb.name, sb.opts...)
}

// ---------------------------------------------------------------------------
// TraceContext — convenience for managing trace in a context-like value
// ---------------------------------------------------------------------------

// TraceContext bundles a SpanContext with a Tracer so that callers can create
// child spans without passing both separately.
type TraceContext struct {
	SpanContext SpanContext
	Tracer      *Tracer
}

// NewTraceContext creates a TraceContext from a span.
func NewTraceContext(span *Span, tracer *Tracer) TraceContext {
	return TraceContext{
		SpanContext: span.SpanContext(),
		Tracer:      tracer,
	}
}

// StartSpan creates a child span within this trace context.
func (tc TraceContext) StartSpan(name string, opts ...SpanOption) *Span {
	allOpts := make([]SpanOption, 0, len(opts)+1)
	allOpts = append(allOpts, WithSpanContext(tc.SpanContext))
	allOpts = append(allOpts, opts...)
	return tc.Tracer.StartSpan(name, allOpts...)
}

// ---------------------------------------------------------------------------
// Encoding helpers for trace-id/span-id in base64 and other formats
// ---------------------------------------------------------------------------

// EncodeTraceIDBase64 returns the standard base64 encoding of the trace id.
func EncodeTraceIDBase64(id TraceID) string {
	// Simple base64 without padding.
	return base64Encode(id[:])
}

// EncodeSpanIDBase64 returns the standard base64 encoding of the span id.
func EncodeSpanIDBase64(id SpanID) string {
	return base64Encode(id[:])
}

func base64Encode(b []byte) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	var buf bytes.Buffer
	for i := 0; i < len(b); i += 3 {
		// Simple chunk-based encoding for fixed-size IDs
		remaining := len(b) - i
		if remaining >= 3 {
			v := uint32(b[i])<<16 | uint32(b[i+1])<<8 | uint32(b[i+2])
			buf.WriteByte(charset[(v>>18)&0x3F])
			buf.WriteByte(charset[(v>>12)&0x3F])
			buf.WriteByte(charset[(v>>6)&0x3F])
			buf.WriteByte(charset[v&0x3F])
		} else if remaining == 2 {
			v := uint32(b[i])<<16 | uint32(b[i+1])<<8
			buf.WriteByte(charset[(v>>18)&0x3F])
			buf.WriteByte(charset[(v>>12)&0x3F])
			buf.WriteByte(charset[(v>>6)&0x3F])
		} else if remaining == 1 {
			v := uint32(b[i]) << 16
			buf.WriteByte(charset[(v>>18)&0x3F])
			buf.WriteByte(charset[(v>>12)&0x3F])
		}
	}
	return buf.String()
}

// ---------------------------------------------------------------------------
// SpanSnapshot — an immutable, serializable copy of a finished span
// ---------------------------------------------------------------------------

// SpanSnapshot is an immutable copy of a span's data, safe for serialization.
type SpanSnapshot struct {
	Name       string
	TraceID    string
	SpanID     string
	ParentID   string
	Kind       string
	StartTime  time.Time
	EndTime    time.Time
	Duration   time.Duration
	Tags       map[string]string
	Logs       []LogRecord
	Links      []SpanLinkSnapshot
	Status     string
	ErrorMsg   string
	TraceState string
	Baggage    map[string]string
	Sampled    bool
}

// SpanLinkSnapshot is a serializable SpanLink.
type SpanLinkSnapshot struct {
	TraceID    string
	SpanID     string
	Attributes map[string]string
}

// Snapshot returns a SpanSnapshot of the span.
func (s *Span) Snapshot() SpanSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := SpanSnapshot{
		Name:       s.name,
		TraceID:    s.ctx.TraceID.String(),
		SpanID:     s.ctx.SpanID.String(),
		Kind:       s.kind.String(),
		StartTime:  s.startTime,
		EndTime:    s.endTime,
		Tags:       make(map[string]string, len(s.tags)),
		Logs:       make([]LogRecord, len(s.logs)),
		TraceState: s.ctx.TraceState,
		Baggage:    make(map[string]string, len(s.ctx.Baggage)),
		Sampled:    s.ctx.TraceFlags.IsSampled(),
	}
	if !s.parentID.IsZero() {
		snap.ParentID = s.parentID.String()
	}
	if s.finished {
		snap.Duration = s.endTime.Sub(s.startTime)
	}
	for k, v := range s.tags {
		snap.Tags[k] = v
	}
	for k, v := range s.ctx.Baggage {
		snap.Baggage[k] = v
	}
	for i, lr := range s.logs {
		snap.Logs[i] = LogRecord{
			Timestamp: lr.Timestamp,
			Fields:    make(map[string]string, len(lr.Fields)),
		}
		for k, v := range lr.Fields {
			snap.Logs[i].Fields[k] = v
		}
	}
	for _, link := range s.links {
		ls := SpanLinkSnapshot{
			TraceID:    link.SpanContext.TraceID.String(),
			SpanID:     link.SpanContext.SpanID.String(),
			Attributes: make(map[string]string, len(link.Attributes)),
		}
		for k, v := range link.Attributes {
			ls.Attributes[k] = v
		}
		snap.Links = append(snap.Links, ls)
	}
	switch s.status {
	case statusOK:
		snap.Status = "OK"
	case statusError:
		snap.Status = "ERROR"
		snap.ErrorMsg = s.errMsg
	default:
		snap.Status = "UNSET"
	}
	return snap
}
