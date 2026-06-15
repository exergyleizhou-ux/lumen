// Package export provides data export pipelines for agent outputs.
// It supports JSON, CSV, columnar (Parquet-like), and streaming NDJSON formats,
// with chunked writing and a composable pipeline architecture.
package export

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Format identifies the output serialization format.
type Format int

const (
	FormatJSON    Format = iota // application/json
	FormatCSV                   // text/csv
	FormatColumnar              // columnar (Parquet-like, stored as compressed columns)
	FormatNDJSON                // application/x-ndjson (newline-delimited JSON)
)

var formatNames = map[Format]string{
	FormatJSON:     "json",
	FormatCSV:      "csv",
	FormatColumnar: "columnar",
	FormatNDJSON:   "ndjson",
}

func (f Format) String() string {
	if s, ok := formatNames[f]; ok {
		return s
	}
	return "unknown"
}

// FormatFromString returns the Format for a name (case-insensitive).
func FormatFromString(s string) (Format, error) {
	lower := strings.ToLower(s)
	for k, v := range formatNames {
		if v == lower {
			return k, nil
		}
	}
	return -1, fmt.Errorf("export: unknown format %q", s)
}

// ---------------------------------------------------------------------------
// ChunkedWriter — writes output in size-bounded chunks, naming them
// base-0001.ext, base-0002.ext, etc.  Supports optional gzip compression.

// ChunkedWriter splits output into multiple files when a byte limit is
// exceeded.  Each chunk is a complete, self-contained document (for JSON and
// NDJSON) or a continuation (for CSV).
type ChunkedWriter struct {
	dir        string
	base       string
	ext        string
	chunkSize  int64
	compress   bool

	mu       sync.Mutex
	seq      int
	current  *os.File
	written  int64
	buf      *bufio.Writer
	gz       *gzip.Writer
	closer   io.WriteCloser // the chain to close on rotate
}

// NewChunkedWriter creates a ChunkedWriter.  dir is the output directory;
// base is the filename stem; chunkSize is the byte threshold before rotating.
func NewChunkedWriter(dir, base string, chunkSize int64) *ChunkedWriter {
	return &ChunkedWriter{
		dir:       dir,
		base:      base,
		chunkSize: chunkSize,
	}
}

// SetExtension overrides the default extension derived from format.
func (cw *ChunkedWriter) SetExtension(ext string) { cw.ext = ext }

// SetCompress enables gzip compression on each chunk.
func (cw *ChunkedWriter) SetCompress(v bool) { cw.compress = v }

// Write implements io.Writer.  It automatically rotates to a new file when
// the current one exceeds chunkSize.
func (cw *ChunkedWriter) Write(p []byte) (int, error) {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if cw.current == nil || (cw.chunkSize > 0 && cw.written+int64(len(p)) > cw.chunkSize) {
		if err := cw.rotate(); err != nil {
			return 0, err
		}
	}
	var n int
	var err error
	if cw.buf != nil {
		n, err = cw.buf.Write(p)
	} else {
		n, err = cw.current.Write(p)
	}
	cw.written += int64(n)
	return n, err
}

// Close flushes and closes the current chunk.
func (cw *ChunkedWriter) Close() error {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	return cw.closeCurrent()
}

func (cw *ChunkedWriter) rotate() error {
	if err := cw.closeCurrent(); err != nil {
		return err
	}
	cw.seq++
	name := fmt.Sprintf("%s-%04d%s", cw.base, cw.seq, cw.ext)
	path := filepath.Join(cw.dir, name)
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("export: chunk create: %w", err)
	}
	cw.current = f
	cw.written = 0

	var w io.WriteCloser = f
	if cw.compress {
		cw.gz = gzip.NewWriter(f)
		w = cw.gz
	}
	cw.buf = bufio.NewWriterSize(w, 64*1024)
	cw.closer = w
	return nil
}

func (cw *ChunkedWriter) closeCurrent() error {
	if cw.buf != nil {
		if err := cw.buf.Flush(); err != nil {
			return err
		}
		cw.buf = nil
	}
	if cw.gz != nil {
		if err := cw.gz.Close(); err != nil {
			return err
		}
		cw.gz = nil
	}
	if cw.current != nil {
		if err := cw.current.Close(); err != nil {
			return err
		}
		cw.current = nil
	}
	cw.closer = nil
	return nil
}

// ---------------------------------------------------------------------------
// Column schema & columnar writer

// ColumnType describes the data type of a column.
type ColumnType int

const (
	ColString ColumnType = iota
	ColInt
	ColFloat
	ColBool
	ColTime
)

func (ct ColumnType) String() string {
	switch ct {
	case ColString:
		return "string"
	case ColInt:
		return "int"
	case ColFloat:
		return "float"
	case ColBool:
		return "bool"
	case ColTime:
		return "time"
	}
	return "unknown"
}

// ColumnSchema defines one column in a columnar dataset.
type ColumnSchema struct {
	Name string
	Type ColumnType
}

// ColumnarWriter writes data in a simple columnar format: one file per column
// containing the raw values serialised sequentially, plus a schema.json
// manifest.  This mimics the Parquet idea without requiring a C library.
type ColumnarWriter struct {
	dir     string
	base    string
	schema  []ColumnSchema
	files   []*os.File
	buffers []*bufio.Writer
	rows    int64
	mu      sync.Mutex
}

// NewColumnarWriter creates a ColumnarWriter.  Schema must not be empty.
func NewColumnarWriter(dir, base string, schema []ColumnSchema) (*ColumnarWriter, error) {
	if len(schema) == 0 {
		return nil, fmt.Errorf("export: columnar requires at least one column")
	}
	cw := &ColumnarWriter{dir: dir, base: base, schema: schema}
	for i, col := range schema {
		path := filepath.Join(dir, fmt.Sprintf("%s-%s.col", base, col.Name))
		f, err := os.Create(path)
		if err != nil {
			cw.closeAll()
			return nil, fmt.Errorf("export: column %d (%s): %w", i, col.Name, err)
		}
		cw.files = append(cw.files, f)
		cw.buffers = append(cw.buffers, bufio.NewWriterSize(f, 64*1024))
	}
	return cw, nil
}

// WriteRow encodes one row (must match schema length and types).
func (cw *ColumnarWriter) WriteRow(row []interface{}) error {
	if len(row) != len(cw.schema) {
		return fmt.Errorf("export: columnar row length %d != schema %d", len(row), len(cw.schema))
	}
	cw.mu.Lock()
	defer cw.mu.Unlock()
	for i, val := range row {
		var enc []byte
		switch cw.schema[i].Type {
		case ColString:
			s, _ := val.(string)
			enc = []byte(s)
		case ColInt:
			var v int64
			switch x := val.(type) {
			case int:
				v = int64(x)
			case int64:
				v = x
			case float64:
				v = int64(x)
			default:
				v, _ = strconv.ParseInt(fmt.Sprint(x), 10, 64)
			}
			enc = strconv.AppendInt(nil, v, 10)
		case ColFloat:
			var v float64
			switch x := val.(type) {
			case float64:
				v = x
			case float32:
				v = float64(x)
			default:
				v, _ = strconv.ParseFloat(fmt.Sprint(x), 64)
			}
			enc = strconv.AppendFloat(nil, v, 'g', -1, 64)
		case ColBool:
			var v bool
			switch x := val.(type) {
			case bool:
				v = x
			default:
				v, _ = strconv.ParseBool(fmt.Sprint(x))
			}
			if v {
				enc = []byte("1")
			} else {
				enc = []byte("0")
			}
		case ColTime:
			switch x := val.(type) {
			case time.Time:
				enc = []byte(x.Format(time.RFC3339Nano))
			case string:
				enc = []byte(x)
			default:
				enc = []byte(fmt.Sprint(x))
			}
		}
		if _, err := cw.buffers[i].Write(enc); err != nil {
			return err
		}
		if _, err := cw.buffers[i].Write([]byte{'\n'}); err != nil {
			return err
		}
	}
	cw.rows++
	return nil
}

// Close flushes all buffers, writes the schema manifest, and closes files.
func (cw *ColumnarWriter) Close() error {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	return cw.closeAll()
}

func (cw *ColumnarWriter) closeAll() error {
	for _, b := range cw.buffers {
		if b != nil {
			_ = b.Flush()
		}
	}
	for _, f := range cw.files {
		if f != nil {
			_ = f.Close()
		}
	}
	cw.buffers = nil
	cw.files = nil
	// Write schema manifest.
	manifest := map[string]interface{}{
		"base":   cw.base,
		"rows":   cw.rows,
		"schema": cw.schema,
	}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	mpath := filepath.Join(cw.dir, cw.base+"-schema.json")
	return os.WriteFile(mpath, data, 0o644)
}

// ---------------------------------------------------------------------------
// Export pipeline — stages are functions that can be composed.

// Row is a generic map representing one record.
type Row map[string]interface{}

// Stage is a function that receives rows from input, transforms them, and
// sends results to output.  Close output when done.
type Stage func(input <-chan Row, output chan<- Row)

// ExportPipeline chains multiple stages and drains them into a sink.
type ExportPipeline struct {
	stages []Stage
	source <-chan Row
}

// NewPipeline creates a pipeline from a source channel.
func NewPipeline(source <-chan Row) *ExportPipeline {
	return &ExportPipeline{source: source}
}

// AddStage appends a transformation stage.
func (ep *ExportPipeline) AddStage(s Stage) *ExportPipeline {
	ep.stages = append(ep.stages, s)
	return ep
}

// Run executes the pipeline and returns the final output channel.
func (ep *ExportPipeline) Run() <-chan Row {
	current := ep.source
	for _, stage := range ep.stages {
		next := make(chan Row, 64)
		go stage(current, next)
		current = next
	}
	return current
}

// Collect reads all rows from the pipeline into a slice.
func (ep *ExportPipeline) Collect() ([]Row, error) {
	out := ep.Run()
	var rows []Row
	for r := range out {
		rows = append(rows, r)
	}
	return rows, nil
}

// ---------------------------------------------------------------------------
// Filter / transform stages (useful built-ins)

// FilterStage returns a Stage that only passes rows where pred returns true.
func FilterStage(pred func(Row) bool) Stage {
	return func(in <-chan Row, out chan<- Row) {
		defer close(out)
		for r := range in {
			if pred(r) {
				out <- r
			}
		}
	}
}

// MapStage returns a Stage that applies fn to every row.
func MapStage(fn func(Row) Row) Stage {
	return func(in <-chan Row, out chan<- Row) {
		defer close(out)
		for r := range in {
			out <- fn(r)
		}
	}
}

// LimitStage caps the number of rows passed through.
func LimitStage(n int) Stage {
	return func(in <-chan Row, out chan<- Row) {
		defer close(out)
		count := 0
		for r := range in {
			if count >= n {
				return
			}
			out <- r
			count++
		}
	}
}

// SortStage collects all rows, sorts them by key, and re-emits.  Use with
// care — this buffers everything.
func SortStage(key string, desc bool) Stage {
	return func(in <-chan Row, out chan<- Row) {
		defer close(out)
		var rows []Row
		for r := range in {
			rows = append(rows, r)
		}
		sort.Slice(rows, func(i, j int) bool {
			a, _ := rows[i][key]
			b, _ := rows[j][key]
			sa := fmt.Sprint(a)
			sb := fmt.Sprint(b)
			if desc {
				return sa > sb
			}
			return sa < sb
		})
		for _, r := range rows {
			out <- r
		}
	}
}

// ---------------------------------------------------------------------------
// FormatReport — writes a formatted report from rows to a writer.

// FormatReport writes rows to w in the requested format.  It returns the
// number of bytes written.
func FormatReport(w io.Writer, format Format, rows []Row) (int64, error) {
	switch format {
	case FormatJSON:
		return writeJSON(w, rows)
	case FormatCSV:
		return writeCSV(w, rows)
	case FormatNDJSON:
		return writeNDJSON(w, rows)
	case FormatColumnar:
		return 0, fmt.Errorf("export: columnar format requires a directory, use ColumnarWriter")
	}
	return 0, fmt.Errorf("export: unknown format %d", format)
}

func writeJSON(w io.Writer, rows []Row) (int64, error) {
	if rows == nil {
		rows = []Row{}
	}
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return 0, err
	}
	data = append(data, '\n')
	n, err := w.Write(data)
	return int64(n), err
}

func writeCSV(w io.Writer, rows []Row) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	cw := csv.NewWriter(w)
	// Derive header from first row.
	header := rowKeys(rows[0])
	if err := cw.Write(header); err != nil {
		return 0, err
	}
	for _, r := range rows {
		rec := make([]string, len(header))
		for i, k := range header {
			rec[i] = formatCell(r[k])
		}
		if err := cw.Write(rec); err != nil {
			return 0, err
		}
	}
	cw.Flush()
	return 0, cw.Error()
}

func writeNDJSON(w io.Writer, rows []Row) (int64, error) {
	var total int64
	enc := json.NewEncoder(w)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return total, err
		}
		// Encode adds a newline; we track bytes approximately.
		data, _ := json.Marshal(r)
		total += int64(len(data)) + 1
	}
	return total, nil
}

func rowKeys(r Row) []string {
	keys := make([]string, 0, len(r))
	for k := range r {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func formatCell(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case time.Time:
		return x.Format(time.RFC3339)
	default:
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Map {
			b, _ := json.Marshal(v)
			return string(b)
		}
		return fmt.Sprint(v)
	}
}

// ---------------------------------------------------------------------------
// Streaming NDJSON encoder — low-allocation, line-at-a-time.

// NDJSONEncoder writes one JSON line per row to an io.Writer.
type NDJSONEncoder struct {
	w   io.Writer
	enc *json.Encoder
}

// NewNDJSONEncoder creates an NDJSONEncoder.
func NewNDJSONEncoder(w io.Writer) *NDJSONEncoder {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &NDJSONEncoder{w: w, enc: enc}
}

// Encode writes one row as a JSON line.
func (e *NDJSONEncoder) Encode(row Row) error {
	return e.enc.Encode(row)
}

// ---------------------------------------------------------------------------
// Auto-detect CSV from a reader.

// CSVImporter reads CSV into []Row.  firstRowHeader determines whether the
// first line is treated as column names.
func CSVImporter(r io.Reader, firstRowHeader bool) ([]Row, error) {
	cr := csv.NewReader(r)
	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("export: csv read: %w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	var header []string
	start := 0
	if firstRowHeader {
		header = records[0]
		start = 1
	} else {
		for i := range records[0] {
			header = append(header, fmt.Sprintf("col_%d", i))
		}
	}
	var rows []Row
	for i := start; i < len(records); i++ {
		row := Row{}
		for j, v := range records[i] {
			if j < len(header) {
				row[header[j]] = v
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// ---------------------------------------------------------------------------
// JSONLines reader (streaming NDJSON input).

// NDJSONDecoder reads newline-delimited JSON line by line.
type NDJSONDecoder struct {
	scanner *bufio.Scanner
}

// NewNDJSONDecoder creates a decoder from a reader.
func NewNDJSONDecoder(r io.Reader) *NDJSONDecoder {
	return &NDJSONDecoder{scanner: bufio.NewScanner(r)}
}

// Decode reads the next line into row.  Returns io.EOF at end.
func (d *NDJSONDecoder) Decode(row *Row) error {
	for d.scanner.Scan() {
		line := bytes.TrimSpace(d.scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		return json.Unmarshal(line, row)
	}
	if err := d.scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}

// ---------------------------------------------------------------------------
// Size helpers

const (
	KB = 1024
	MB = 1024 * KB
	GB = 1024 * MB
)

// FormatBytes returns a human-readable byte count.
func FormatBytes(n int64) string {
	switch {
	case n >= GB:
		return fmt.Sprintf("%.2f GB", float64(n)/float64(GB))
	case n >= MB:
		return fmt.Sprintf("%.2f MB", float64(n)/float64(MB))
	case n >= KB:
		return fmt.Sprintf("%.2f KB", float64(n)/float64(KB))
	}
	return fmt.Sprintf("%d B", n)
}

// CountWriter wraps an io.Writer and counts bytes written.
type CountWriter struct {
	w     io.Writer
	count int64
}

// NewCountWriter creates a CountWriter.
func NewCountWriter(w io.Writer) *CountWriter { return &CountWriter{w: w} }

func (cw *CountWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.count += int64(n)
	return n, err
}

// Count returns the number of bytes written so far.
func (cw *CountWriter) Count() int64 { return cw.count }

// ---------------------------------------------------------------------------
// Batch helper — split rows into groups of n.

// BatchRows splits rows into batches of at most n.
func BatchRows(rows []Row, n int) [][]Row {
	if n <= 0 {
		return [][]Row{rows}
	}
	var batches [][]Row
	for i := 0; i < len(rows); i += n {
		end := i + n
		if end > len(rows) {
			end = len(rows)
		}
		batches = append(batches, rows[i:end])
	}
	return batches
}

// ---------------------------------------------------------------------------
// MergeRows combines multiple slices of rows, optionally deduplicating by key.
func MergeRows(key string, sources ...[]Row) []Row {
	seen := map[string]bool{}
	var merged []Row
	for _, src := range sources {
		for _, r := range src {
			if key != "" {
				k := fmt.Sprint(r[key])
				if seen[k] {
					continue
				}
				seen[k] = true
			}
			merged = append(merged, r)
		}
	}
	return merged
}

// ---------------------------------------------------------------------------
// Typed column helpers (used by ColumnarWriter consumers).

// InferSchema examines a set of rows and returns a likely ColumnSchema.
func InferSchema(rows []Row) []ColumnSchema {
	if len(rows) == 0 {
		return nil
	}
	keys := rowKeys(rows[0])
	schema := make([]ColumnSchema, 0, len(keys))
	for _, k := range keys {
		ct := ColString
		for _, r := range rows {
			v := r[k]
			if v == nil {
				continue
			}
			switch v.(type) {
			case bool:
				ct = ColBool
			case int, int8, int16, int32, int64:
				if ct == ColString || ct == ColFloat {
					ct = ColInt
				}
			case float32, float64:
				if ct == ColString {
					ct = ColFloat
				}
			case time.Time:
				ct = ColTime
			}
		}
		schema = append(schema, ColumnSchema{Name: k, Type: ct})
	}
	return schema
}

// ---------------------------------------------------------------------------
// File-format helpers.

// WriteFileFormat writes rows to a file, automatically picking the format
// from the extension (.json, .csv, .ndjson).
func WriteFileFormat(path string, rows []Row) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	ext := strings.ToLower(filepath.Ext(path))
	var format Format
	switch ext {
	case ".json":
		format = FormatJSON
	case ".csv":
		format = FormatCSV
	case ".ndjson", ".jsonl":
		format = FormatNDJSON
	default:
		return fmt.Errorf("export: unrecognised extension %q", ext)
	}
	_, err = FormatReport(f, format, rows)
	return err
}

// ReadFileFormat reads rows from a file, guessing the format from extension.
func ReadFileFormat(path string) ([]Row, error) {
	ext := strings.ToLower(filepath.Ext(path))
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	switch ext {
	case ".json":
		var rows []Row
		if err := json.NewDecoder(f).Decode(&rows); err != nil {
			return nil, fmt.Errorf("export: json decode: %w", err)
		}
		return rows, nil
	case ".csv":
		return CSVImporter(f, true)
	case ".ndjson", ".jsonl":
		dec := NewNDJSONDecoder(f)
		var rows []Row
		for {
			var row Row
			if err := dec.Decode(&row); err != nil {
				break
			}
			rows = append(rows, row)
		}
		return rows, nil
	}
	return nil, fmt.Errorf("export: unrecognised extension %q", ext)
}
