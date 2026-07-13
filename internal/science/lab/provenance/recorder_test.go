package provenance

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecorderMCPImmediate(t *testing.T) {
	dir := t.TempDir()
	rec, err := NewRecorder(dir, "sess_test", "deepseek")
	if err != nil {
		t.Fatal(err)
	}
	rec.RecordMCP("pubmed", "search_articles", `{"query":"aspirin"}`)
	data, err := os.ReadFile(filepath.Join(dir, "provenance.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"kind":"mcp_call"`) {
		t.Fatalf("missing mcp_call: %s", data)
	}
	if !strings.Contains(string(data), "pubmed/search_articles") {
		t.Fatalf("missing tool: %s", data)
	}
}

func TestRecorderRunIDLinksMCPAndArtifacts(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "result.csv")
	if err := os.WriteFile(artifact, []byte("x\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec, err := NewRecorder(dir, "sess_test", "deepseek")
	if err != nil {
		t.Fatal(err)
	}
	rec.SetRunID("run_first")
	rec.RecordMCP("pubmed", "search_articles", `{"query":"aspirin"}`)
	if err := rec.RecordArtifact(artifact); err != nil {
		t.Fatal(err)
	}
	rec.SetRunID("run_second")
	rec.RecordMCP("chembl", "search", `{}`)

	f, err := os.Open(filepath.Join(dir, "provenance.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var records []Record
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("records=%#v", records)
	}
	if records[0].RunID != "run_first" || records[1].RunID != "run_first" || records[2].RunID != "run_second" {
		t.Fatalf("run linkage=%#v", records)
	}
}

func TestRecorderAppend(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "workspace")
	_ = os.MkdirAll(filepath.Join(ws, "reports"), 0o700)
	path := filepath.Join(ws, "reports", "brief.md")
	_ = os.WriteFile(path, []byte("# brief"), 0o600)

	rec, err := NewRecorder(dir, "sess_test", "deepseek")
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.RecordArtifact(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "provenance.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "workspace/reports/brief.md") {
		t.Fatalf("missing path: %s", data)
	}
}
