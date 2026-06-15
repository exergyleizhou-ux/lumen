// Package logger provides structured logging with levels, fields, and
// multiple output targets (stdout, file, JSONL). It supports log level
// filtering, caller location, and colorized terminal output.
package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Level is a log severity level.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

func (l Level) String() string {
	switch l {
	case LevelDebug: return "DEBUG"
	case LevelInfo: return "INFO"
	case LevelWarn: return "WARN"
	case LevelError: return "ERROR"
	case LevelFatal: return "FATAL"
	default: return "???"
	}
}

func (l Level) Color() string {
	switch l {
	case LevelDebug: return "\x1b[90m"
	case LevelInfo: return "\x1b[36m"
	case LevelWarn: return "\x1b[33m"
	case LevelError: return "\x1b[31m"
	case LevelFatal: return "\x1b[35m"
	default: return ""
	}
}

// Entry is one structured log entry.
type Entry struct {
	Timestamp time.Time         `json:"timestamp"`
	Level     string            `json:"level"`
	Message   string            `json:"message"`
	Fields    map[string]any    `json:"fields,omitempty"`
	Caller    string            `json:"caller,omitempty"`
}

// Logger writes structured log entries.
type Logger struct {
	mu       sync.Mutex
	level    Level
	writers  []io.Writer
	fields   map[string]any
	format   string // "text" or "json"
	colorize bool
}

// New creates a logger that writes to stdout with text format.
func New(level Level) *Logger {
	return &Logger{
		level: level, writers: []io.Writer{os.Stderr},
		format: "text", fields: map[string]any{},
		colorize: true,
	}
}

// WithLevel sets the minimum log level.
func (l *Logger) WithLevel(level Level) *Logger { l.level = level; return l }

// WithWriter adds an output writer.
func (l *Logger) WithWriter(w io.Writer) *Logger { l.writers = append(l.writers, w); return l }

// WithFile adds a file output.
func (l *Logger) WithFile(path string) (*Logger, error) {
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil { return l, err }
	l.writers = append(l.writers, f)
	return l, nil
}

// WithField adds a field to all future log entries.
func (l *Logger) WithField(key string, value any) *Logger {
	l.fields[key] = value; return l
}

// WithJSON enables JSON output format.
func (l *Logger) WithJSON() *Logger { l.format = "json"; return l }

// Log writes a log entry at the given level.
func (l *Logger) Log(level Level, msg string, fields ...any) {
	if level < l.level { return }
	l.mu.Lock(); defer l.mu.Unlock()

	entry := Entry{
		Timestamp: time.Now(), Level: level.String(), Message: msg,
		Fields: map[string]any{}, Caller: caller(2),
	}
	for k, v := range l.fields { entry.Fields[k] = v }
	for i := 0; i+1 < len(fields); i += 2 {
		k := fmt.Sprint(fields[i])
		entry.Fields[k] = fields[i+1]
	}

	var line string
	if l.format == "json" {
		b, _ := json.Marshal(entry)
		line = string(b) + "\n"
	} else {
		line = l.formatText(entry)
	}

	for _, w := range l.writers {
		fmt.Fprint(w, line)
	}
}

func (l *Logger) formatText(e Entry) string {
	color := ""
	reset := ""
	if l.colorize {
		lv, _ := parseLevel(e.Level)
		color = lv.Color()
		reset = "\x1b[0m"
	}
	fields := ""
	if len(e.Fields) > 0 {
		var pairs []string
		for k, v := range e.Fields {
			pairs = append(pairs, fmt.Sprintf("%s=%v", k, v))
		}
		fields = " " + strings.Join(pairs, " ")
	}
	caller := ""
	if e.Caller != "" { caller = " " + e.Caller }
	return fmt.Sprintf("%s%s%s %-5s %s%s%s\n",
		color, e.Timestamp.Format("15:04:05.000"), reset,
		e.Level, e.Message, fields, caller)
}

// Shortcut methods
func (l *Logger) Debug(msg string, fields ...any) { l.Log(LevelDebug, msg, fields...) }
func (l *Logger) Info(msg string, fields ...any)  { l.Log(LevelInfo, msg, fields...) }
func (l *Logger) Warn(msg string, fields ...any)  { l.Log(LevelWarn, msg, fields...) }
func (l *Logger) Error(msg string, fields ...any) { l.Log(LevelError, msg, fields...) }
func (l *Logger) Fatal(msg string, fields ...any) { l.Log(LevelFatal, msg, fields...); os.Exit(1) }

func caller(skip int) string {
	_, file, line, ok := runtime.Caller(skip + 1)
	if !ok { return "" }
	return fmt.Sprintf("%s:%d", filepath.Base(file), line)
}

func parseLevel(s string) (Level, bool) {
	switch strings.ToUpper(s) {
	case "DEBUG": return LevelDebug, true
	case "INFO": return LevelInfo, true
	case "WARN": return LevelWarn, true
	case "ERROR": return LevelError, true
	case "FATAL": return LevelFatal, true
	}
	return LevelInfo, false
}

// ── Global convenience ─────────────────────────────────────

var defaultLogger = New(LevelInfo)

func SetLevel(level Level) { defaultLogger.level = level }
func Debug(msg string, fields ...any) { defaultLogger.Debug(msg, fields...) }
func Info(msg string, fields ...any)  { defaultLogger.Info(msg, fields...) }
func Warn(msg string, fields ...any)  { defaultLogger.Warn(msg, fields...) }
func Error(msg string, fields ...any) { defaultLogger.Error(msg, fields...) }
