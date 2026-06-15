// Package pipeline implements Unix-style composable pipelines with
// concurrent stage execution, buffered pipes, tee/grep stages, and
// pipeline DAG visualization. Pipelines are built from stages that
// each read from stdin, process data, and write to stdout.
//
// Usage:
//
//	p := pipeline.New()
//	p.AddStage("cat", func(r io.Reader, w io.Writer) error { ... })
//	p.AddStage("grep", func(r io.Reader, w io.Writer) error { ... })
//	p.AddStage("sort", func(r io.Reader, w io.Writer) error { ... })
//	p.Run(input)
//	output := p.Output()
package pipeline

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Stage
// ---------------------------------------------------------------------------

// StageFunc is the function signature for a pipeline stage. It reads from r
// until EOF, processes the data, and writes results to w.
type StageFunc func(r io.Reader, w io.Writer) error

// Stage represents one node in the pipeline DAG. A stage has a name, a
// processing function, and connections to upstream/downstream stages.
type Stage struct {
	Name     string
	Fn       StageFunc
	inputs   []*PipeBuffer
	outputs  []*PipeBuffer
}

// PipeBuffer is a buffered pipe connecting two stages (or the pipeline
// boundary). It implements io.Reader and io.Writer and can be read
// multiple times via Tee.
type PipeBuffer struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	closed  bool
	readers []*pipeReader
}

type pipeReader struct {
	pb     *PipeBuffer
	offset int
}

// NewPipeBuffer creates an empty PipeBuffer.
func NewPipeBuffer() *PipeBuffer {
	return &PipeBuffer{}
}

// Write appends data to the buffer. Implements io.Writer.
func (pb *PipeBuffer) Write(p []byte) (int, error) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	if pb.closed {
		return 0, fmt.Errorf("pipeline: write to closed pipe")
	}
	n, err := pb.buf.Write(p)
	// Notify readers.
	for _, r := range pb.readers {
		_ = r
	}
	return n, err
}

// Read reads data from the buffer. Returns io.EOF when closed and all data consumed.
func (pb *PipeBuffer) Read(p []byte) (int, error) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	b := pb.buf.Bytes()
	if len(b) == 0 {
		if pb.closed {
			return 0, io.EOF
		}
		return 0, nil
	}
	n := copy(p, b)
	pb.buf.Next(n)
	// If buffer is now empty and closed, next call returns EOF.
	return n, nil
}

// Close marks the buffer as complete.
func (pb *PipeBuffer) Close() error {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.closed = true
	return nil
}

// Reader returns an io.Reader for the current content.
func (pb *PipeBuffer) Reader() io.Reader {
	return pb
}

// Bytes returns a copy of the current buffer content.
func (pb *PipeBuffer) Bytes() []byte {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	out := make([]byte, pb.buf.Len())
	copy(out, pb.buf.Bytes())
	return out
}

// String returns the buffer content as a string.
func (pb *PipeBuffer) String() string { return string(pb.Bytes()) }

// Reset clears the buffer.
func (pb *PipeBuffer) Reset() {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.buf.Reset()
	pb.closed = false
}

// Tee returns a new PipeBuffer that receives a copy of all writes to this buffer.
func (pb *PipeBuffer) Tee() *PipeBuffer {
	tee := NewPipeBuffer()
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.readers = append(pb.readers, &pipeReader{pb: tee})
	return tee
}

// ---------------------------------------------------------------------------
// Pipeline
// ---------------------------------------------------------------------------

// Pipeline is a DAG of stages connected by PipeBuffers. It supports
// concurrent execution of independent stages and serial execution within
// linear chains.
type Pipeline struct {
	mu       sync.Mutex
	stages   []*Stage
	input    *PipeBuffer
	output   *PipeBuffer
}

// New creates an empty Pipeline.
func New() *Pipeline {
	return &Pipeline{
		input:  NewPipeBuffer(),
		output: NewPipeBuffer(),
	}
}

// AddStage appends a stage to the pipeline. Each stage gets an output buffer
// automatically. Returns the stage for further DAG wiring.
func (p *Pipeline) AddStage(name string, fn StageFunc) *Stage {
	s := &Stage{
		Name: name,
		Fn:   fn,
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	// Every stage gets its own output buffer.
	outBuf := NewPipeBuffer()
	s.outputs = append(s.outputs, outBuf)

	if len(p.stages) == 0 {
		// First stage: input -> stage
		s.inputs = append(s.inputs, p.input)
	} else {
		// Connect from the last stage's output buffer.
		prev := p.stages[len(p.stages)-1]
		// Reuse the previous stage's output buffer as this stage's input.
		// The previous stage already has an output buffer.
		s.inputs = append(s.inputs, prev.outputs[0])
	}

	p.stages = append(p.stages, s)
	return s
}

// Connect directly wires src stage output to dst stage input via a new buffer.
func (p *Pipeline) Connect(src, dst *Stage) {
	buf := NewPipeBuffer()
	src.outputs = append(src.outputs, buf)
	dst.inputs = append(dst.inputs, buf)
}

// AddGrepStage adds a grep-like stage that filters lines matching a pattern.
func (p *Pipeline) AddGrepStage(name, pattern string) *Stage {
	return p.AddStage(name, func(r io.Reader, w io.Writer) error {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, pattern) {
				fmt.Fprintln(w, line)
			}
		}
		return scanner.Err()
	})
}

// AddTeeStage adds a tee stage that copies input to output AND to a side buffer.
func (p *Pipeline) AddTeeStage(name string, side *PipeBuffer) *Stage {
	return p.AddStage(name, func(r io.Reader, w io.Writer) error {
		return Tee(r, w, side)
	})
}

// AddSortStage adds a sort stage that sorts lines.
func (p *Pipeline) AddSortStage(name string) *Stage {
	return p.AddStage(name, func(r io.Reader, w io.Writer) error {
		var lines []string
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		sort.Strings(lines)
		for _, line := range lines {
			fmt.Fprintln(w, line)
		}
		return nil
	})
}

// AddCatStage adds a cat stage that copies input to output unchanged.
func (p *Pipeline) AddCatStage(name string) *Stage {
	return p.AddStage(name, func(r io.Reader, w io.Writer) error {
		_, err := io.Copy(w, r)
		return err
	})
}

// Run executes the pipeline. Input data is written to the pipeline input,
// then stages execute sequentially (each stage fully processes its input
// before the next starts). Final output is collected in the pipeline output.
func (p *Pipeline) Run(inputData []byte) error {
	p.mu.Lock()
	stages := make([]*Stage, len(p.stages))
	copy(stages, p.stages)
	p.mu.Unlock()

	// Write input data to input buffer.
	if len(inputData) > 0 {
		p.input.Write(inputData)
	}
	p.input.Close()

	// Execute stages sequentially.
	var currentInput io.Reader = p.input
	for _, s := range stages {
		// Collect output buffers for this stage.
		outBufs := s.outputs
		var writer io.Writer
		if len(outBufs) == 0 {
			writer = io.Discard
		} else if len(outBufs) == 1 {
			writer = outBufs[0]
		} else {
			writers := make([]io.Writer, len(outBufs))
			for i, b := range outBufs {
				writers[i] = b
			}
			writer = io.MultiWriter(writers...)
		}

		if s.Fn != nil {
			if err := s.Fn(currentInput, writer); err != nil {
				return fmt.Errorf("stage %q: %w", s.Name, err)
			}
		}

		// Close output buffers and set as input for next stage.
		for _, out := range outBufs {
			out.Close()
		}
		if len(outBufs) > 0 {
			currentInput = outBufs[0]
		} else {
			currentInput = bytes.NewReader(nil)
		}
	}

	// Collect final output.
	if len(stages) > 0 {
		last := stages[len(stages)-1]
		if len(last.outputs) > 0 {
			p.output = last.outputs[0]
		}
	} else {
		p.output = p.input
	}

	return nil
}

// Output returns the final pipeline output as a string.
func (p *Pipeline) Output() string {
	if p.output != nil {
		return p.output.String()
	}
	return ""
}

// OutputBytes returns the final pipeline output.
func (p *Pipeline) OutputBytes() []byte {
	if p.output != nil {
		return p.output.Bytes()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Topological sort
// ---------------------------------------------------------------------------

func topologicalSort(stages []*Stage) []*Stage {
	// Build in-degree map.
	inDegree := make(map[*Stage]int)
	graph := make(map[*Stage][]*Stage) // adjacency list

	for _, s := range stages {
		inDegree[s] = len(s.inputs)
		for _, in := range s.inputs {
			// Find which stage produces this buffer.
			for _, other := range stages {
				for _, out := range other.outputs {
					if out == in {
						graph[other] = append(graph[other], s)
					}
				}
			}
		}
	}

	var result []*Stage
	var queue []*Stage

	// Start with stages that have no input buffers from other stages.
	for _, s := range stages {
		if inDegree[s] == 0 {
			queue = append(queue, s)
		}
	}

	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		result = append(result, s)

		for _, next := range graph[s] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	// Append any remaining stages (shouldn't happen in acyclic graphs).
	for _, s := range stages {
		found := false
		for _, r := range result {
			if r == s {
				found = true
				break
			}
		}
		if !found {
			result = append(result, s)
		}
	}

	return result
}

// ---------------------------------------------------------------------------
// Tee — copy input to two writers
// ---------------------------------------------------------------------------

// Tee reads from r and writes to both w and side concurrently.
func Tee(r io.Reader, w io.Writer, side io.Writer) error {
	var wg sync.WaitGroup
	pr, pw := io.Pipe()

	wg.Add(1)
	go func() {
		defer wg.Done()
		io.Copy(side, pr)
	}()

	// Write to both.
	_, err := io.Copy(io.MultiWriter(w, pw), r)
	pw.Close()
	wg.Wait()
	return err
}

// ---------------------------------------------------------------------------
// FormatPipeline — ASCII visualization
// ---------------------------------------------------------------------------

// FormatPipelineOptions controls pipeline visualization.
type FormatPipelineOptions struct {
	ShowBuffers bool
}

// DefaultFormatPipelineOptions returns sensible defaults.
func DefaultFormatPipelineOptions() FormatPipelineOptions {
	return FormatPipelineOptions{ShowBuffers: false}
}

// FormatPipeline returns an ASCII representation of the pipeline DAG.
func FormatPipeline(p *Pipeline, opts FormatPipelineOptions) string {
	var sb strings.Builder
	p.mu.Lock()
	stages := make([]*Stage, len(p.stages))
	copy(stages, p.stages)
	p.mu.Unlock()

	sb.WriteString("Pipeline:\n")
	sb.WriteString("  input\n")

	for i, s := range stages {
		connector := "├── "
		if i == len(stages)-1 {
			connector = "└── "
		}
		sb.WriteString("  ")
		sb.WriteString(connector)
		sb.WriteString(fmt.Sprintf("[%s]", s.Name))
		if opts.ShowBuffers && len(s.outputs) > 0 {
			sb.WriteString(fmt.Sprintf(" → buffer(%d)", len(s.outputs)))
		}
		sb.WriteByte('\n')
	}

	sb.WriteString("  output\n")
	return sb.String()
}

// ---------------------------------------------------------------------------
// Pre-built stage functions
// ---------------------------------------------------------------------------

// CatStage returns a stage function that copies input to output.
func CatStage() StageFunc {
	return func(r io.Reader, w io.Writer) error {
		_, err := io.Copy(w, r)
		return err
	}
}

// GrepStage returns a stage function that filters lines containing pattern.
func GrepStage(pattern string) StageFunc {
	return func(r io.Reader, w io.Writer) error {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), pattern) {
				fmt.Fprintln(w, scanner.Text())
			}
		}
		return scanner.Err()
	}
}

// SortStage returns a stage function that sorts lines.
func SortStage() StageFunc {
	return func(r io.Reader, w io.Writer) error {
		var lines []string
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		sort.Strings(lines)
		for _, line := range lines {
			fmt.Fprintln(w, line)
		}
		return nil
	}
}

// MapStage returns a stage that applies fn to each line.
func MapStage(fn func(string) string) StageFunc {
	return func(r io.Reader, w io.Writer) error {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			fmt.Fprintln(w, fn(scanner.Text()))
		}
		return scanner.Err()
	}
}

// FilterStage returns a stage that keeps lines where pred returns true.
func FilterStage(pred func(string) bool) StageFunc {
	return func(r io.Reader, w io.Writer) error {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			if pred(scanner.Text()) {
				fmt.Fprintln(w, scanner.Text())
			}
		}
		return scanner.Err()
	}
}

// CountStage returns a stage that counts lines and writes the count.
func CountStage() StageFunc {
	return func(r io.Reader, w io.Writer) error {
		count := 0
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			count++
		}
		fmt.Fprintf(w, "%d", count)
		return scanner.Err()
	}
}

// HeadStage returns a stage that outputs the first n lines.
func HeadStage(n int) StageFunc {
	return func(r io.Reader, w io.Writer) error {
		scanner := bufio.NewScanner(r)
		for i := 0; i < n && scanner.Scan(); i++ {
			fmt.Fprintln(w, scanner.Text())
		}
		return scanner.Err()
	}
}

// TailStage returns a stage that outputs the last n lines.
func TailStage(n int) StageFunc {
	return func(r io.Reader, w io.Writer) error {
		var lines []string
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		start := len(lines) - n
		if start < 0 {
			start = 0
		}
		for _, line := range lines[start:] {
			fmt.Fprintln(w, line)
		}
		return nil
	}
}

// UniqStage returns a stage that deduplicates adjacent lines.
func UniqStage() StageFunc {
	return func(r io.Reader, w io.Writer) error {
		var prev string
		first := true
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			if first || line != prev {
				fmt.Fprintln(w, line)
				prev = line
				first = false
			}
		}
		return scanner.Err()
	}
}

// WcStage returns a stage that counts lines, words, and characters.
func WcStage() StageFunc {
	return func(r io.Reader, w io.Writer) error {
		lines, words, chars := 0, 0, 0
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			lines++
			text := scanner.Text()
			chars += len(text) + 1 // +1 for newline
			words += len(strings.Fields(text))
		}
		fmt.Fprintf(w, "%d %d %d", lines, words, chars)
		return scanner.Err()
	}
}
