// Package batch provides batch processing with backpressure control,
// chunking strategies, and progress tracking. Used for processing large
// agent output streams efficiently.
package batch

import ("context";"fmt";"strings";"sync";"sync/atomic";"time")

// Chunk is a batch of items.
type Chunk[T any] struct {
	Items    []T         `json:"items"`
	Index    int         `json:"index"`
	Size     int         `json:"size"`
}

// Processor transforms a batch of input items to output items.
type Processor[T, U any] interface {
	Process(ctx context.Context, chunk Chunk[T]) (Chunk[U], error)
}

// Progress tracks batch progress.
type Progress struct {
	Total     int64         `json:"total"`
	Processed int64         `json:"processed"`
	Failed    int64         `json:"failed"`
	StartedAt time.Time     `json:"started_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	Done      bool          `json:"done"`
}

// Config configures batch processing.
type Config struct {
	ChunkSize    int           `json:"chunk_size"`
	MaxParallel  int           `json:"max_parallel"`
	MaxRetries   int           `json:"max_retries"`
	Backpressure int           `json:"backpressure"` // max inflight chunks
	Timeout      time.Duration `json:"timeout"`
}

// DefaultBatchConfig returns sensible defaults.
func DefaultBatchConfig() Config {
	return Config{ChunkSize: 100, MaxParallel: 4, MaxRetries: 3, Backpressure: 10, Timeout: 5 * time.Minute}
}

// Runner executes batch processing.
type Runner[T, U any] struct {
	cfg      Config
	mu       sync.Mutex
	progress *Progress
}

// NewRunner creates a batch runner.
func NewRunner[T, U any](cfg Config) *Runner[T, U] {
	return &Runner[T, U]{cfg: cfg, progress: &Progress{}}
}

// Run processes all items through the processor.
func (r *Runner[T, U]) Run(ctx context.Context, items []T, proc Processor[T, U]) ([]U, error) {
	r.progress = &Progress{Total: int64(len(items)), StartedAt: time.Now()}
	chunks := r.chunk(items)
	sem := make(chan struct{}, r.cfg.Backpressure)
	var out []U
	var outMu sync.Mutex
	var wg sync.WaitGroup
	var firstErr atomic.Value
	var failedChunks atomic.Int64

	for i, chunk := range chunks {
		select {
		case <-ctx.Done(): return out, ctx.Err()
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(idx int, c Chunk[T]) {
			defer wg.Done(); defer func() { <-sem }()

			var result Chunk[U]
			var err error
			for attempt := 0; attempt <= r.cfg.MaxRetries; attempt++ {
				if attempt > 0 { time.Sleep(time.Duration(attempt) * 100 * time.Millisecond) }
				result, err = proc.Process(ctx, c)
				if err == nil { break }
			}

			if err != nil {
				failedChunks.Add(1)
				firstErr.CompareAndSwap(nil, err)
				r.updateProgress(int64(len(c.Items)), true)
				return
			}
			outMu.Lock()
			out = append(out, result.Items...)
			outMu.Unlock()
			r.updateProgress(int64(len(c.Items)), false)
		}(i, chunk)
	}
	wg.Wait()

	r.mu.Lock()
	r.progress.Done = true
	r.progress.UpdatedAt = time.Now()
	r.mu.Unlock()

	var err error
	if v := firstErr.Load(); v != nil { err = v.(error) }
	return out, err
}

func (r *Runner[T, U]) chunk(items []T) []Chunk[T] {
	var chunks []Chunk[T]
	for i := 0; i < len(items); i += r.cfg.ChunkSize {
		end := i + r.cfg.ChunkSize
		if end > len(items) { end = len(items) }
		chunks = append(chunks, Chunk[T]{Items: items[i:end], Index: len(chunks), Size: end - i})
	}
	return chunks
}

func (r *Runner[T, U]) updateProgress(n int64, failed bool) {
	r.mu.Lock(); defer r.mu.Unlock()
	r.progress.Processed += n
	if failed { r.progress.Failed += n }
	r.progress.UpdatedAt = time.Now()
}

// Progress returns current progress.
func (r *Runner[T, U]) Progress() Progress {
	r.mu.Lock(); defer r.mu.Unlock()
	return *r.progress
}

// FormatProgress renders progress information.
func (r *Runner[T, U]) FormatProgress() string {
	p := r.Progress()
	var sb strings.Builder
	pct := 0.0
	if p.Total > 0 { pct = float64(p.Processed) / float64(p.Total) * 100 }
	bar := strings.Repeat("█", int(pct/5)) + strings.Repeat("░", 20-int(pct/5))
	fmt.Fprintf(&sb, "[%s] %.1f%% (%d/%d)", bar, pct, p.Processed, p.Total)
	if p.Failed > 0 { fmt.Fprintf(&sb, " %d failed", p.Failed) }
	if p.Done { sb.WriteString(" ✅") }
	return sb.String()
}

// ── Built-in Processors ───────────────────────────────────

// MapProcessor applies a function to each item.
type MapProcessor[T, U any] struct{ fn func(T) U }

func NewMapProcessor[T, U any](fn func(T) U) *MapProcessor[T, U] { return &MapProcessor[T, U]{fn: fn} }
func (p *MapProcessor[T, U]) Process(ctx context.Context, ch Chunk[T]) (Chunk[U], error) {
	out := make([]U, len(ch.Items))
	for i, item := range ch.Items { out[i] = p.fn(item) }
	return Chunk[U]{Items: out, Index: ch.Index, Size: len(out)}, nil
}

// FilterProcessor keeps only items matching a predicate.
type FilterProcessor[T any] struct{ fn func(T) bool }

func NewFilterProcessor[T any](fn func(T) bool) *FilterProcessor[T] { return &FilterProcessor[T]{fn: fn} }
func (p *FilterProcessor[T]) Process(ctx context.Context, ch Chunk[T]) (Chunk[T], error) {
	var out []T
	for _, item := range ch.Items { if p.fn(item) { out = append(out, item) } }
	return Chunk[T]{Items: out, Index: ch.Index, Size: len(out)}, nil
}
