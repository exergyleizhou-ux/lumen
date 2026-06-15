package websearch

import "testing"

func TestAutoEngineNoKey(t *testing.T) {
	eng := AutoEngine()
	if eng != nil {
		t.Log("search engine available (API key set)")
	}
}

func TestFormatResultsEmpty(t *testing.T) {
	if s := FormatResults(nil); s != "No results found." {
		t.Errorf("nil: got %q", s)
	}
	if s := FormatResults(&Response{}); s != "No results found." {
		t.Errorf("empty: got %q", s)
	}
}

func TestFormatResults(t *testing.T) {
	resp := &Response{
		Query:  "test",
		Source: "brave",
		Results: []Result{
			{Title: "Test Result", URL: "https://example.com", Description: "A test result"},
		},
	}
	s := FormatResults(resp)
	if s == "" {
		t.Error("FormatResults should return non-empty")
	}
}

func TestFormatResultsCompact(t *testing.T) {
	resp := &Response{
		Results: []Result{
			{Title: "R1", URL: "https://a.com", Description: "desc"},
		},
	}
	s := FormatResultsCompact(resp)
	if s == "" {
		t.Error("FormatResultsCompact should return non-empty")
	}
}

func TestAvailable(t *testing.T) {
	avail := Available()
	t.Logf("web search available: %v", avail)
}

func TestEngineName(t *testing.T) {
	e := NewBraveEngine()
	if e.Name() != "brave" {
		t.Errorf("brave name: got %s", e.Name())
	}
	e2 := NewBingEngine()
	if e2.Name() != "bing" {
		t.Errorf("bing name: got %s", e2.Name())
	}
}
