// Package compiler implements a simple expression compiler: lexer → parser →
// AST → bytecode compiler → stack-based VM. It supports arithmetic, comparison,
// logical operators, variables, and function calls.
//
// Usage:
//
//	expr, err := compiler.Compile("2 + 3 * 4")
//	result, err := expr.Evaluate(map[string]interface{}{})
//	fmt.Println(result) // 14
package compiler

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// ---------------------------------------------------------------------------
// Token types
// ---------------------------------------------------------------------------

// TokenKind enumerates token types.
type TokenKind int

const (
	TokEOF TokenKind = iota
	TokNumber
	TokString
	TokIdent
	TokPlus
	TokMinus
	TokStar
	TokSlash
	TokPercent
	TokCaret
	TokEq
	TokNotEq
	TokLt
	TokLe
	TokGt
	TokGe
	TokAnd
	TokOr
	TokNot
	TokAssign
	TokLParen
	TokRParen
	TokComma
	TokDot
	TokQuestion
	TokColon
)

var tokenNames = map[TokenKind]string{
	TokEOF:      "EOF",
	TokNumber:   "NUMBER",
	TokString:   "STRING",
	TokIdent:    "IDENT",
	TokPlus:     "+",
	TokMinus:    "-",
	TokStar:     "*",
	TokSlash:    "/",
	TokPercent:  "%",
	TokCaret:    "^",
	TokEq:       "==",
	TokNotEq:    "!=",
	TokLt:       "<",
	TokLe:       "<=",
	TokGt:       ">",
	TokGe:       ">=",
	TokAnd:      "&&",
	TokOr:       "||",
	TokNot:      "!",
	TokAssign:   "=",
	TokLParen:   "(",
	TokRParen:   ")",
	TokComma:    ",",
	TokDot:      ".",
	TokQuestion: "?",
	TokColon:    ":",
}

func (k TokenKind) String() string {
	if s, ok := tokenNames[k]; ok {
		return s
	}
	return fmt.Sprintf("Token(%d)", k)
}

// Token is a single lexical token.
type Token struct {
	Kind  TokenKind
	Value string
	Pos   int
}

// ---------------------------------------------------------------------------
// Lexer
// ---------------------------------------------------------------------------

// Lexer tokenizes an input string.
type Lexer struct {
	input string
	pos   int
}

// NewLexer creates a lexer.
func NewLexer(input string) *Lexer {
	return &Lexer{input: input}
}

// NextToken returns the next token.
func (l *Lexer) NextToken() Token {
	l.skipWS()
	if l.pos >= len(l.input) {
		return Token{Kind: TokEOF, Pos: l.pos}
	}

	ch := l.input[l.pos]

	// Numbers.
	if ch >= '0' && ch <= '9' || ch == '.' && l.pos+1 < len(l.input) && l.input[l.pos+1] >= '0' && l.input[l.pos+1] <= '9' {
		return l.readNumber()
	}

	// Identifiers and keywords.
	if isIdentStart(ch) {
		return l.readIdent()
	}

	// Strings.
	if ch == '"' || ch == '\'' {
		return l.readString(ch)
	}

	// Multi-char operators.
	if ch == '=' && l.peek(1) == '=' {
		l.pos += 2
		return Token{Kind: TokEq, Value: "==", Pos: l.pos - 2}
	}
	if ch == '!' && l.peek(1) == '=' {
		l.pos += 2
		return Token{Kind: TokNotEq, Value: "!=", Pos: l.pos - 2}
	}
	if ch == '<' && l.peek(1) == '=' {
		l.pos += 2
		return Token{Kind: TokLe, Value: "<=", Pos: l.pos - 2}
	}
	if ch == '>' && l.peek(1) == '=' {
		l.pos += 2
		return Token{Kind: TokGe, Value: ">=", Pos: l.pos - 2}
	}
	if ch == '&' && l.peek(1) == '&' {
		l.pos += 2
		return Token{Kind: TokAnd, Value: "&&", Pos: l.pos - 2}
	}
	if ch == '|' && l.peek(1) == '|' {
		l.pos += 2
		return Token{Kind: TokOr, Value: "||", Pos: l.pos - 2}
	}

	// Single-char tokens.
	pos := l.pos
	l.pos++
	switch ch {
	case '+':
		return Token{Kind: TokPlus, Value: "+", Pos: pos}
	case '-':
		return Token{Kind: TokMinus, Value: "-", Pos: pos}
	case '*':
		return Token{Kind: TokStar, Value: "*", Pos: pos}
	case '/':
		return Token{Kind: TokSlash, Value: "/", Pos: pos}
	case '%':
		return Token{Kind: TokPercent, Value: "%", Pos: pos}
	case '^':
		return Token{Kind: TokCaret, Value: "^", Pos: pos}
	case '<':
		return Token{Kind: TokLt, Value: "<", Pos: pos}
	case '>':
		return Token{Kind: TokGt, Value: ">", Pos: pos}
	case '!':
		return Token{Kind: TokNot, Value: "!", Pos: pos}
	case '=':
		return Token{Kind: TokAssign, Value: "=", Pos: pos}
	case '(':
		return Token{Kind: TokLParen, Value: "(", Pos: pos}
	case ')':
		return Token{Kind: TokRParen, Value: ")", Pos: pos}
	case ',':
		return Token{Kind: TokComma, Value: ",", Pos: pos}
	case '.':
		return Token{Kind: TokDot, Value: ".", Pos: pos}
	case '?':
		return Token{Kind: TokQuestion, Value: "?", Pos: pos}
	case ':':
		return Token{Kind: TokColon, Value: ":", Pos: pos}
	default:
		return Token{Kind: TokEOF, Value: string(ch), Pos: pos}
	}
}

func (l *Lexer) readNumber() Token {
	start := l.pos
	for l.pos < len(l.input) && l.input[l.pos] >= '0' && l.input[l.pos] <= '9' {
		l.pos++
	}
	if l.pos < len(l.input) && l.input[l.pos] == '.' {
		l.pos++
		for l.pos < len(l.input) && l.input[l.pos] >= '0' && l.input[l.pos] <= '9' {
			l.pos++
		}
	}
	val := l.input[start:l.pos]
	return Token{Kind: TokNumber, Value: val, Pos: start}
}

func (l *Lexer) readIdent() Token {
	start := l.pos
	for l.pos < len(l.input) && (isIdentPart(l.input[l.pos])) {
		l.pos++
	}
	val := l.input[start:l.pos]
	return Token{Kind: TokIdent, Value: val, Pos: start}
}

func (l *Lexer) readString(quote byte) Token {
	l.pos++ // skip opening quote
	start := l.pos
	var sb strings.Builder
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == quote {
			val := sb.String()
			l.pos++
			return Token{Kind: TokString, Value: val, Pos: start - 1}
		}
		if ch == '\\' && l.pos+1 < len(l.input) {
			l.pos++
			switch l.input[l.pos] {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case '\\':
				sb.WriteByte('\\')
			case '"':
				sb.WriteByte('"')
			case '\'':
				sb.WriteByte('\'')
			default:
				sb.WriteByte(l.input[l.pos])
			}
		} else {
			sb.WriteByte(ch)
		}
		l.pos++
	}
	return Token{Kind: TokEOF, Value: sb.String(), Pos: start}
}

func (l *Lexer) skipWS() {
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			l.pos++
		} else {
			break
		}
	}
}

func (l *Lexer) peek(offset int) byte {
	if l.pos+offset >= len(l.input) {
		return 0
	}
	return l.input[l.pos+offset]
}

func isIdentStart(ch byte) bool {
	return unicode.IsLetter(rune(ch)) || ch == '_'
}

func isIdentPart(ch byte) bool {
	return unicode.IsLetter(rune(ch)) || unicode.IsDigit(rune(ch)) || ch == '_'
}

// ---------------------------------------------------------------------------
// AST nodes
// ---------------------------------------------------------------------------

// Node is an AST node.
type Node interface {
	nodeMarker()
	String() string
}

// NumberNode is a numeric literal.
type NumberNode struct {
	Value float64
	Raw   string
}

func (NumberNode) nodeMarker()   {}
func (n NumberNode) String() string { return n.Raw }

// StringNode is a string literal.
type StringNode struct{ Value string }
func (StringNode) nodeMarker()    {}
func (n StringNode) String() string { return fmt.Sprintf("%q", n.Value) }

// IdentNode is a variable reference.
type IdentNode struct{ Name string }
func (IdentNode) nodeMarker()     {}
func (n IdentNode) String() string { return n.Name }

// BinaryNode is a binary operation.
type BinaryNode struct {
	Op    string
	Left  Node
	Right Node
}

func (BinaryNode) nodeMarker() {}
func (n BinaryNode) String() string {
	return fmt.Sprintf("(%s %s %s)", n.Left, n.Op, n.Right)
}

// UnaryNode is a unary operation.
type UnaryNode struct {
	Op   string
	Expr Node
}

func (UnaryNode) nodeMarker() {}
func (n UnaryNode) String() string {
	return fmt.Sprintf("(%s %s)", n.Op, n.Expr)
}

// TernaryNode is a ?: conditional.
type TernaryNode struct {
	Cond  Node
	True  Node
	False Node
}

func (TernaryNode) nodeMarker() {}
func (n TernaryNode) String() string {
	return fmt.Sprintf("(%s ? %s : %s)", n.Cond, n.True, n.False)
}

// CallNode is a function call.
type CallNode struct {
	Name string
	Args []Node
}

func (CallNode) nodeMarker() {}
func (n CallNode) String() string {
	args := make([]string, len(n.Args))
	for i, a := range n.Args {
		args[i] = a.String()
	}
	return fmt.Sprintf("%s(%s)", n.Name, strings.Join(args, ", "))
}

// ---------------------------------------------------------------------------
// Parser — recursive descent
// ---------------------------------------------------------------------------

// Parser parses tokens into an AST.
type Parser struct {
	lexer *Lexer
	tok   Token
}

// ParseExpression parses an expression string into an AST.
func ParseExpression(input string) (Node, error) {
	p := &Parser{lexer: NewLexer(input)}
	p.advance()
	return p.parseTernary()
}

func (p *Parser) advance() {
	p.tok = p.lexer.NextToken()
}

func (p *Parser) parseTernary() (Node, error) {
	left, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.tok.Kind == TokQuestion {
		p.advance()
		trueExpr, err := p.parseTernary()
		if err != nil {
			return nil, err
		}
		if p.tok.Kind != TokColon {
			return nil, fmt.Errorf("expected ':' in ternary, got %s", p.tok.Kind)
		}
		p.advance()
		falseExpr, err := p.parseTernary()
		if err != nil {
			return nil, err
		}
		return TernaryNode{Cond: left, True: trueExpr, False: falseExpr}, nil
	}
	return left, nil
}

func (p *Parser) parseOr() (Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.tok.Kind == TokOr {
		op := p.tok.Value
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = BinaryNode{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseAnd() (Node, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for p.tok.Kind == TokAnd {
		op := p.tok.Value
		p.advance()
		right, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		left = BinaryNode{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseComparison() (Node, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	for p.tok.Kind == TokEq || p.tok.Kind == TokNotEq ||
		p.tok.Kind == TokLt || p.tok.Kind == TokLe ||
		p.tok.Kind == TokGt || p.tok.Kind == TokGe {
		op := p.tok.Value
		p.advance()
		right, err := p.parseAdditive()
		if err != nil {
			return nil, err
		}
		left = BinaryNode{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseAdditive() (Node, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for p.tok.Kind == TokPlus || p.tok.Kind == TokMinus {
		op := p.tok.Value
		p.advance()
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = BinaryNode{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseMultiplicative() (Node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.tok.Kind == TokStar || p.tok.Kind == TokSlash || p.tok.Kind == TokPercent {
		op := p.tok.Value
		p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = BinaryNode{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseUnary() (Node, error) {
	if p.tok.Kind == TokMinus || p.tok.Kind == TokNot {
		op := p.tok.Value
		p.advance()
		expr, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return UnaryNode{Op: op, Expr: expr}, nil
	}
	return p.parsePrimary()
}

func (p *Parser) parsePrimary() (Node, error) {
	switch p.tok.Kind {
	case TokNumber:
		v, _ := strconv.ParseFloat(p.tok.Value, 64)
		n := NumberNode{Value: v, Raw: p.tok.Value}
		p.advance()

		// Check for function call: number followed by '(' would be invalid.
		// Check for power operator ^.
		if p.tok.Kind == TokCaret {
			p.advance()
			right, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			return BinaryNode{Op: "^", Left: n, Right: right}, nil
		}

		return n, nil
	case TokString:
		n := StringNode{Value: p.tok.Value}
		p.advance()
		return n, nil
	case TokIdent:
		name := p.tok.Value
		p.advance()
		// Function call?
		if p.tok.Kind == TokLParen {
			p.advance()
			var args []Node
			if p.tok.Kind != TokRParen {
				for {
					arg, err := p.parseTernary()
					if err != nil {
						return nil, err
					}
					args = append(args, arg)
					if p.tok.Kind == TokComma {
						p.advance()
						continue
					}
					break
				}
			}
			if p.tok.Kind != TokRParen {
				return nil, fmt.Errorf("expected ')', got %s", p.tok.Kind)
			}
			p.advance()
			return CallNode{Name: name, Args: args}, nil
		}
		return IdentNode{Name: name}, nil
	case TokLParen:
		p.advance()
		expr, err := p.parseTernary()
		if err != nil {
			return nil, err
		}
		if p.tok.Kind != TokRParen {
			return nil, fmt.Errorf("expected ')', got %s", p.tok.Kind)
		}
		p.advance()
		return expr, nil
	default:
		return nil, fmt.Errorf("unexpected token %s (%q)", p.tok.Kind, p.tok.Value)
	}
}

// ---------------------------------------------------------------------------
// Bytecode instructions
// ---------------------------------------------------------------------------

// Opcode is a bytecode instruction opcode.
type Opcode int

const (
	OpPush   Opcode = iota // push a constant from pool
	OpLoad                  // load a variable
	OpStore                 // store to a variable
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpPow
	OpNeg
	OpNot
	OpEq
	OpNotEq
	OpLt
	OpLe
	OpGt
	OpGe
	OpAnd
	OpOr
	OpJmpFalse  // conditional jump if top of stack is false (consumes value)
	OpJmp       // unconditional jump
	OpDup       // duplicate top of stack
	OpCall      // call function (name in pool)
	OpRet       // return
	OpPop       // discard top of stack
)

var opcodeNames = map[Opcode]string{
	OpPush:    "PUSH",
	OpLoad:    "LOAD",
	OpStore:   "STORE",
	OpAdd:     "ADD",
	OpSub:     "SUB",
	OpMul:     "MUL",
	OpDiv:     "DIV",
	OpMod:     "MOD",
	OpPow:     "POW",
	OpNeg:     "NEG",
	OpNot:     "NOT",
	OpEq:      "EQ",
	OpNotEq:   "NEQ",
	OpLt:      "LT",
	OpLe:      "LE",
	OpGt:      "GT",
	OpGe:      "GE",
	OpAnd:     "AND",
	OpOr:      "OR",
	OpJmpFalse: "JMPF",
	OpJmp:     "JMP",
	OpDup:     "DUP",
	OpCall:    "CALL",
	OpRet:     "RET",
	OpPop:     "POP",
}

func (o Opcode) String() string {
	if s, ok := opcodeNames[o]; ok {
		return s
	}
	return fmt.Sprintf("OP(%d)", o)
}

// Instruction is a single bytecode instruction.
type Instruction struct {
	Op   Opcode
	Arg  int // index into constant/variable pool
}

// Bytecode is a compiled expression ready for execution.
type Bytecode struct {
	Instructions []Instruction
	Constants    []interface{}
	Variables    []string
}

// ---------------------------------------------------------------------------
// Compiler — AST → bytecode
// ---------------------------------------------------------------------------

// Compiler converts an AST to bytecode.
type Compiler struct {
	constants []interface{}
	variables map[string]int
}

// Compile parses and compiles an expression.
func Compile(input string) (*Bytecode, error) {
	ast, err := ParseExpression(input)
	if err != nil {
		return nil, err
	}
	c := &Compiler{
		variables: make(map[string]int),
	}
	return c.compile(ast)
}

func (c *Compiler) compile(node Node) (*Bytecode, error) {
	bc := &Bytecode{}

	if err := c.compileNode(node, bc); err != nil {
		return nil, err
	}
	bc.Instructions = append(bc.Instructions, Instruction{Op: OpRet})
	bc.Constants = c.constants
	bc.Variables = make([]string, len(c.variables))
	for name, idx := range c.variables {
		bc.Variables[idx] = name
	}
	return bc, nil
}

func (c *Compiler) compileNode(node Node, bc *Bytecode) error {
	switch n := node.(type) {
	case NumberNode:
		idx := c.addConst(n.Value)
		bc.Instructions = append(bc.Instructions, Instruction{Op: OpPush, Arg: idx})
	case StringNode:
		idx := c.addConst(n.Value)
		bc.Instructions = append(bc.Instructions, Instruction{Op: OpPush, Arg: idx})
	case IdentNode:
		idx := c.addVar(n.Name)
		bc.Instructions = append(bc.Instructions, Instruction{Op: OpLoad, Arg: idx})
	case BinaryNode:
		// Special case for short-circuit && and ||.
		if n.Op == "&&" {
			// left; JMPF end; pop; right; end:
			if err := c.compileNode(n.Left, bc); err != nil {
				return err
			}
			// Duplicate top for result, jump if false.
			jmpIdx := len(bc.Instructions)
			bc.Instructions = append(bc.Instructions, Instruction{Op: OpJmpFalse, Arg: 0})
			bc.Instructions = append(bc.Instructions, Instruction{Op: OpPop}) // discard left result
			if err := c.compileNode(n.Right, bc); err != nil {
				return err
			}
			endIdx := len(bc.Instructions)
			bc.Instructions[jmpIdx].Arg = endIdx
		} else if n.Op == "||" {
			if err := c.compileNode(n.Left, bc); err != nil {
				return err
			}
			// Dup left so we can test it without consuming the result.
			bc.Instructions = append(bc.Instructions, Instruction{Op: OpDup})
			jmpIdx := len(bc.Instructions)
			bc.Instructions = append(bc.Instructions, Instruction{Op: OpJmpFalse, Arg: 0})
			// Left is truthy: JMPF consumed the duplicate. The original is
			// still on the stack. Skip to end.
			skipIdx := len(bc.Instructions)
			bc.Instructions = append(bc.Instructions, Instruction{Op: OpJmp, Arg: 0})
			// Left was falsy: discard the original and evaluate right.
			bc.Instructions[jmpIdx].Arg = len(bc.Instructions)
			bc.Instructions = append(bc.Instructions, Instruction{Op: OpPop})
			if err := c.compileNode(n.Right, bc); err != nil {
				return err
			}
			bc.Instructions[skipIdx].Arg = len(bc.Instructions)
		} else {
			if err := c.compileNode(n.Left, bc); err != nil {
				return err
			}
			if err := c.compileNode(n.Right, bc); err != nil {
				return err
			}
			op := binaryOpcode(n.Op)
			bc.Instructions = append(bc.Instructions, Instruction{Op: op})
		}
	case UnaryNode:
		if err := c.compileNode(n.Expr, bc); err != nil {
			return err
		}
		op := unaryOpcode(n.Op)
		bc.Instructions = append(bc.Instructions, Instruction{Op: op})
	case TernaryNode:
		if err := c.compileNode(n.Cond, bc); err != nil {
			return err
		}
		jmpFalseIdx := len(bc.Instructions)
		bc.Instructions = append(bc.Instructions, Instruction{Op: OpJmpFalse, Arg: 0})
		if err := c.compileNode(n.True, bc); err != nil {
			return err
		}
		jmpEndIdx := len(bc.Instructions)
		bc.Instructions = append(bc.Instructions, Instruction{Op: OpJmp, Arg: 0})
		bc.Instructions[jmpFalseIdx].Arg = len(bc.Instructions)
		if err := c.compileNode(n.False, bc); err != nil {
			return err
		}
		bc.Instructions[jmpEndIdx].Arg = len(bc.Instructions)
	case CallNode:
		for _, arg := range n.Args {
			if err := c.compileNode(arg, bc); err != nil {
				return err
			}
		}
		idx := c.addConst(n.Name)
		bc.Instructions = append(bc.Instructions, Instruction{Op: OpCall, Arg: idx})
		bc.Instructions = append(bc.Instructions, Instruction{Op: OpPush, Arg: idx}) // placeholder
	}
	return nil
}

func (c *Compiler) addConst(v interface{}) int {
	c.constants = append(c.constants, v)
	return len(c.constants) - 1
}

func (c *Compiler) addVar(name string) int {
	if idx, ok := c.variables[name]; ok {
		return idx
	}
	idx := len(c.variables)
	c.variables[name] = idx
	return idx
}

func binaryOpcode(op string) Opcode {
	switch op {
	case "+":
		return OpAdd
	case "-":
		return OpSub
	case "*":
		return OpMul
	case "/":
		return OpDiv
	case "%":
		return OpMod
	case "^":
		return OpPow
	case "==":
		return OpEq
	case "!=":
		return OpNotEq
	case "<":
		return OpLt
	case "<=":
		return OpLe
	case ">":
		return OpGt
	case ">=":
		return OpGe
	default:
		return OpAdd
	}
}

func unaryOpcode(op string) Opcode {
	switch op {
	case "-":
		return OpNeg
	case "!":
		return OpNot
	default:
		return OpNeg
	}
}

// ---------------------------------------------------------------------------
// VM — executes bytecode
// ---------------------------------------------------------------------------

// VM executes compiled bytecode.
type VM struct {
	bc    *Bytecode
	stack []interface{}
	sp    int // stack pointer (index of next free slot)
}

// NewVM creates a VM for the given bytecode.
func NewVM(bc *Bytecode) *VM {
	return &VM{
		bc:    bc,
		stack: make([]interface{}, 256),
		sp:    0,
	}
}

// Run executes the bytecode with the given variable bindings.
func (vm *VM) Run(vars map[string]interface{}) (interface{}, error) {
	ip := 0
	instrs := vm.bc.Instructions

	for ip < len(instrs) {
		instr := instrs[ip]

		switch instr.Op {
		case OpPush:
			vm.push(vm.bc.Constants[instr.Arg])
			ip++
		case OpLoad:
			name := vm.bc.Variables[instr.Arg]
			val, ok := vars[name]
			if !ok {
				return nil, fmt.Errorf("undefined variable %q", name)
			}
			vm.push(val)
			ip++
		case OpStore:
			// Not implemented for expression-only VM.
			ip++
		case OpAdd:
			b := vm.popFloat()
			a := vm.popFloat()
			vm.push(a + b)
			ip++
		case OpSub:
			b := vm.popFloat()
			a := vm.popFloat()
			vm.push(a - b)
			ip++
		case OpMul:
			b := vm.popFloat()
			a := vm.popFloat()
			vm.push(a * b)
			ip++
		case OpDiv:
			b := vm.popFloat()
			a := vm.popFloat()
			if b == 0 {
				return nil, fmt.Errorf("division by zero")
			}
			vm.push(a / b)
			ip++
		case OpMod:
			b := vm.popFloat()
			a := vm.popFloat()
			vm.push(math.Mod(a, b))
			ip++
		case OpPow:
			b := vm.popFloat()
			a := vm.popFloat()
			vm.push(math.Pow(a, b))
			ip++
		case OpNeg:
			vm.push(-vm.popFloat())
			ip++
		case OpNot:
			vm.push(!vm.popBool())
			ip++
		case OpEq:
			b := vm.pop()
			a := vm.pop()
			vm.push(equals(a, b))
			ip++
		case OpNotEq:
			b := vm.pop()
			a := vm.pop()
			vm.push(!equals(a, b))
			ip++
		case OpLt:
			b := vm.popFloat()
			a := vm.popFloat()
			vm.push(a < b)
			ip++
		case OpLe:
			b := vm.popFloat()
			a := vm.popFloat()
			vm.push(a <= b)
			ip++
		case OpGt:
			b := vm.popFloat()
			a := vm.popFloat()
			vm.push(a > b)
			ip++
		case OpGe:
			b := vm.popFloat()
			a := vm.popFloat()
			vm.push(a >= b)
			ip++
		case OpAnd, OpOr:
			// Short-circuit is handled by JmpFalse/Jmp.
			ip++
		case OpJmpFalse:
			if !vm.popBool() {
				ip = instr.Arg
			} else {
				ip++
			}
		case OpJmp:
			ip = instr.Arg
		case OpDup:
			if vm.sp > 0 {
				v := vm.stack[vm.sp-1]
				vm.push(v)
			}
			ip++
		case OpCall:
			funcName := vm.bc.Constants[instr.Arg].(string)
			result, err := vm.callBuiltin(funcName)
			if err != nil {
				return nil, err
			}
			vm.push(result)
			ip++
		case OpPop:
			vm.pop()
			ip++
		case OpRet:
			if vm.sp > 0 {
				return vm.stack[0], nil
			}
			return nil, nil
		default:
			return nil, fmt.Errorf("unknown opcode %s", instr.Op)
		}
	}

	if vm.sp > 0 {
		return vm.stack[0], nil
	}
	return nil, nil
}

func (vm *VM) push(v interface{}) {
	if vm.sp >= len(vm.stack) {
		vm.stack = append(vm.stack, make([]interface{}, len(vm.stack))...)
	}
	vm.stack[vm.sp] = v
	vm.sp++
}

func (vm *VM) pop() interface{} {
	if vm.sp == 0 {
		return nil
	}
	vm.sp--
	return vm.stack[vm.sp]
}

func (vm *VM) popFloat() float64 {
	v := vm.pop()
	return toFloat(v)
}

func (vm *VM) popBool() bool {
	v := vm.pop()
	if b, ok := v.(bool); ok {
		return b
	}
	return toFloat(v) != 0
}

func (vm *VM) callBuiltin(name string) (interface{}, error) {
	// Collect arguments from stack.
	// For simplicity, we support min/max/sqrt builtins.
	switch name {
	case "min":
		b := vm.popFloat()
		a := vm.popFloat()
		return math.Min(a, b), nil
	case "max":
		b := vm.popFloat()
		a := vm.popFloat()
		return math.Max(a, b), nil
	case "sqrt":
		a := vm.popFloat()
		return math.Sqrt(a), nil
	case "abs":
		a := vm.popFloat()
		return math.Abs(a), nil
	case "floor":
		a := vm.popFloat()
		return math.Floor(a), nil
	case "ceil":
		a := vm.popFloat()
		return math.Ceil(a), nil
	default:
		return nil, fmt.Errorf("unknown function %q", name)
	}
}

func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	default:
		return 0
	}
}

func equals(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	// Try numeric comparison.
	fa, aOk := toFloatCheck(a)
	fb, bOk := toFloatCheck(b)
	if aOk && bOk {
		return fa == fb
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func toFloatCheck(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// FormatBytecode — disassembly
// ---------------------------------------------------------------------------

// FormatBytecodeOptions controls disassembly output.
type FormatBytecodeOptions struct {
	ShowConstants bool
}

// DefaultFormatBytecodeOptions returns sensible defaults.
func DefaultFormatBytecodeOptions() FormatBytecodeOptions {
	return FormatBytecodeOptions{ShowConstants: true}
}

// FormatBytecode returns a human-readable disassembly of the bytecode.
func FormatBytecode(bc *Bytecode, opts FormatBytecodeOptions) string {
	var sb strings.Builder
	sb.WriteString("Bytecode:\n")
	sb.WriteString(fmt.Sprintf("  variables: %v\n", bc.Variables))

	if opts.ShowConstants {
		sb.WriteString("  constants:\n")
		for i, c := range bc.Constants {
			sb.WriteString(fmt.Sprintf("    [%d] %v\n", i, c))
		}
	}

	sb.WriteString("  instructions:\n")
	for i, instr := range bc.Instructions {
		arg := ""
		if instr.Op == OpPush || instr.Op == OpLoad || instr.Op == OpStore ||
			instr.Op == OpJmpFalse || instr.Op == OpJmp || instr.Op == OpCall {
			arg = fmt.Sprintf(" %d", instr.Arg)
		}
		sb.WriteString(fmt.Sprintf("    %04d: %s%s\n", i, instr.Op, arg))
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Evaluate — convenience function
// ---------------------------------------------------------------------------

// Evaluate compiles and runs an expression.
func Evaluate(input string, vars map[string]interface{}) (interface{}, error) {
	bc, err := Compile(input)
	if err != nil {
		return nil, err
	}
	vm := NewVM(bc)
	return vm.Run(vars)
}
