package langgraph

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	labworkspace "lumen/internal/science/lab/workspace"
)

var historyMu sync.Mutex

// HistoryEntry is one sidecar run persisted per project.
type HistoryEntry struct {
	ID        string `json:"id"`
	TS        int64  `json:"ts"`
	ProjectID string `json:"project_id"`
	Prompt    string `json:"prompt"`
	OK        bool   `json:"ok"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
	Mode      string `json:"mode,omitempty"` // heuristic | llm
}

const (
	historyFileName  = "langgraph-history.jsonl"
	historyMaxLines  = 100
	historyMaxResult = 12000
)

// HistoryPath returns the jsonl path under a project directory.
func HistoryPath(projectDir string) string {
	return filepath.Join(projectDir, historyFileName)
}

// AppendHistory appends one entry and trims the file to historyMaxLines.
func AppendHistory(projectDir string, e HistoryEntry) error {
	historyMu.Lock()
	defer historyMu.Unlock()
	if strings.TrimSpace(projectDir) == "" {
		return nil
	}
	g, err := labworkspace.NewGuard(projectDir)
	if err != nil {
		return err
	}
	if e.ID == "" {
		e.ID = "lg_" + time.Now().UTC().Format("20060102T150405") + "_" + randomSuffix()
	}
	if e.TS == 0 {
		e.TS = time.Now().UnixMilli()
	}
	if len(e.Result) > historyMaxResult {
		e.Result = e.Result[:historyMaxResult] + "\n…(截断)"
	}
	if len(e.Prompt) > 2000 {
		e.Prompt = e.Prompt[:2000]
	}
	data, err := g.ReadFile(historyFileName)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	lines := bytes.Split(bytes.TrimSpace(append(data, append(line, '\n')...)), []byte{'\n'})
	if len(lines) > historyMaxLines {
		lines = lines[len(lines)-historyMaxLines:]
	}
	return g.AtomicWriteFile(historyFileName, append(bytes.Join(lines, []byte{'\n'}), '\n'), 0o600)
}

// ListHistory returns newest-first entries (up to limit).
func ListHistory(projectDir string, limit int) ([]HistoryEntry, error) {
	historyMu.Lock()
	defer historyMu.Unlock()
	return listHistory(projectDir, limit)
}
func listHistory(projectDir string, limit int) ([]HistoryEntry, error) {
	if limit <= 0 {
		limit = 40
	}
	g, err := labworkspace.NewGuard(projectDir)
	if err != nil {
		return nil, err
	}
	data, err := g.ReadFile(historyFileName)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var all []HistoryEntry
	for _, raw := range bytes.Split(data, []byte{'\n'}) {
		line := strings.TrimSpace(string(raw))
		if line == "" {
			continue
		}
		var e HistoryEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		all = append(all, e)
	}
	// newest last in file → reverse
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func trimHistoryFile(path string, max int) error {
	entries, err := ListHistory(filepath.Dir(path), max*2)
	if err != nil || len(entries) <= max {
		return err
	}
	// ListHistory returns newest first; keep first max, rewrite oldest→newest
	keep := entries[:max]
	for i, j := 0, len(keep)-1; i < j; i, j = i+1, j-1 {
		keep[i], keep[j] = keep[j], keep[i]
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, e := range keep {
		if err := enc.Encode(e); err != nil {
			f.Close()
			return err
		}
	}
	f.Close()
	return os.Rename(tmp, path)
}

func randomSuffix() string {
	// short non-crypto suffix
	return time.Now().Format("150405.000")
}
