package jsonpath

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func mustParse(t *testing.T, s string) *Path {
	t.Helper()
	p, err := Parse(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return p
}

func loadJSON(s string) interface{} {
	var v interface{}
	json.Unmarshal([]byte(s), &v)
	return v
}

func TestParse_Root(t *testing.T) {
	_, err := Parse("$")
	if err == nil {
		t.Fatal("expected error for root-only")
	}
}

func TestParse_DotNotation(t *testing.T) {
	p := mustParse(t, "$.store.book")
	if len(p.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(p.Nodes))
	}
	_, ok := p.Nodes[1].(ChildNode)
	if !ok {
		t.Fatal("expected ChildNode")
	}
}

func TestParse_BracketNotation(t *testing.T) {
	p := mustParse(t, "$['store']['book']")
	if len(p.Nodes) != 3 {
		t.Fatalf("nodes: %d", len(p.Nodes))
	}
}

func TestParse_Wildcard(t *testing.T) {
	p := mustParse(t, "$.store[*]")
	_, ok := p.Nodes[2].(WildcardNode)
	if !ok {
		t.Fatal("expected WildcardNode")
	}
}

func TestParse_RecursiveDescent(t *testing.T) {
	p := mustParse(t, "$..author")
	_, ok := p.Nodes[1].(RecursiveDescent)
	if !ok {
		t.Fatal("expected RecursiveDescent")
	}
}

func TestParse_Index(t *testing.T) {
	p := mustParse(t, "$.store.book[0]")
	n, ok := p.Nodes[3].(IndexNode)
	if !ok {
		t.Fatal("expected IndexNode")
	}
	if n.Index != 0 {
		t.Fatalf("index: %d", n.Index)
	}
}

func TestParse_NegativeIndex(t *testing.T) {
	p := mustParse(t, "$.store.book[-1]")
	n := p.Nodes[3].(IndexNode)
	if n.Index != -1 {
		t.Fatalf("index: %d", n.Index)
	}
}

func TestParse_Slice(t *testing.T) {
	p := mustParse(t, "$.store.book[0:5]")
	n := p.Nodes[3].(SliceNode)
	if n.Start != 0 || n.End != 5 || !n.HasStart || !n.HasEnd {
		t.Fatalf("slice: %+v", n)
	}
}

func TestParse_SliceStep(t *testing.T) {
	p := mustParse(t, "$.store.book[0:10:2]")
	n := p.Nodes[3].(SliceNode)
	if n.Step != 2 || !n.HasStep {
		t.Fatalf("slice: %+v", n)
	}
}

func TestParse_FilterComparison(t *testing.T) {
	p := mustParse(t, "$.store.book[?(@.price < 10)]")
	fn, ok := p.Nodes[3].(FilterNode)
	if !ok {
		t.Fatal("expected FilterNode")
	}
	cmp, ok := fn.Expression.(ComparisonExpr)
	if !ok {
		t.Fatalf("expected ComparisonExpr, got %T", fn.Expression)
	}
	if cmp.Op != "<" {
		t.Fatalf("op: %s", cmp.Op)
	}
}

func TestParse_FilterEquality(t *testing.T) {
	p := mustParse(t, "$.store.book[?(@.category == 'fiction')]")
	fn := p.Nodes[3].(FilterNode)
	cmp := fn.Expression.(ComparisonExpr)
	if cmp.Op != "==" {
		t.Fatalf("op: %s", cmp.Op)
	}
}

func TestParse_FilterLogical(t *testing.T) {
	p := mustParse(t, "$.store.book[?(@.price < 10 && @.category == 'fiction')]")
	fn := p.Nodes[3].(FilterNode)
	log, ok := fn.Expression.(LogicalExpr)
	if !ok {
		t.Fatalf("expected LogicalExpr, got %T", fn.Expression)
	}
	if log.Op != "&&" {
		t.Fatalf("op: %s", log.Op)
	}
}

func TestParse_FilterNot(t *testing.T) {
	p := mustParse(t, "$.store.book[?(!@.sold)]")
	fn := p.Nodes[3].(FilterNode)
	_, ok := fn.Expression.(NotExpr)
	if !ok {
		t.Fatalf("expected NotExpr, got %T", fn.Expression)
	}
}

// ---------------------------------------------------------------------------
// Evaluate tests
// ---------------------------------------------------------------------------

const testDoc = `{
  "store": {
    "name": "Acme Books",
    "book": [
      {"title": "Book A", "price": 8.99, "category": "fiction", "sold": true},
      {"title": "Book B", "price": 12.50, "category": "fiction", "sold": false},
      {"title": "Book C", "price": 5.00, "category": "non-fiction", "sold": true},
      {"title": "Book D", "price": 20.00, "category": "fiction", "sold": false}
    ]
  }
}`

func TestEval_DotNotation(t *testing.T) {
	p := mustParse(t, "$.store.name")
	results, err := p.Evaluate(loadJSON(testDoc))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0] != "Acme Books" {
		t.Fatalf("results: %v", results)
	}
}

func TestEval_Wildcard(t *testing.T) {
	p := mustParse(t, "$.store.book[*].title")
	results, err := p.Evaluate(loadJSON(testDoc))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4, got %d: %v", len(results), results)
	}
}

func TestEval_Index(t *testing.T) {
	p := mustParse(t, "$.store.book[0].title")
	results, err := p.Evaluate(loadJSON(testDoc))
	if err != nil {
		t.Fatal(err)
	}
	if results[0] != "Book A" {
		t.Fatalf("got %v", results[0])
	}
}

func TestEval_NegativeIndex(t *testing.T) {
	p := mustParse(t, "$.store.book[-1].title")
	results, err := p.Evaluate(loadJSON(testDoc))
	if err != nil {
		t.Fatal(err)
	}
	if results[0] != "Book D" {
		t.Fatalf("got %v", results[0])
	}
}

func TestEval_Slice(t *testing.T) {
	p := mustParse(t, "$.store.book[1:3]")
	results, err := p.Evaluate(loadJSON(testDoc))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
	}
}

func TestEval_FilterComparison(t *testing.T) {
	p := mustParse(t, "$.store.book[?(@.price < 10)]")
	results, err := p.Evaluate(loadJSON(testDoc))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 books < $10, got %d", len(results))
	}
}

func TestEval_FilterEquality(t *testing.T) {
	p := mustParse(t, "$.store.book[?(@.category == 'non-fiction')]")
	results, err := p.Evaluate(loadJSON(testDoc))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

func TestEval_FilterLogical(t *testing.T) {
	p := mustParse(t, "$.store.book[?(@.price < 15 && @.sold == true)]")
	results, err := p.Evaluate(loadJSON(testDoc))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
	}
}

func TestEval_FilterNot(t *testing.T) {
	p := mustParse(t, "$.store.book[?(!@.sold)]")
	results, err := p.Evaluate(loadJSON(testDoc))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 unsold, got %d", len(results))
	}
}

func TestEval_FilterGTE(t *testing.T) {
	p := mustParse(t, "$.store.book[?(@.price >= 12)]")
	results, err := p.Evaluate(loadJSON(testDoc))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 (prices >=12), got %d", len(results))
	}
}

func TestEval_RecursiveDescent(t *testing.T) {
	p := mustParse(t, "$..price")
	results, err := p.Evaluate(loadJSON(testDoc))
	if err != nil {
		t.Fatal(err)
	}
	// Should find all price values recursively.
	if len(results) != 4 {
		t.Fatalf("expected 4 prices, got %d: %v", len(results), results)
	}
}

// ---------------------------------------------------------------------------
// FormatPath
// ---------------------------------------------------------------------------

func TestFormatPath(t *testing.T) {
	tests := []string{
		"$.store.name",
		"$.store.book[*].title",
		"$..author",
		"$.store.book[0]",
		"$.store.book[-1]",
		"$.store.book[0:5]",
		"$.store.book[0:10:2]",
		"$.store.book[?(@.price < 10)]",
		"$['store']['book']",
		"$.store.book[?(@.category == 'fiction')]",
	}
	for _, tt := range tests {
		p, err := Parse(tt)
		if err != nil {
			t.Fatalf("parse %q: %v", tt, err)
		}
		s := FormatPath(p)
		// Re-parse to verify roundtrip for simple cases.
		if !strings.Contains(tt, "?(") {
			if s != tt {
				t.Fatalf("roundtrip: %q -> %q", tt, s)
			}
		}
	}
}

func TestErrors(t *testing.T) {
	bad := []string{
		"",
		"hello",
		"$.",
		"$[",
		"$.store[?]",
	}
	for _, b := range bad {
		_, err := Parse(b)
		if err == nil {
			t.Fatalf("expected error for %q", b)
		}
	}
}

func TestString(t *testing.T) {
	p := mustParse(t, "$.store.book[0].title")
	if !strings.Contains(p.String(), "$.store.book[0].title") {
		t.Fatalf("String: %s", p.String())
	}
}

func TestEval_NonExistent(t *testing.T) {
	p := mustParse(t, "$.nonexistent")
	results, err := p.Evaluate(loadJSON(testDoc))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty, got %v", results)
	}
}

func TestEval_NestedFilter(t *testing.T) {
	// Filter with parenthesized logical.
	p := mustParse(t, "$.store.book[?(@.price < 10 || @.price > 15)]")
	fmt.Println("parsed:", p)
	results, err := p.Evaluate(loadJSON(testDoc))
	if err != nil {
		t.Fatal(err)
	}
	// Books: A=8.99, B=12.50, C=5.00, D=20.00
	// <10: A, C. >15: D. Total: 3.
	if len(results) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(results), results)
	}
}

func TestEval_ChildNodeOnArray(t *testing.T) {
	// Applying .field to each element of an array (implicit wildcard).
	// This is a common JSONPath idiom: $.store.book.title
	p := mustParse(t, "$.store.book.title")
	results, err := p.Evaluate(loadJSON(testDoc))
	if err != nil {
		t.Fatal(err)
	}
	// Should iterate over array and get title from each book.
	if len(results) != 4 {
		t.Fatalf("expected 4 titles, got %d: %v", len(results), results)
	}
}
