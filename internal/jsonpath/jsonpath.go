// Package jsonpath implements a JSONPath expression evaluator based on a
// practical subset of RFC 9535. It supports the most commonly used features:
//
//   - $.store.book[*].author          — child and wildcard selection
//   - $..author                        — recursive descent
//   - $.store.book[0]                  — array index
//   - $.store.book[?(@.price < 10)]    — filter expressions
//   - $.store.book[0:2]                — array slice
//   - $..book[-1]                      — negative indexing
//   - $.store.book[?(@.category == 'fiction')] — comparison filters
//
// Usage:
//
//	expr, err := jsonpath.Parse("$.store.book[*].author")
//	if err != nil { ... }
//	result, err := expr.Evaluate(data)
//	fmt.Println(jsonpath.FormatPath(expr))
package jsonpath

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// ---------------------------------------------------------------------------
// AST nodes
// ---------------------------------------------------------------------------

// Node is a single segment in a JSONPath expression.
type Node interface {
	nodeMarker()
	String() string
}

// RootNode represents the root "$" token.
type RootNode struct{}

func (RootNode) nodeMarker()    {}
func (RootNode) String() string { return "$" }

// ChildNode represents a named child accessor: .field or ['field'] or bare after ..
type ChildNode struct {
	Key    string
	Bare   bool // true when this follows .. (no leading dot)
	Quoted bool // true when bracket notation was used ['key']
}

func (ChildNode) nodeMarker() {}
func (n ChildNode) String() string {
	if n.Bare {
		return n.Key
	}
	if n.Quoted {
		return "['" + escapeString(n.Key) + "']"
	}
	if isSimpleName(n.Key) {
		return "." + n.Key
	}
	return "['" + escapeString(n.Key) + "']"
}

// RecursiveDescent represents ".." — search recursively for the next segment.
type RecursiveDescent struct{}

func (RecursiveDescent) nodeMarker()    {}
func (RecursiveDescent) String() string { return ".." }

// WildcardNode represents "*" — select all children/elements.
type WildcardNode struct {
	Bracket bool // true if was [*], false if .*
}

func (WildcardNode) nodeMarker() {}
func (n WildcardNode) String() string {
	if n.Bracket {
		return "[*]"
	}
	return "*"
}

// IndexNode represents an array index: [0], [-1], etc.
type IndexNode struct {
	Index int
}

func (IndexNode) nodeMarker() {}
func (n IndexNode) String() string {
	if n.Index >= 0 {
		return fmt.Sprintf("[%d]", n.Index)
	}
	return fmt.Sprintf("[%d]", n.Index)
}

// SliceNode represents an array slice: [start:end:step]
type SliceNode struct {
	Start int
	End   int
	Step  int
	// Flags indicate whether each field was explicitly provided.
	HasStart bool
	HasEnd   bool
	HasStep  bool
}

func (SliceNode) nodeMarker() {}
func (n SliceNode) String() string {
	var parts []string
	if n.HasStart {
		parts = append(parts, fmt.Sprintf("%d", n.Start))
	}
	parts = append(parts, "")
	if n.HasEnd {
		parts[len(parts)-1] = fmt.Sprintf(":%d", n.End)
	} else {
		parts[len(parts)-1] = ":"
	}
	if n.HasStep {
		parts = append(parts, fmt.Sprintf(":%d", n.Step))
	}
	return "[" + strings.Join(parts, "") + "]"
}

// FilterNode represents a filter expression: [?(@.field op value)]
type FilterNode struct {
	Expression FilterExpr
}

func (FilterNode) nodeMarker() {}
func (n FilterNode) String() string {
	return "[?(" + n.Expression.String() + ")]"
}

// FilterExpr is a boolean expression used in filters.
type FilterExpr interface {
	filterMarker()
	String() string
}

// ComparisonExpr: @.field <op> value
type ComparisonExpr struct {
	Left  FilterExpr
	Op    string
	Right FilterExpr
}

func (ComparisonExpr) filterMarker() {}
func (e ComparisonExpr) String() string {
	return fmt.Sprintf("%s %s %s", e.Left, e.Op, e.Right)
}

// LogicalExpr: left && right or left || right
type LogicalExpr struct {
	Left  FilterExpr
	Op    string
	Right FilterExpr
}

func (LogicalExpr) filterMarker() {}
func (e LogicalExpr) String() string {
	return fmt.Sprintf("%s %s %s", e.Left, e.Op, e.Right)
}

// NotExpr: !expr
type NotExpr struct {
	Expr FilterExpr
}

func (NotExpr) filterMarker()    {}
func (e NotExpr) String() string { return "!" + e.Expr.String() }

// CurrentNodeExpr represents "@" (the current node in a filter).
type CurrentNodeExpr struct {
	Path []string // optional sub-path from current node
}

func (CurrentNodeExpr) filterMarker() {}
func (e CurrentNodeExpr) String() string {
	if len(e.Path) == 0 {
		return "@"
	}
	return "@." + strings.Join(e.Path, ".")
}

// LiteralExpr is a literal value: string, number, bool, null.
type LiteralExpr struct {
	Kind  string // "string", "number", "bool", "null"
	Value string
}

func (LiteralExpr) filterMarker() {}
func (e LiteralExpr) String() string {
	switch e.Kind {
	case "string":
		return "'" + e.Value + "'"
	case "null":
		return "null"
	default:
		return e.Value
	}
}

// Path is a compiled JSONPath expression.
type Path struct {
	Nodes []Node
}

// String returns the canonical string form.
func (p *Path) String() string {
	var sb strings.Builder
	for _, n := range p.Nodes {
		sb.WriteString(n.String())
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------------

// Parser parses JSONPath expressions.
type Parser struct {
	input string
	pos   int
}

// Parse parses a JSONPath string into a Path.
func Parse(input string) (*Path, error) {
	p := &Parser{input: input}
	return p.parse()
}

func (p *Parser) parse() (*Path, error) {
	path := &Path{}

	// Must start with "$".
	p.skipWS()
	if p.peek() != '$' {
		return nil, p.errf("JSONPath must start with '$'")
	}
	p.advance()
	path.Nodes = append(path.Nodes, RootNode{})

	for p.pos < len(p.input) {
		p.skipWS()
		if p.pos >= len(p.input) {
			break
		}

		switch p.peek() {
		case '.':
			p.advance()
			if p.peek() == '.' {
				p.advance()
				path.Nodes = append(path.Nodes, RecursiveDescent{})
				// After .. the next segment may be a bare field name, * , or bracket.
				p.skipWS()
				if p.peek() == '*' {
					p.advance()
					path.Nodes = append(path.Nodes, WildcardNode{Bracket: false})
				} else if p.peek() == '[' {
					continue
				} else if name := p.readName(); name != "" {
					path.Nodes = append(path.Nodes, ChildNode{Key: name, Bare: true})
				}
				continue
			}
			// Named child: .field or .*
			if p.peek() == '*' {
				p.advance()
				path.Nodes = append(path.Nodes, WildcardNode{Bracket: false})
				continue
			}
			name := p.readName()
			if name == "" {
				return nil, p.errf("expected field name after '.'")
			}
			path.Nodes = append(path.Nodes, ChildNode{Key: name})

		case '[':
			p.advance()
			p.skipWS()
			node, err := p.parseBracket()
			if err != nil {
				return nil, err
			}
			path.Nodes = append(path.Nodes, node)
			p.skipWS()
			if p.peek() != ']' {
				return nil, p.errf("expected ']'")
			}
			p.advance()

		default:
			return nil, p.errf("unexpected character %q", p.peek())
		}
	}

	if len(path.Nodes) == 1 {
		return nil, p.errf("empty path (only '$')")
	}

	return path, nil
}

func (p *Parser) parseBracket() (Node, error) {
	ch := p.peek()

	// [*] wildcard
	if ch == '*' {
		p.advance()
		return WildcardNode{Bracket: true}, nil
	}

	// [?  filter
	if ch == '?' {
		p.advance()
		p.skipWS()
		if p.peek() != '(' {
			return nil, p.errf("expected '(' after '?'")
		}
		p.advance()
		expr, err := p.parseFilterExpr()
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.peek() != ')' {
			return nil, p.errf("expected ')' closing filter")
		}
		p.advance()
		return FilterNode{Expression: expr}, nil
	}

	// Number: index or slice.
	if ch >= '0' && ch <= '9' || ch == '-' {
		start := p.readInt()
		p.skipWS()
		if p.peek() == ':' {
			p.advance()
			p.skipWS()
			if p.peek() == ':' {
				// start:step or start::step
				p.advance()
				p.skipWS()
				step := p.readInt()
				p.skipWS()
				return SliceNode{
					Start: start, HasStart: true,
					End: 0, HasEnd: false,
					Step: step, HasStep: true,
				}, nil
			}
			if p.peek() == ']' || p.peek() == ' ' {
				// start:
				return SliceNode{
					Start: start, HasStart: true,
					End: 0, HasEnd: false,
					Step: 1, HasStep: false,
				}, nil
			}
			end := p.readInt()
			p.skipWS()
			if p.peek() == ':' {
				p.advance()
				p.skipWS()
				step := p.readInt()
				return SliceNode{
					Start: start, HasStart: true,
					End: end, HasEnd: true,
					Step: step, HasStep: true,
				}, nil
			}
			return SliceNode{
				Start: start, HasStart: true,
				End: end, HasEnd: true,
				Step: 1, HasStep: false,
			}, nil
		}
		return IndexNode{Index: start}, nil
	}

	// :end or ::step (leading colon)
	if ch == ':' {
		p.advance()
		p.skipWS()
		if p.peek() == ':' {
			p.advance()
			p.skipWS()
			step := p.readInt()
			return SliceNode{
				Start: 0, HasStart: false,
				End: 0, HasEnd: false,
				Step: step, HasStep: true,
			}, nil
		}
		if p.peek() == ']' || p.peek() == ' ' {
			return SliceNode{
				Start: 0, HasStart: false,
				End: 0, HasEnd: false,
				Step: 1, HasStep: false,
			}, nil
		}
		end := p.readInt()
		p.skipWS()
		if p.peek() == ':' {
			p.advance()
			p.skipWS()
			step := p.readInt()
			return SliceNode{
				Start: 0, HasStart: false,
				End: end, HasEnd: true,
				Step: step, HasStep: true,
			}, nil
		}
		return SliceNode{
			Start: 0, HasStart: false,
			End: end, HasEnd: true,
			Step: 1, HasStep: false,
		}, nil
	}

	// Quoted name: ['field-name'] or ["field-name"]
	if ch == '\'' || ch == '"' {
		quote := p.advanceGet()
		name := p.readUntil(byte(quote))
		if p.peek() != byte(quote) {
			return nil, p.errf("unterminated string in bracket")
		}
		p.advance()
		return ChildNode{Key: name, Quoted: true}, nil
	}

	return nil, p.errf("unexpected character %q in bracket", ch)
}

func (p *Parser) parseFilterExpr() (FilterExpr, error) {
	return p.parseOrExpr()
}

func (p *Parser) parseOrExpr() (FilterExpr, error) {
	left, err := p.parseAndExpr()
	if err != nil {
		return nil, err
	}
	p.skipWS()
	if strings.HasPrefix(p.input[p.pos:], "||") {
		p.pos += 2
		p.skipWS()
		right, err := p.parseOrExpr()
		if err != nil {
			return nil, err
		}
		return LogicalExpr{Left: left, Op: "||", Right: right}, nil
	}
	return left, nil
}

func (p *Parser) parseAndExpr() (FilterExpr, error) {
	left, err := p.parseUnaryExpr()
	if err != nil {
		return nil, err
	}
	p.skipWS()
	if strings.HasPrefix(p.input[p.pos:], "&&") {
		p.pos += 2
		p.skipWS()
		right, err := p.parseAndExpr()
		if err != nil {
			return nil, err
		}
		return LogicalExpr{Left: left, Op: "&&", Right: right}, nil
	}
	return left, nil
}

func (p *Parser) parseUnaryExpr() (FilterExpr, error) {
	p.skipWS()
	if p.peek() == '!' {
		p.advance()
		expr, err := p.parseUnaryExpr()
		if err != nil {
			return nil, err
		}
		return NotExpr{Expr: expr}, nil
	}
	return p.parseComparison()
}

func (p *Parser) parseComparison() (FilterExpr, error) {
	p.skipWS()
	if p.peek() == '(' {
		p.advance()
		expr, err := p.parseOrExpr()
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.peek() != ')' {
			return nil, p.errf("expected ')'")
		}
		p.advance()
		return expr, nil
	}

	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	p.skipWS()

	// Check for comparison operator.
	op := ""
	switch {
	case strings.HasPrefix(p.input[p.pos:], "=="):
		op = "=="
		p.pos += 2
	case strings.HasPrefix(p.input[p.pos:], "!="):
		op = "!="
		p.pos += 2
	case strings.HasPrefix(p.input[p.pos:], ">="):
		op = ">="
		p.pos += 2
	case strings.HasPrefix(p.input[p.pos:], "<="):
		op = "<="
		p.pos += 2
	case p.peek() == '>':
		op = ">"
		p.advance()
	case p.peek() == '<':
		op = "<"
		p.advance()
	case p.peek() == '=':
		op = "="
		p.advance()
	}

	if op != "" {
		p.skipWS()
		right, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return ComparisonExpr{Left: left, Op: op, Right: right}, nil
	}

	return left, nil
}

func (p *Parser) parsePrimary() (FilterExpr, error) {
	p.skipWS()
	ch := p.peek()

	switch {
	case ch == '@':
		p.advance()
		var path []string
		if p.peek() == '.' {
			p.advance()
			path = append(path, p.readName())
			for p.peek() == '.' {
				p.advance()
				path = append(path, p.readName())
			}
		}
		return CurrentNodeExpr{Path: path}, nil

	case ch == '\'':
		p.advance()
		s := p.readUntil('\'')
		if p.peek() != '\'' {
			return nil, p.errf("unterminated string")
		}
		p.advance()
		return LiteralExpr{Kind: "string", Value: s}, nil

	case ch == '"':
		p.advance()
		s := p.readUntil('"')
		if p.peek() != '"' {
			return nil, p.errf("unterminated string")
		}
		p.advance()
		return LiteralExpr{Kind: "string", Value: s}, nil

	case ch == '-' || (ch >= '0' && ch <= '9'):
		num := p.readNumber()
		return LiteralExpr{Kind: "number", Value: num}, nil

	case ch == 't' || ch == 'f':
		word := p.readName()
		if word == "true" || word == "false" {
			return LiteralExpr{Kind: "bool", Value: word}, nil
		}
		// Might be a function call like "length(@.items)"
		return LiteralExpr{Kind: "function", Value: word}, nil

	case ch == 'n':
		word := p.readName()
		if word == "null" {
			return LiteralExpr{Kind: "null", Value: "null"}, nil
		}
		return LiteralExpr{Kind: "function", Value: word}, nil

	default:
		if isIdentStart(ch) {
			word := p.readName()
			return LiteralExpr{Kind: "function", Value: word}, nil
		}
		return nil, p.errf("unexpected character %q in filter expression", ch)
	}
}

// ---------------------------------------------------------------------------
// Evaluator
// ---------------------------------------------------------------------------

// Evaluate runs a compiled JSONPath expression against JSON data.
// data should be a Go value obtained from json.Unmarshal (interface{}).
func (p *Path) Evaluate(data interface{}) ([]interface{}, error) {
	current := []interface{}{data}
	return p.evaluateNodes(p.Nodes[1:], current)
}

func (p *Path) evaluateNodes(nodes []Node, current []interface{}) ([]interface{}, error) {
	if len(nodes) == 0 {
		return current, nil
	}

	node := nodes[0]
	rest := nodes[1:]

	// Special case: RecursiveDescent followed by ChildNode — do a direct
	// recursive key search instead of collecting all descendants first.
	if _, ok := node.(RecursiveDescent); ok && len(rest) > 0 {
		if cn, ok2 := rest[0].(ChildNode); ok2 {
			var results []interface{}
			for _, item := range current {
				r := recursiveKeySearch(item, cn.Key)
				results = append(results, r...)
			}
			if len(rest) > 1 {
				return p.evaluateNodes(rest[1:], results)
			}
			return results, nil
		}
	}

	var next []interface{}
	for _, item := range current {
		results, err := p.applyNode(node, item)
		if err != nil {
			return nil, err
		}
		next = append(next, results...)
	}

	if len(rest) == 0 {
		return next, nil
	}
	return p.evaluateNodes(rest, next)
}

// recursiveKeySearch finds all values for key at any depth in item.
func recursiveKeySearch(item interface{}, key string) []interface{} {
	var results []interface{}
	switch v := item.(type) {
	case map[string]interface{}:
		if val, ok := v[key]; ok {
			results = append(results, val)
		}
		for _, val := range v {
			results = append(results, recursiveKeySearch(val, key)...)
		}
	case []interface{}:
		for _, val := range v {
			results = append(results, recursiveKeySearch(val, key)...)
		}
	}
	return results
}

func (p *Path) applyNode(node Node, item interface{}) ([]interface{}, error) {
	switch n := node.(type) {
	case ChildNode:
		return p.applyChild(n, item)
	case RecursiveDescent:
		return p.applyRecursive(n, item)
	case WildcardNode:
		return p.applyWildcard(item)
	case IndexNode:
		return p.applyIndex(n, item)
	case SliceNode:
		return p.applySlice(n, item)
	case FilterNode:
		return p.applyFilter(n, item)
	default:
		return nil, fmt.Errorf("jsonpath: unknown node type %T", node)
	}
}

func (p *Path) applyChild(n ChildNode, item interface{}) ([]interface{}, error) {
	// If item is an array, apply to each element (implicit iteration).
	if arr, ok := item.([]interface{}); ok {
		var results []interface{}
		for _, elem := range arr {
			r, _ := p.applyChild(n, elem)
			results = append(results, r...)
		}
		return results, nil
	}
	m, ok := item.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	if v, exists := m[n.Key]; exists {
		return []interface{}{v}, nil
	}
	return nil, nil
}

func (p *Path) applyRecursive(n RecursiveDescent, item interface{}) ([]interface{}, error) {
	// For ".." alone (no following node), collect all descendants.
	// When followed by another node, the recursion is handled differently.
	return collectDescendants(item), nil
}

func collectDescendants(item interface{}) []interface{} {
	var result []interface{}
	collectRecursive(item, &result)
	return result
}

func collectRecursive(item interface{}, result *[]interface{}) {
	switch v := item.(type) {
	case map[string]interface{}:
		for _, val := range v {
			*result = append(*result, val)
			collectRecursive(val, result)
		}
	case []interface{}:
		for _, val := range v {
			*result = append(*result, val)
			collectRecursive(val, result)
		}
	}
}

func (p *Path) applyWildcard(item interface{}) ([]interface{}, error) {
	switch v := item.(type) {
	case map[string]interface{}:
		result := make([]interface{}, 0, len(v))
		for _, val := range v {
			result = append(result, val)
		}
		return result, nil
	case []interface{}:
		return v, nil
	}
	return nil, nil
}

func (p *Path) applyIndex(n IndexNode, item interface{}) ([]interface{}, error) {
	arr, ok := item.([]interface{})
	if !ok {
		return nil, nil
	}
	idx := n.Index
	if idx < 0 {
		idx = len(arr) + idx
	}
	if idx >= 0 && idx < len(arr) {
		return []interface{}{arr[idx]}, nil
	}
	return nil, nil
}

func (p *Path) applySlice(n SliceNode, item interface{}) ([]interface{}, error) {
	arr, ok := item.([]interface{})
	if !ok {
		return nil, nil
	}
	length := len(arr)
	start, end, step := n.Start, n.End, n.Step

	if !n.HasStart {
		start = 0
	}
	if !n.HasEnd {
		end = length
	}
	if !n.HasStep {
		step = 1
	}

	// Normalize negative indices.
	if start < 0 {
		start = length + start
	}
	if end < 0 {
		end = length + end
	}

	// Clamp.
	if start < 0 {
		start = 0
	}
	if end > length {
		end = length
	}

	if step <= 0 {
		return nil, fmt.Errorf("jsonpath: slice step must be positive, got %d", step)
	}

	var result []interface{}
	for i := start; i < end; i += step {
		result = append(result, arr[i])
	}
	return result, nil
}

func (p *Path) applyFilter(n FilterNode, item interface{}) ([]interface{}, error) {
	switch v := item.(type) {
	case []interface{}:
		var result []interface{}
		for _, elem := range v {
			ok, err := evalFilterExpr(n.Expression, elem)
			if err != nil {
				return nil, err
			}
			if ok {
				result = append(result, elem)
			}
		}
		return result, nil
	case map[string]interface{}:
		ok, err := evalFilterExpr(n.Expression, v)
		if err != nil {
			return nil, err
		}
		if ok {
			return []interface{}{v}, nil
		}
		return nil, nil
	}
	return nil, nil
}

func evalFilterExpr(expr FilterExpr, current interface{}) (bool, error) {
	switch e := expr.(type) {
	case ComparisonExpr:
		left, err := resolveFilterValue(e.Left, current)
		if err != nil {
			return false, err
		}
		right, err := resolveFilterValue(e.Right, current)
		if err != nil {
			return false, err
		}
		return compareValues(left, right, e.Op)

	case LogicalExpr:
		left, err := evalFilterExpr(e.Left, current)
		if err != nil {
			return false, err
		}
		// Short-circuit
		if e.Op == "||" && left {
			return true, nil
		}
		if e.Op == "&&" && !left {
			return false, nil
		}
		return evalFilterExpr(e.Right, current)

	case NotExpr:
		v, err := evalFilterExpr(e.Expr, current)
		if err != nil {
			return false, err
		}
		return !v, nil

	case CurrentNodeExpr:
		val, err := resolveCurrentPath(e.Path, current)
		if err != nil {
			return false, err
		}
		return isTruthy(val), nil

	case LiteralExpr:
		val := literalToGo(e)
		return isTruthy(val), nil

	default:
		return false, fmt.Errorf("jsonpath: unknown filter expression %T", expr)
	}
}

func resolveFilterValue(expr FilterExpr, current interface{}) (interface{}, error) {
	switch e := expr.(type) {
	case CurrentNodeExpr:
		return resolveCurrentPath(e.Path, current)
	case LiteralExpr:
		return literalToGo(e), nil
	default:
		return nil, fmt.Errorf("jsonpath: cannot resolve value from %T", expr)
	}
}

func resolveCurrentPath(path []string, current interface{}) (interface{}, error) {
	val := current
	for _, key := range path {
		m, ok := val.(map[string]interface{})
		if !ok {
			return nil, nil
		}
		v, exists := m[key]
		if !exists {
			return nil, nil
		}
		val = v
	}
	return val, nil
}

func literalToGo(e LiteralExpr) interface{} {
	switch e.Kind {
	case "string":
		return e.Value
	case "number":
		f, err := strconv.ParseFloat(e.Value, 64)
		if err != nil {
			return e.Value
		}
		// If it's an integer, return int64.
		if f == float64(int64(f)) {
			return int64(f)
		}
		return f
	case "bool":
		return e.Value == "true"
	case "null":
		return nil
	default:
		return e.Value
	}
}

func compareValues(left, right interface{}, op string) (bool, error) {
	// Handle nil comparisons.
	if left == nil && right == nil {
		return op == "==" || op == "=", nil
	}
	if left == nil || right == nil {
		return op == "!=", nil
	}

	// Try numeric comparison.
	ln, lOk := toFloat(left)
	rn, rOk := toFloat(right)
	if lOk && rOk {
		switch op {
		case "==", "=":
			return ln == rn, nil
		case "!=":
			return ln != rn, nil
		case ">":
			return ln > rn, nil
		case ">=":
			return ln >= rn, nil
		case "<":
			return ln < rn, nil
		case "<=":
			return ln <= rn, nil
		default:
			return false, fmt.Errorf("jsonpath: unknown operator %q", op)
		}
	}

	// String comparison.
	ls := fmt.Sprintf("%v", left)
	rs := fmt.Sprintf("%v", right)
	switch op {
	case "==", "=":
		return ls == rs, nil
	case "!=":
		return ls != rs, nil
	case ">":
		return ls > rs, nil
	case ">=":
		return ls >= rs, nil
	case "<":
		return ls < rs, nil
	case "<=":
		return ls <= rs, nil
	default:
		return false, fmt.Errorf("jsonpath: unknown operator %q", op)
	}
}

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	}
	return 0, false
}

func isTruthy(v interface{}) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val != ""
	case float64:
		return val != 0
	case int64:
		return val != 0
	case int:
		return val != 0
	}
	return true
}

// ---------------------------------------------------------------------------
// Parser helpers
// ---------------------------------------------------------------------------

func (p *Parser) peek() byte {
	if p.pos >= len(p.input) {
		return 0
	}
	return p.input[p.pos]
}

func (p *Parser) advance() {
	if p.pos < len(p.input) {
		p.pos++
	}
}

func (p *Parser) advanceGet() byte {
	ch := p.peek()
	p.advance()
	return ch
}

func (p *Parser) skipWS() {
	for p.pos < len(p.input) && (p.input[p.pos] == ' ' || p.input[p.pos] == '\t' || p.input[p.pos] == '\n' || p.input[p.pos] == '\r') {
		p.pos++
	}
}

func (p *Parser) readName() string {
	start := p.pos
	for p.pos < len(p.input) && (isIdentStart(p.input[p.pos]) || (p.pos > start && isIdentPart(p.input[p.pos]))) {
		p.pos++
	}
	return p.input[start:p.pos]
}

func (p *Parser) readInt() int {
	p.skipWS()
	neg := false
	if p.peek() == '-' {
		neg = true
		p.advance()
	}
	start := p.pos
	for p.pos < len(p.input) && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
		p.pos++
	}
	s := p.input[start:p.pos]
	if s == "" {
		return 0
	}
	v, _ := strconv.Atoi(s)
	if neg {
		return -v
	}
	return v
}

func (p *Parser) readNumber() string {
	start := p.pos
	if p.peek() == '-' {
		p.advance()
	}
	for p.pos < len(p.input) && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
		p.pos++
	}
	if p.pos < len(p.input) && p.input[p.pos] == '.' {
		p.advance()
		for p.pos < len(p.input) && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
			p.pos++
		}
	}
	if p.pos < len(p.input) && (p.input[p.pos] == 'e' || p.input[p.pos] == 'E') {
		p.advance()
		if p.peek() == '+' || p.peek() == '-' {
			p.advance()
		}
		for p.pos < len(p.input) && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
			p.pos++
		}
	}
	return p.input[start:p.pos]
}

func (p *Parser) readUntil(term byte) string {
	start := p.pos
	for p.pos < len(p.input) && p.input[p.pos] != term {
		if p.input[p.pos] == '\\' {
			p.pos += 2 // skip escape
		} else {
			p.pos++
		}
	}
	s := p.input[start:p.pos]
	// Unescape.
	s = strings.ReplaceAll(s, "\\'", "'")
	s = strings.ReplaceAll(s, "\\\"", "\"")
	s = strings.ReplaceAll(s, "\\\\", "\\")
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\t", "\t")
	return s
}

func (p *Parser) errf(format string, args ...interface{}) error {
	return fmt.Errorf("jsonpath: "+format, args...)
}

func isIdentStart(b byte) bool {
	return unicode.IsLetter(rune(b)) || b == '_'
}

func isIdentPart(b byte) bool {
	return unicode.IsLetter(rune(b)) || unicode.IsDigit(rune(b)) || b == '_'
}

func isSimpleName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
		} else {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				return false
			}
		}
	}
	return true
}

func escapeString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}

// ---------------------------------------------------------------------------
// FormatPath — canonical string form of a Path
// ---------------------------------------------------------------------------

// FormatPath returns a canonical string representation.
func FormatPath(p *Path) string {
	if p == nil {
		return ""
	}
	return p.String()
}
