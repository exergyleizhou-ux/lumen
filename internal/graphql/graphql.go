// Package graphql implements a lightweight GraphQL engine with schema
// definition, query parsing, resolver registration, and parallel field
// resolution. It supports a practical subset of the GraphQL specification
// including fields, nested selections, arguments, aliases, variables,
// fragments, directives (@skip/@include), and __typename introspection.
//
// Usage:
//
//	schema := graphql.NewSchema()
//	userType := schema.AddObject("User")
//	userType.AddField("name", graphql.StringType, graphql.FieldConfig{
//	    Resolve: func(ctx graphql.ResolveCtx) (interface{}, error) {
//	        return "Alice", nil
//	    },
//	})
//	schema.Query().AddField("user", graphql.NamedType("User"), graphql.FieldConfig{
//	    Resolve: func(ctx graphql.ResolveCtx) (interface{}, error) {
//	        return map[string]interface{}{"name": "Alice"}, nil
//	    },
//	})
//	exec := graphql.NewExecutor(schema)
//	result := exec.Execute(ctx, `{ user { name } }`, nil)
package graphql

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Scalar types
// ---------------------------------------------------------------------------

// Scalar represents a built-in GraphQL scalar type.
type Scalar int

const (
	ScalarString Scalar = iota
	ScalarInt
	ScalarFloat
	ScalarBoolean
	ScalarID
)

var scalarNames = map[Scalar]string{
	ScalarString:  "String",
	ScalarInt:     "Int",
	ScalarFloat:   "Float",
	ScalarBoolean: "Boolean",
	ScalarID:      "ID",
}

// String returns the GraphQL name of the scalar.
func (s Scalar) String() string {
	if n, ok := scalarNames[s]; ok {
		return n
	}
	return "Unknown"
}

// ---------------------------------------------------------------------------
// TypeRef
// ---------------------------------------------------------------------------

// TypeKind classifies a type reference.
type TypeKind int

const (
	TypeKindScalar TypeKind = iota
	TypeKindObject
	TypeKindList
	TypeKindNonNull
	TypeKindEnum
	TypeKindInputObject
)

// TypeRef describes the type of a field, argument, or variable.
type TypeRef struct {
	Kind   TypeKind
	Name   string   // for Scalar, Object, Enum, InputObject
	Scalar Scalar   // for Scalar
	OfType *TypeRef // for List, NonNull
}

// NamedType creates a reference to a named type.
func NamedType(name string) *TypeRef {
	return &TypeRef{Kind: TypeKindObject, Name: name}
}

// ListOf wraps t in a list type.
func ListOf(t *TypeRef) *TypeRef { return &TypeRef{Kind: TypeKindList, OfType: t} }

// NonNullOf wraps t in a non-null type.
func NonNullOf(t *TypeRef) *TypeRef { return &TypeRef{Kind: TypeKindNonNull, OfType: t} }

// String returns a GraphQL-ish representation.
func (tr *TypeRef) String() string {
	switch tr.Kind {
	case TypeKindScalar:
		return tr.Scalar.String()
	case TypeKindObject, TypeKindEnum, TypeKindInputObject:
		return tr.Name
	case TypeKindList:
		return fmt.Sprintf("[%s]", tr.OfType)
	case TypeKindNonNull:
		return fmt.Sprintf("%s!", tr.OfType)
	default:
		return "?"
	}
}

// UnwrapNonNull returns the inner type if NonNull, else tr.
func (tr *TypeRef) UnwrapNonNull() *TypeRef {
	if tr.Kind == TypeKindNonNull {
		return tr.OfType
	}
	return tr
}

// UnwrapList returns the inner type if List (after unwrapping NonNull), else nil.
func (tr *TypeRef) UnwrapList() *TypeRef {
	t := tr.UnwrapNonNull()
	if t.Kind == TypeKindList {
		return t.OfType
	}
	return nil
}

// IsNonNull reports whether tr is NonNull.
func (tr *TypeRef) IsNonNull() bool { return tr.Kind == TypeKindNonNull }

// IsList reports whether tr is a List.
func (tr *TypeRef) IsList() bool {
	t := tr.UnwrapNonNull()
	return t.Kind == TypeKindList
}

// Named returns the root named type name.
func (tr *TypeRef) Named() string {
	t := tr
	for t != nil {
		switch t.Kind {
		case TypeKindObject, TypeKindScalar, TypeKindEnum, TypeKindInputObject:
			return t.Name
		}
		t = t.OfType
	}
	return ""
}

// Predefined scalar TypeRefs.
var (
	StringType  = &TypeRef{Kind: TypeKindScalar, Name: "String", Scalar: ScalarString}
	IntType     = &TypeRef{Kind: TypeKindScalar, Name: "Int", Scalar: ScalarInt}
	FloatType   = &TypeRef{Kind: TypeKindScalar, Name: "Float", Scalar: ScalarFloat}
	BooleanType = &TypeRef{Kind: TypeKindScalar, Name: "Boolean", Scalar: ScalarBoolean}
	IDType      = &TypeRef{Kind: TypeKindScalar, Name: "ID", Scalar: ScalarID}
)

// ---------------------------------------------------------------------------
// ObjectType and FieldDefinition
// ---------------------------------------------------------------------------

// ObjectType defines a GraphQL object type.
type ObjectType struct {
	Name        string
	Description string
	Fields      map[string]*FieldDefinition
	Interfaces  []string
}

// FieldDefinition describes a field on an object type.
type FieldDefinition struct {
	Name              string
	Description       string
	Type              *TypeRef
	Arguments         []*InputValueDef
	Resolve           ResolveFunc
	DeprecationReason string
}

// InputValueDef describes an argument.
type InputValueDef struct {
	Name         string
	Description  string
	Type         *TypeRef
	DefaultValue interface{}
}

// ResolveFunc resolves a field value.
type ResolveFunc func(ctx ResolveCtx) (interface{}, error)

// ResolveCtx provides context to field resolvers.
type ResolveCtx struct {
	ParentValue interface{}
	Args        map[string]interface{}
	Field       *FieldDefinition
	Path        string
	Variables   map[string]interface{}
	Schema      *Schema
}

// FieldConfig bundles options for adding a field.
type FieldConfig struct {
	Type              *TypeRef
	Description       string
	Args              []*InputValueDef
	Resolve           ResolveFunc
	DeprecationReason string
}

// AddField adds a field to an ObjectType.
func (ot *ObjectType) AddField(name string, typ *TypeRef, cfg FieldConfig) *FieldDefinition {
	fd := &FieldDefinition{
		Name:              name,
		Description:       cfg.Description,
		Type:              typ,
		Arguments:         cfg.Args,
		Resolve:           cfg.Resolve,
		DeprecationReason: cfg.DeprecationReason,
	}
	ot.Fields[name] = fd
	return fd
}

// Field returns the named field, or nil.
func (ot *ObjectType) Field(name string) *FieldDefinition { return ot.Fields[name] }

// ---------------------------------------------------------------------------
// Resolver interface
// ---------------------------------------------------------------------------

// Resolver can be implemented by parent values to resolve fields dynamically.
type Resolver interface {
	ResolveField(name string, ctx ResolveCtx) (interface{}, error)
}

// ---------------------------------------------------------------------------
// Schema
// ---------------------------------------------------------------------------

// Schema holds the complete GraphQL schema.
type Schema struct {
	mu               sync.RWMutex
	types            map[string]*ObjectType
	queryType        *ObjectType
	mutationType     *ObjectType
	directives       map[string]*DirectiveDef
	queryTypeName    string
	mutationTypeName string
}

// DirectiveDef describes a schema directive.
type DirectiveDef struct {
	Name      string
	Locations []string
	Args      []*InputValueDef
}

// NewSchema creates an empty schema with Query and Mutation types.
func NewSchema() *Schema {
	s := &Schema{
		types:      make(map[string]*ObjectType),
		directives: make(map[string]*DirectiveDef),
	}
	q := &ObjectType{Name: "Query", Fields: make(map[string]*FieldDefinition)}
	m := &ObjectType{Name: "Mutation", Fields: make(map[string]*FieldDefinition)}
	s.types["Query"] = q
	s.types["Mutation"] = m
	s.queryType = q
	s.mutationType = m
	s.queryTypeName = "Query"
	s.mutationTypeName = "Mutation"
	s.directives["include"] = &DirectiveDef{
		Name: "include", Locations: []string{"FIELD", "FRAGMENT_SPREAD", "INLINE_FRAGMENT"},
		Args: []*InputValueDef{{Name: "if", Type: NonNullOf(BooleanType)}},
	}
	s.directives["skip"] = &DirectiveDef{
		Name: "skip", Locations: []string{"FIELD", "FRAGMENT_SPREAD", "INLINE_FRAGMENT"},
		Args: []*InputValueDef{{Name: "if", Type: NonNullOf(BooleanType)}},
	}
	return s
}

// Query returns the Query root type.
func (s *Schema) Query() *ObjectType { return s.queryType }

// Mutation returns the Mutation root type.
func (s *Schema) Mutation() *ObjectType { return s.mutationType }

// AddObject registers a new ObjectType and returns it.
func (s *Schema) AddObject(name string) *ObjectType {
	t := &ObjectType{Name: name, Fields: make(map[string]*FieldDefinition)}
	s.mu.Lock()
	s.types[name] = t
	s.mu.Unlock()
	return t
}

// Type returns the named ObjectType or nil.
func (s *Schema) Type(name string) *ObjectType {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.types[name]
}

// Types returns all registered types.
func (s *Schema) Types() map[string]*ObjectType {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]*ObjectType, len(s.types))
	for k, v := range s.types {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// AST
// ---------------------------------------------------------------------------

// Document is the parsed query.
type Document struct {
	Operations []*OperationDef
	Fragments  map[string]*FragmentDef
}

// OpType is the operation type.
type OpType int

const (
	OpQuery OpType = iota
	OpMutation
	OpSubscription
)

func (ot OpType) String() string {
	switch ot {
	case OpQuery:
		return "query"
	case OpMutation:
		return "mutation"
	case OpSubscription:
		return "subscription"
	default:
		return "unknown"
	}
}

// OperationDef is a single operation.
type OperationDef struct {
	Type         OpType
	Name         string
	VariableDefs []*VariableDef
	Selections   []Selection
	Directives   []*Directive
}

// VariableDef defines a query variable.
type VariableDef struct {
	Name         string
	Type         *TypeRef
	DefaultValue interface{}
}

// Selection is a field, inline fragment, or fragment spread.
type Selection interface {
	isSelection()
	SelName() string
}

// FieldSel represents a field.
type FieldSel struct {
	Alias      string
	Name       string
	Arguments  []*Arg
	Selections []Selection
	Directives []*Directive
}

func (f *FieldSel) isSelection()    {}
func (f *FieldSel) SelName() string { return f.Name }

// InlineFrag is an inline fragment.
type InlineFrag struct {
	TypeCondition string
	Selections    []Selection
	Directives    []*Directive
}

func (f *InlineFrag) isSelection()    {}
func (f *InlineFrag) SelName() string { return "... on " + f.TypeCondition }

// FragSpread is a named fragment spread.
type FragSpread struct {
	Name       string
	Directives []*Directive
}

func (f *FragSpread) isSelection()    {}
func (f *FragSpread) SelName() string { return "..." + f.Name }

// FragmentDef is a named fragment.
type FragmentDef struct {
	Name          string
	TypeCondition string
	Selections    []Selection
}

// Arg is a field argument.
type Arg struct {
	Name  string
	Value Val
}

// Directive is a directive annotation.
type Directive struct {
	Name      string
	Arguments []*Arg
}

// ValKind classifies a literal value.
type ValKind int

const (
	ValString ValKind = iota
	ValInt
	ValFloat
	ValBoolean
	ValNull
	ValEnum
	ValList
	ValObject
	ValVariable
)

// Val represents a literal or variable value.
type Val struct {
	Kind   ValKind
	Str    string
	Int    int64
	Float  float64
	Bool   bool
	List   []Val
	Object map[string]Val
}

// ---------------------------------------------------------------------------
// QueryParser
// ---------------------------------------------------------------------------

// QueryParser parses GraphQL query strings.
type QueryParser struct {
	input string
	pos   int
	line  int
	col   int
}

// NewQueryParser creates a parser.
func NewQueryParser(input string) *QueryParser {
	return &QueryParser{input: input, pos: 0, line: 1, col: 1}
}

// Parse parses the input.
func (p *QueryParser) Parse() (*Document, error) {
	doc := &Document{
		Operations: make([]*OperationDef, 0),
		Fragments:  make(map[string]*FragmentDef),
	}
	p.skipWS()
	for p.pos < len(p.input) {
		if p.match("fragment") {
			frag, err := p.parseFragmentDef()
			if err != nil {
				return nil, err
			}
			doc.Fragments[frag.Name] = frag
		} else {
			op, err := p.parseOperation()
			if err != nil {
				return nil, err
			}
			doc.Operations = append(doc.Operations, op)
		}
		p.skipWS()
	}
	return doc, nil
}

func (p *QueryParser) parseOperation() (*OperationDef, error) {
	op := &OperationDef{Type: OpQuery}

	if p.match("query") {
		op.Type = OpQuery
	} else if p.match("mutation") {
		op.Type = OpMutation
	} else if p.match("subscription") {
		op.Type = OpSubscription
	}

	p.skipWS()
	if r, _ := p.peekRune(); isAlpha(r) || r == '_' {
		op.Name = p.parseName()
	}

	p.skipWS()
	if p.peek() == '(' {
		vds, err := p.parseVariableDefs()
		if err != nil {
			return nil, err
		}
		op.VariableDefs = vds
	}

	p.skipWS()
	op.Directives = p.parseDirectives()

	p.skipWS()
	sel, err := p.parseSelectionSet()
	if err != nil {
		return nil, err
	}
	op.Selections = sel
	return op, nil
}

func (p *QueryParser) parseSelectionSet() ([]Selection, error) {
	if p.peek() != '{' {
		return nil, p.errf("expected '{'")
	}
	p.advance()
	p.skipWS()

	var sels []Selection
	for p.peek() != '}' {
		if p.pos >= len(p.input) {
			return nil, p.errf("unexpected end of input")
		}
		sel, err := p.parseSelection()
		if err != nil {
			return nil, err
		}
		sels = append(sels, sel)
		p.skipWS()
	}
	p.advance()
	return sels, nil
}

func (p *QueryParser) parseSelection() (Selection, error) {
	// Check for "..." (spread operator) — handle specially since match()
	// requires a word boundary, but "..." can be followed immediately by a name.
	if p.peek3() == "..." {
		p.pos += 3
		p.col += 3
		p.skipWS()
		if p.match("on") {
			p.skipWS()
			tc := p.parseName()
			p.skipWS()
			dirs := p.parseDirectives()
			p.skipWS()
			sel, err := p.parseSelectionSet()
			if err != nil {
				return nil, err
			}
			return &InlineFrag{TypeCondition: tc, Selections: sel, Directives: dirs}, nil
		}
		name := p.parseName()
		dirs := p.parseDirectives()
		return &FragSpread{Name: name, Directives: dirs}, nil
	}

	alias := ""
	name := p.parseName()
	p.skipWS()
	if p.peek() == ':' {
		p.advance()
		p.skipWS()
		alias = name
		name = p.parseName()
		p.skipWS()
	}

	var args []*Arg
	if p.peek() == '(' {
		var err error
		args, err = p.parseArguments()
		if err != nil {
			return nil, err
		}
		p.skipWS()
	}

	dirs := p.parseDirectives()
	p.skipWS()

	var subSels []Selection
	if p.peek() == '{' {
		var err error
		subSels, err = p.parseSelectionSet()
		if err != nil {
			return nil, err
		}
	}

	return &FieldSel{Alias: alias, Name: name, Arguments: args, Selections: subSels, Directives: dirs}, nil
}

func (p *QueryParser) parseArguments() ([]*Arg, error) {
	if p.peek() != '(' {
		return nil, p.errf("expected '('")
	}
	p.advance()
	p.skipWS()

	var args []*Arg
	for p.peek() != ')' {
		name := p.parseName()
		p.skipWS()
		if p.peek() != ':' {
			return nil, p.errf("expected ':'")
		}
		p.advance()
		p.skipWS()
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		args = append(args, &Arg{Name: name, Value: val})
		p.skipWS()
	}
	p.advance()
	return args, nil
}

func (p *QueryParser) parseVariableDefs() ([]*VariableDef, error) {
	if p.peek() != '(' {
		return nil, p.errf("expected '('")
	}
	p.advance()
	p.skipWS()

	var defs []*VariableDef
	for p.peek() != ')' {
		if p.peek() != '$' {
			return nil, p.errf("expected '$'")
		}
		p.advance()
		name := p.parseName()
		p.skipWS()
		if p.peek() != ':' {
			return nil, p.errf("expected ':'")
		}
		p.advance()
		p.skipWS()
		typ, err := p.parseType()
		if err != nil {
			return nil, err
		}
		def := &VariableDef{Name: name, Type: typ}
		p.skipWS()
		if p.peek() == '=' {
			p.advance()
			p.skipWS()
			val, err := p.parseValue()
			if err != nil {
				return nil, err
			}
			def.DefaultValue = p.valToGo(val)
		}
		defs = append(defs, def)
		p.skipWS()
	}
	p.advance()
	return defs, nil
}

func (p *QueryParser) parseType() (*TypeRef, error) {
	var tr *TypeRef
	if p.peek() == '[' {
		p.advance()
		p.skipWS()
		inner, err := p.parseType()
		if err != nil {
			return nil, err
		}
		tr = ListOf(inner)
		p.skipWS()
		if p.peek() != ']' {
			return nil, p.errf("expected ']'")
		}
		p.advance()
	} else {
		name := p.parseName()
		switch name {
		case "String":
			tr = StringType
		case "Int":
			tr = IntType
		case "Float":
			tr = FloatType
		case "Boolean":
			tr = BooleanType
		case "ID":
			tr = IDType
		default:
			tr = NamedType(name)
		}
	}
	p.skipWS()
	if p.peek() == '!' {
		p.advance()
		tr = NonNullOf(tr)
	}
	return tr, nil
}

func (p *QueryParser) parseValue() (Val, error) {
	p.skipWS()
	switch {
	case p.peek() == '$':
		p.advance()
		return Val{Kind: ValVariable, Str: p.parseName()}, nil
	case p.peek() == '"':
		s, err := p.parseString()
		return Val{Kind: ValString, Str: s}, err
	case p.peek() == '-' || (p.peek() >= '0' && p.peek() <= '9'):
		return p.parseNumber()
	case p.match("true"):
		return Val{Kind: ValBoolean, Bool: true}, nil
	case p.match("false"):
		return Val{Kind: ValBoolean, Bool: false}, nil
	case p.match("null"):
		return Val{Kind: ValNull}, nil
	case p.peek() == '[':
		return p.parseListVal()
	case p.peek() == '{':
		return p.parseObjectVal()
	default:
		return Val{Kind: ValEnum, Str: p.parseName()}, nil
	}
}

func (p *QueryParser) parseString() (string, error) {
	if p.peek() != '"' {
		return "", p.errf("expected '\"'")
	}
	p.advance()
	var sb strings.Builder
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if ch == '"' {
			p.advance()
			return sb.String(), nil
		}
		if ch == '\\' {
			p.advance()
			if p.pos >= len(p.input) {
				return "", p.errf("unexpected end of string")
			}
			esc := p.input[p.pos]
			p.advance()
			switch esc {
			case '"', '\\', '/':
				sb.WriteByte(esc)
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			default:
				sb.WriteByte('\\')
				sb.WriteByte(esc)
			}
		} else if ch == '\n' {
			return "", p.errf("unexpected newline in string")
		} else {
			sb.WriteByte(ch)
			p.advance()
		}
	}
	return "", p.errf("unterminated string")
}

func (p *QueryParser) parseNumber() (Val, error) {
	start := p.pos
	if p.peek() == '-' {
		p.advance()
	}
	for p.pos < len(p.input) && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
		p.advance()
	}
	isFloat := false
	if p.pos < len(p.input) && p.input[p.pos] == '.' {
		isFloat = true
		p.advance()
		for p.pos < len(p.input) && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
			p.advance()
		}
	}
	if p.pos < len(p.input) && (p.input[p.pos] == 'e' || p.input[p.pos] == 'E') {
		isFloat = true
		p.advance()
		if p.pos < len(p.input) && (p.input[p.pos] == '+' || p.input[p.pos] == '-') {
			p.advance()
		}
		for p.pos < len(p.input) && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
			p.advance()
		}
	}
	s := p.input[start:p.pos]
	if isFloat {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return Val{}, p.errf("invalid float %q", s)
		}
		return Val{Kind: ValFloat, Float: f}, nil
	}
	iv, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return Val{}, p.errf("invalid int %q", s)
	}
	return Val{Kind: ValInt, Int: iv}, nil
}

func (p *QueryParser) parseListVal() (Val, error) {
	if p.peek() != '[' {
		return Val{}, p.errf("expected '['")
	}
	p.advance()
	p.skipWS()
	var vals []Val
	for p.peek() != ']' {
		v, err := p.parseValue()
		if err != nil {
			return Val{}, err
		}
		vals = append(vals, v)
		p.skipWS()
	}
	p.advance()
	return Val{Kind: ValList, List: vals}, nil
}

func (p *QueryParser) parseObjectVal() (Val, error) {
	if p.peek() != '{' {
		return Val{}, p.errf("expected '{'")
	}
	p.advance()
	p.skipWS()
	obj := make(map[string]Val)
	for p.peek() != '}' {
		name := p.parseName()
		p.skipWS()
		if p.peek() != ':' {
			return Val{}, p.errf("expected ':'")
		}
		p.advance()
		p.skipWS()
		v, err := p.parseValue()
		if err != nil {
			return Val{}, err
		}
		obj[name] = v
		p.skipWS()
	}
	p.advance()
	return Val{Kind: ValObject, Object: obj}, nil
}

func (p *QueryParser) parseFragmentDef() (*FragmentDef, error) {
	p.skipWS()
	name := p.parseName()
	p.skipWS()
	if !p.match("on") {
		return nil, p.errf("expected 'on'")
	}
	p.skipWS()
	tc := p.parseName()
	p.skipWS()
	sel, err := p.parseSelectionSet()
	if err != nil {
		return nil, err
	}
	return &FragmentDef{Name: name, TypeCondition: tc, Selections: sel}, nil
}

func (p *QueryParser) parseDirectives() []*Directive {
	var dirs []*Directive
	for p.peek() == '@' {
		p.advance()
		name := p.parseName()
		var args []*Arg
		p.skipWS()
		if p.peek() == '(' {
			args, _ = p.parseArguments()
		}
		dirs = append(dirs, &Directive{Name: name, Arguments: args})
		p.skipWS()
	}
	return dirs
}

func (p *QueryParser) valToGo(v Val) interface{} {
	switch v.Kind {
	case ValString:
		return v.Str
	case ValInt:
		return v.Int
	case ValFloat:
		return v.Float
	case ValBoolean:
		return v.Bool
	case ValNull:
		return nil
	case ValEnum:
		return v.Str
	case ValList:
		out := make([]interface{}, len(v.List))
		for i, e := range v.List {
			out[i] = p.valToGo(e)
		}
		return out
	case ValObject:
		out := make(map[string]interface{})
		for k, e := range v.Object {
			out[k] = p.valToGo(e)
		}
		return out
	default:
		return nil
	}
}

func (p *QueryParser) parseName() string {
	p.skipWS()
	start := p.pos
	for p.pos < len(p.input) {
		r, sz := utf8.DecodeRuneInString(p.input[p.pos:])
		if isAlphaNum(r) || r == '_' {
			p.pos += sz
		} else {
			break
		}
	}
	return p.input[start:p.pos]
}

// Parser helpers.
func (p *QueryParser) peek() byte {
	if p.pos >= len(p.input) {
		return 0
	}
	return p.input[p.pos]
}

func (p *QueryParser) peek3() string {
	if p.pos+3 > len(p.input) {
		return ""
	}
	return p.input[p.pos : p.pos+3]
}

func (p *QueryParser) peekRune() (rune, int) {
	if p.pos >= len(p.input) {
		return 0, 0
	}
	return utf8.DecodeRuneInString(p.input[p.pos:])
}

func (p *QueryParser) advance() {
	if p.pos >= len(p.input) {
		return
	}
	if p.input[p.pos] == '\n' {
		p.line++
		p.col = 1
	} else {
		p.col++
	}
	p.pos++
}

func (p *QueryParser) match(word string) bool {
	p.skipWS()
	if strings.HasPrefix(p.input[p.pos:], word) {
		end := p.pos + len(word)
		if end <= len(p.input) {
			if end < len(p.input) {
				r, _ := utf8.DecodeRuneInString(p.input[end:])
				if isAlphaNum(r) || r == '_' {
					return false
				}
			}
		}
		for i := 0; i < len(word); i++ {
			p.advance()
		}
		return true
	}
	return false
}

func (p *QueryParser) skipWS() {
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == ',' {
			p.advance()
		} else if ch == '#' {
			for p.pos < len(p.input) && p.input[p.pos] != '\n' {
				p.advance()
			}
		} else {
			break
		}
	}
}

func (p *QueryParser) errf(format string, args ...interface{}) error {
	return fmt.Errorf("graphql: %s at line %d col %d",
		fmt.Sprintf(format, args...), p.line, p.col)
}

func isAlpha(r rune) bool    { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }
func isAlphaNum(r rune) bool { return isAlpha(r) || (r >= '0' && r <= '9') }

// ---------------------------------------------------------------------------
// Executor
// ---------------------------------------------------------------------------

// ExecResult holds the result of executing a GraphQL operation.
type ExecResult struct {
	Data   json.RawMessage `json:"data,omitempty"`
	Errors []*ExecError    `json:"errors,omitempty"`
}

// ExecError describes an execution error.
type ExecError struct {
	Message    string                 `json:"message"`
	Path       []string               `json:"path,omitempty"`
	Extensions map[string]interface{} `json:"extensions,omitempty"`
}

func (e *ExecError) Error() string {
	if len(e.Path) > 0 {
		return fmt.Sprintf("%s (path: %s)", e.Message, strings.Join(e.Path, "."))
	}
	return e.Message
}

// Executor runs GraphQL operations against a schema.
type Executor struct {
	schema         *Schema
	MaxParallelism int
}

// NewExecutor creates an executor.
func NewExecutor(schema *Schema) *Executor {
	return &Executor{schema: schema}
}

// Execute runs a query and returns the result.
func (e *Executor) Execute(ctx ResolveCtx, query string, variables map[string]interface{}) *ExecResult {
	parser := NewQueryParser(query)
	doc, err := parser.Parse()
	if err != nil {
		return &ExecResult{Errors: []*ExecError{{Message: err.Error()}}}
	}
	if len(doc.Operations) == 0 {
		return &ExecResult{Errors: []*ExecError{{Message: "no operations"}}}
	}

	op := doc.Operations[0]
	for _, candidate := range doc.Operations {
		if candidate.Name != "" && candidate.Name == ctx.Field.Name {
			op = candidate
			break
		}
	}

	if variables != nil {
		if ctx.Variables == nil {
			ctx.Variables = make(map[string]interface{})
		}
		for _, vd := range op.VariableDefs {
			if val, ok := variables[vd.Name]; ok {
				ctx.Variables[vd.Name] = val
			} else if vd.DefaultValue != nil {
				ctx.Variables[vd.Name] = vd.DefaultValue
			}
		}
	}

	var rootType *ObjectType
	switch op.Type {
	case OpQuery:
		rootType = e.schema.Query()
	case OpMutation:
		rootType = e.schema.Mutation()
	default:
		rootType = e.schema.Query()
	}

	result, errs := e.executeSelectionSet(ctx, rootType, nil, op.Selections, doc.Fragments, "")
	data, _ := json.Marshal(result)
	return &ExecResult{Data: data, Errors: errs}
}

func (e *Executor) executeSelectionSet(
	ctx ResolveCtx,
	objType *ObjectType,
	parent interface{},
	selections []Selection,
	fragments map[string]*FragmentDef,
	pathPrefix string,
) (map[string]interface{}, []*ExecError) {
	result := make(map[string]interface{})
	var errs []*ExecError

	type fieldJob struct {
		key   string
		field *FieldSel
	}

	var jobs []fieldJob
	for _, sel := range selections {
		for _, exp := range e.expand(sel, objType, fragments, parent, ctx.Variables) {
			if fs, ok := exp.(*FieldSel); ok {
				key := fs.Alias
				if key == "" {
					key = fs.Name
				}
				if fs.Name == "__typename" {
					result[key] = objType.Name
					continue
				}
				jobs = append(jobs, fieldJob{key: key, field: fs})
			}
		}
	}

	if len(jobs) == 0 {
		return result, errs
	}

	if e.MaxParallelism <= 1 || len(jobs) == 1 {
		for _, j := range jobs {
			val, fErrs := e.resolveField(ctx, objType, parent, j.field, fragments, pathPrefix)
			result[j.key] = val
			errs = append(errs, fErrs...)
		}
	} else {
		type jobResult struct {
			key  string
			val  interface{}
			errs []*ExecError
		}
		parallel := e.MaxParallelism
		if parallel <= 0 || parallel > len(jobs) {
			parallel = len(jobs)
		}
		sem := make(chan struct{}, parallel)
		results := make(chan jobResult, len(jobs))

		for _, j := range jobs {
			sem <- struct{}{}
			go func(jb fieldJob) {
				defer func() { <-sem }()
				val, fErrs := e.resolveField(ctx, objType, parent, jb.field, fragments, pathPrefix)
				results <- jobResult{key: jb.key, val: val, errs: fErrs}
			}(j)
		}

		for i := 0; i < len(jobs); i++ {
			r := <-results
			result[r.key] = r.val
			errs = append(errs, r.errs...)
		}
	}

	return result, errs
}

func (e *Executor) resolveField(
	ctx ResolveCtx,
	objType *ObjectType,
	parent interface{},
	field *FieldSel,
	fragments map[string]*FragmentDef,
	pathPrefix string,
) (interface{}, []*ExecError) {
	fd := objType.Field(field.Name)
	if fd == nil {
		return nil, []*ExecError{{Message: fmt.Sprintf("cannot query field %q on type %q", field.Name, objType.Name)}}
	}

	args := make(map[string]interface{})
	for _, arg := range field.Arguments {
		args[arg.Name] = e.resolveArg(arg.Value, ctx.Variables)
	}

	if shouldSkip(field.Directives, ctx.Variables) {
		return nil, nil
	}

	rctx := ResolveCtx{
		ParentValue: parent,
		Args:        args,
		Field:       fd,
		Path:        joinPath(pathPrefix, field.Name),
		Variables:   ctx.Variables,
		Schema:      e.schema,
	}

	var resolved interface{}
	var err error

	if fd.Resolve != nil {
		resolved, err = fd.Resolve(rctx)
	} else if r, ok := parent.(Resolver); ok {
		resolved, err = r.ResolveField(field.Name, rctx)
	} else if parent == nil {
		resolved = nil
	} else if m, ok := parent.(map[string]interface{}); ok {
		resolved = m[field.Name]
	}

	if err != nil {
		return nil, []*ExecError{{
			Message: fmt.Sprintf("error resolving %q: %v", field.Name, err),
			Path:    strings.Split(rctx.Path, "."),
		}}
	}

	if len(field.Selections) > 0 && resolved != nil {
		childType := e.resolveType(fd.Type)
		if childType != nil {
			subResult, subErrs := e.executeSelectionSet(
				rctx, childType, resolved, field.Selections, fragments, rctx.Path)
			if len(subErrs) > 0 {
				return subResult, subErrs
			}
			return subResult, nil
		}
	}

	return resolved, nil
}

func (e *Executor) resolveArg(v Val, variables map[string]interface{}) interface{} {
	switch v.Kind {
	case ValVariable:
		if variables != nil {
			if val, ok := variables[v.Str]; ok {
				return val
			}
		}
		return nil
	case ValString:
		return v.Str
	case ValInt:
		return v.Int
	case ValFloat:
		return v.Float
	case ValBoolean:
		return v.Bool
	case ValNull:
		return nil
	case ValEnum:
		return v.Str
	case ValList:
		out := make([]interface{}, len(v.List))
		for i, ev := range v.List {
			out[i] = e.resolveArg(ev, variables)
		}
		return out
	case ValObject:
		out := make(map[string]interface{})
		for k, ev := range v.Object {
			out[k] = e.resolveArg(ev, variables)
		}
		return out
	}
	return nil
}

func (e *Executor) resolveType(tr *TypeRef) *ObjectType {
	name := tr.Named()
	if name != "" {
		return e.schema.Type(name)
	}
	return nil
}

func (e *Executor) expand(sel Selection, objType *ObjectType, fragments map[string]*FragmentDef, parent interface{}, vars map[string]interface{}) []Selection {
	switch s := sel.(type) {
	case *FieldSel:
		if shouldSkip(s.Directives, vars) {
			return nil
		}
		return []Selection{s}
	case *FragSpread:
		if shouldSkip(s.Directives, vars) {
			return nil
		}
		frag, ok := fragments[s.Name]
		if !ok {
			return nil
		}
		if objType.Name != frag.TypeCondition {
			return nil
		}
		var out []Selection
		for _, inner := range frag.Selections {
			out = append(out, e.expand(inner, objType, fragments, parent, vars)...)
		}
		return out
	case *InlineFrag:
		if shouldSkip(s.Directives, vars) {
			return nil
		}
		if s.TypeCondition != "" && objType.Name != s.TypeCondition {
			return nil
		}
		var out []Selection
		for _, inner := range s.Selections {
			out = append(out, e.expand(inner, objType, fragments, parent, vars)...)
		}
		return out
	}
	return []Selection{sel}
}

func shouldSkip(dirs []*Directive, vars map[string]interface{}) bool {
	for _, d := range dirs {
		switch d.Name {
		case "skip":
			if getBool(d, "if", vars) {
				return true
			}
		case "include":
			if !getBool(d, "if", vars) {
				return true
			}
		}
	}
	return false
}

func getBool(d *Directive, name string, vars map[string]interface{}) bool {
	for _, a := range d.Arguments {
		if a.Name == name {
			switch a.Value.Kind {
			case ValBoolean:
				return a.Value.Bool
			case ValVariable:
				if v, ok := vars[a.Value.Str]; ok {
					if b, ok := v.(bool); ok {
						return b
					}
				}
			}
		}
	}
	return false
}

func joinPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// ---------------------------------------------------------------------------
// Introspection
// ---------------------------------------------------------------------------

// IntrospectSchema builds a minimal introspection result.
func IntrospectSchema(s *Schema) map[string]interface{} {
	types := make([]map[string]interface{}, 0)
	for _, t := range s.Types() {
		ti := map[string]interface{}{
			"kind": "OBJECT",
			"name": t.Name,
		}
		if t.Description != "" {
			ti["description"] = t.Description
		}
		fields := make([]map[string]interface{}, 0, len(t.Fields))
		for _, f := range t.Fields {
			fi := map[string]interface{}{
				"name": f.Name,
				"type": introspectType(f.Type),
			}
			if f.Description != "" {
				fi["description"] = f.Description
			}
			if f.DeprecationReason != "" {
				fi["isDeprecated"] = true
				fi["deprecationReason"] = f.DeprecationReason
			}
			if len(f.Arguments) > 0 {
				args := make([]map[string]interface{}, 0, len(f.Arguments))
				for _, a := range f.Arguments {
					args = append(args, map[string]interface{}{
						"name": a.Name,
						"type": introspectType(a.Type),
					})
				}
				fi["args"] = args
			}
			fields = append(fields, fi)
		}
		ti["fields"] = fields
		types = append(types, ti)
	}

	return map[string]interface{}{
		"__schema": map[string]interface{}{
			"queryType":    map[string]interface{}{"name": s.queryTypeName},
			"mutationType": map[string]interface{}{"name": s.mutationTypeName},
			"types":        types,
		},
	}
}

func introspectType(tr *TypeRef) map[string]interface{} {
	switch tr.Kind {
	case TypeKindScalar:
		return map[string]interface{}{"kind": "SCALAR", "name": tr.Name}
	case TypeKindObject:
		return map[string]interface{}{"kind": "OBJECT", "name": tr.Name}
	case TypeKindList:
		return map[string]interface{}{"kind": "LIST", "ofType": introspectType(tr.OfType)}
	case TypeKindNonNull:
		return map[string]interface{}{"kind": "NON_NULL", "ofType": introspectType(tr.OfType)}
	default:
		return map[string]interface{}{"kind": "UNKNOWN"}
	}
}

// RegisterIntrospection adds __typename field to every type.
func RegisterIntrospection(s *Schema) {
	for _, t := range s.Types() {
		if _, ok := t.Fields["__typename"]; !ok {
			t.Fields["__typename"] = &FieldDefinition{
				Name: "__typename",
				Type: NonNullOf(StringType),
				Resolve: func(ctx ResolveCtx) (interface{}, error) {
					return t.Name, nil
				},
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

// CoerceValue attempts to coerce a Go value to match a TypeRef.
func CoerceValue(tr *TypeRef, val interface{}) (interface{}, error) {
	if val == nil {
		if tr.IsNonNull() {
			return nil, fmt.Errorf("null for non-null type")
		}
		return nil, nil
	}
	inner := tr.UnwrapNonNull()
	switch inner.Kind {
	case TypeKindScalar:
		return coerceScalar(inner.Scalar, val)
	case TypeKindList:
		return coerceList(inner, val)
	default:
		return val, nil
	}
}

func coerceScalar(s Scalar, val interface{}) (interface{}, error) {
	switch s {
	case ScalarString:
		switch v := val.(type) {
		case string:
			return v, nil
		case fmt.Stringer:
			return v.String(), nil
		default:
			return fmt.Sprintf("%v", v), nil
		}
	case ScalarInt:
		switch v := val.(type) {
		case int:
			return int64(v), nil
		case int32:
			return int64(v), nil
		case int64:
			return v, nil
		case float64:
			if v == math.Trunc(v) {
				return int64(v), nil
			}
			return nil, fmt.Errorf("float %v is not an integer", v)
		case json.Number:
			iv, err := v.Int64()
			if err != nil {
				return nil, err
			}
			return iv, nil
		default:
			return nil, fmt.Errorf("cannot coerce %T to Int", val)
		}
	case ScalarFloat:
		switch v := val.(type) {
		case float64:
			return v, nil
		case float32:
			return float64(v), nil
		case int:
			return float64(v), nil
		case int64:
			return float64(v), nil
		case json.Number:
			return v.Float64()
		default:
			return nil, fmt.Errorf("cannot coerce %T to Float", val)
		}
	case ScalarBoolean:
		switch v := val.(type) {
		case bool:
			return v, nil
		default:
			return nil, fmt.Errorf("cannot coerce %T to Boolean", val)
		}
	case ScalarID:
		switch v := val.(type) {
		case string:
			return v, nil
		case int, int32, int64:
			return fmt.Sprintf("%v", v), nil
		case fmt.Stringer:
			return v.String(), nil
		default:
			return fmt.Sprintf("%v", v), nil
		}
	}
	return val, nil
}

func coerceList(tr *TypeRef, val interface{}) (interface{}, error) {
	inner := tr.UnwrapList()
	if inner == nil {
		return val, nil
	}

	// If it's already a slice, coerce each element.
	switch v := val.(type) {
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, elem := range v {
			c, err := CoerceValue(inner, elem)
			if err != nil {
				return nil, err
			}
			out[i] = c
		}
		return out, nil
	default:
		// Wrap single value in a list.
		c, err := CoerceValue(inner, v)
		if err != nil {
			return nil, err
		}
		return []interface{}{c}, nil
	}
}
