package compiler

import (
	"fmt"
	"strings"
	"testing"
)

func TestLexer_Numbers(t *testing.T) {
	l := NewLexer("42 3.14")
	tok := l.NextToken()
	if tok.Kind != TokNumber || tok.Value != "42" {
		t.Fatalf("tok: %v", tok)
	}
	tok = l.NextToken()
	if tok.Kind != TokNumber || tok.Value != "3.14" {
		t.Fatalf("tok: %v", tok)
	}
}

func TestLexer_Idents(t *testing.T) {
	l := NewLexer("foo bar_1 _baz")
	if tok := l.NextToken(); tok.Value != "foo" {
		t.Fatal(tok)
	}
	if tok := l.NextToken(); tok.Value != "bar_1" {
		t.Fatal(tok)
	}
}

func TestLexer_Operators(t *testing.T) {
	l := NewLexer("+ - * / % == != < <= > >= && || ! =")
	kinds := []TokenKind{TokPlus, TokMinus, TokStar, TokSlash, TokPercent,
		TokEq, TokNotEq, TokLt, TokLe, TokGt, TokGe, TokAnd, TokOr, TokNot, TokAssign}
	for _, k := range kinds {
		tok := l.NextToken()
		if tok.Kind != k {
			t.Fatalf("expected %s, got %s", k, tok.Kind)
		}
	}
}

func TestParse_Simple(t *testing.T) {
	node, err := ParseExpression("2 + 3 * 4")
	if err != nil {
		t.Fatal(err)
	}
	bin, ok := node.(BinaryNode)
	if !ok {
		t.Fatal("expected BinaryNode")
	}
	if bin.Op != "+" {
		t.Fatalf("op: %s", bin.Op)
	}
	// Right should be 3 * 4.
	right, ok := bin.Right.(BinaryNode)
	if !ok || right.Op != "*" {
		t.Fatal("expected multiply on right")
	}
}

func TestParse_Comparison(t *testing.T) {
	node, err := ParseExpression("x > 5")
	if err != nil {
		t.Fatal(err)
	}
	bin := node.(BinaryNode)
	if bin.Op != ">" {
		t.Fatalf("op: %s", bin.Op)
	}
}

func TestParse_Logical(t *testing.T) {
	node, err := ParseExpression("a && b || c")
	if err != nil {
		t.Fatal(err)
	}
	// Should be ((a && b) || c)
	bin := node.(BinaryNode)
	if bin.Op != "||" {
		t.Fatalf("op: %s", bin.Op)
	}
}

func TestParse_Unary(t *testing.T) {
	node, err := ParseExpression("-5")
	if err != nil {
		t.Fatal(err)
	}
	un, ok := node.(UnaryNode)
	if !ok || un.Op != "-" {
		t.Fatal("expected unary minus")
	}
}

func TestParse_Ternary(t *testing.T) {
	node, err := ParseExpression("a ? 1 : 2")
	if err != nil {
		t.Fatal(err)
	}
	ter, ok := node.(TernaryNode)
	if !ok {
		t.Fatalf("expected TernaryNode, got %T", node)
	}
	_ = ter
}

func TestParse_Call(t *testing.T) {
	node, err := ParseExpression("min(x, y)")
	if err != nil {
		t.Fatal(err)
	}
	call, ok := node.(CallNode)
	if !ok || call.Name != "min" || len(call.Args) != 2 {
		t.Fatalf("expected call min with 2 args, got %T", node)
	}
}

func TestParse_Error(t *testing.T) {
	_, err := ParseExpression("1 +")
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// Compile + VM tests
// ---------------------------------------------------------------------------

func TestCompileRun_Arithmetic(t *testing.T) {
	tests := []struct {
		expr   string
		expect float64
	}{
		{"2 + 3 * 4", 14},
		{"(2 + 3) * 4", 20},
		{"10 / 2", 5},
		{"10 - 3 - 2", 5},
		{"2 ^ 3", 8},
		{"10 % 3", 1},
		{"-5 + 10", 5},
		{"2 + 3 * 4 - 5", 9},
	}

	for _, tt := range tests {
		result, err := Evaluate(tt.expr, nil)
		if err != nil {
			t.Fatalf("%s: %v", tt.expr, err)
		}
		r := toFloat(result)
		if r != tt.expect {
			t.Fatalf("%s: expected %v, got %v", tt.expr, tt.expect, r)
		}
	}
}

func TestCompileRun_Comparison(t *testing.T) {
	tests := []struct {
		expr   string
		expect bool
	}{
		{"5 > 3", true},
		{"5 < 3", false},
		{"5 == 5", true},
		{"5 != 5", false},
		{"5 >= 5", true},
		{"5 <= 3", false},
	}

	for _, tt := range tests {
		result, err := Evaluate(tt.expr, nil)
		if err != nil {
			t.Fatalf("%s: %v", tt.expr, err)
		}
		if result.(bool) != tt.expect {
			t.Fatalf("%s: expected %v, got %v", tt.expr, tt.expect, result)
		}
	}
}

func TestCompileRun_Logical(t *testing.T) {
	// 1 && 1 => truthy (returns last evaluated value: 1)
	result, err := Evaluate("1 && 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.(float64) != 1 {
		t.Fatalf("expected 1, got %v", result)
	}

	// 1 && 0 => 0 (falsy)
	result, err = Evaluate("1 && 0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.(float64) != 0 {
		t.Fatalf("expected 0, got %v", result)
	}

	// 1 || 0 => 1 (short-circuit)
	result, err = Evaluate("1 || 0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.(float64) != 1 {
		t.Fatalf("expected 1, got %v", result)
	}

	// 0 || 0 => 0
	result, err = Evaluate("0 || 0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.(float64) != 0 {
		t.Fatalf("expected 0, got %v", result)
	}

	// Not operator.
	result, err = Evaluate("!1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.(bool) != false {
		t.Fatalf("!1 should be false, got %v", result)
	}
	result, err = Evaluate("!0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.(bool) != true {
		t.Fatalf("!0 should be true, got %v", result)
	}
}

func TestCompileRun_Variables(t *testing.T) {
	result, err := Evaluate("x + y", map[string]interface{}{
		"x": float64(10), "y": float64(20),
	})
	if err != nil {
		t.Fatal(err)
	}
	if toFloat(result) != 30 {
		t.Fatalf("expected 30, got %v", result)
	}
}

func TestCompileRun_Functions(t *testing.T) {
	result, err := Evaluate("min(10, 20)", nil)
	if err != nil {
		t.Fatal(err)
	}
	if toFloat(result) != 10 {
		t.Fatalf("expected 10, got %v", result)
	}

	result, err = Evaluate("sqrt(9)", nil)
	if err != nil {
		t.Fatal(err)
	}
	if toFloat(result) != 3 {
		t.Fatalf("expected 3, got %v", result)
	}
}

func TestCompileRun_Ternary(t *testing.T) {
	result, err := Evaluate("1 ? 10 : 20", nil)
	if err != nil {
		t.Fatal(err)
	}
	if toFloat(result) != 10 {
		t.Fatalf("expected 10, got %v", result)
	}

	result, err = Evaluate("0 ? 10 : 20", nil)
	if err != nil {
		t.Fatal(err)
	}
	if toFloat(result) != 20 {
		t.Fatalf("expected 20, got %v", result)
	}
}

func TestFormatBytecode(t *testing.T) {
	bc, err := Compile("2 + 3 * 4")
	if err != nil {
		t.Fatal(err)
	}
	out := FormatBytecode(bc, DefaultFormatBytecodeOptions())
	if !strings.Contains(out, "Bytecode") || !strings.Contains(out, "PUSH") || !strings.Contains(out, "RET") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestCompileRun_DivisionByZero(t *testing.T) {
	_, err := Evaluate("1 / 0", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCompileRun_UndefinedVar(t *testing.T) {
	_, err := Evaluate("x + 1", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTokenString(t *testing.T) {
	if TokPlus.String() != "+" {
		t.Fatal("bad string")
	}
	if TokenKind(999).String() != "Token(999)" {
		t.Fatal("bad unknown")
	}
}

func TestOpcodeString(t *testing.T) {
	if OpAdd.String() != "ADD" {
		t.Fatal("bad")
	}
	if Opcode(999).String() != "OP(999)" {
		t.Fatal("bad")
	}
}

func TestAST_String(t *testing.T) {
	node, _ := ParseExpression("2 + 3")
	s := node.String()
	if !strings.Contains(s, "+") {
		t.Fatalf("unexpected: %s", s)
	}
}

func ExampleEvaluate() {
	result, _ := Evaluate("2 + 3 * 4", nil)
	fmt.Println(result)
	// Output: 14
}
