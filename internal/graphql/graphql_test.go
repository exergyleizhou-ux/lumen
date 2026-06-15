package graphql

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestNewSchema(t *testing.T) {
	s := NewSchema()
	if s.Query() == nil {
		t.Fatal("query nil")
	}
	if s.Mutation() == nil {
		t.Fatal("mutation nil")
	}
}

func TestAddObject(t *testing.T) {
	s := NewSchema()
	u := s.AddObject("User")
	u.AddField("name", StringType, FieldConfig{
		Resolve: func(ctx ResolveCtx) (interface{}, error) { return "test", nil },
	})
	if s.Type("User") == nil {
		t.Fatal("type not found")
	}
	if s.Type("User").Field("name") == nil {
		t.Fatal("field not found")
	}
}

func TestTypeRef(t *testing.T) {
	if StringType.String() != "String" {
		t.Fatal("bad string")
	}
	l := ListOf(StringType)
	if l.String() != "[String]" {
		t.Fatal("bad list")
	}
	n := NonNullOf(StringType)
	if n.String() != "String!" {
		t.Fatal("bad nonnull")
	}
	if !n.IsNonNull() {
		t.Fatal("not nonnull")
	}
	if n.UnwrapNonNull() != StringType {
		t.Fatal("unwrap fail")
	}
	if !l.IsList() {
		t.Fatal("not list")
	}
	if l.UnwrapList() != StringType {
		t.Fatal("unwrap list fail")
	}
	nnl := NonNullOf(ListOf(StringType))
	if nnl.Named() != "String" {
		t.Fatalf("named: %s", nnl.Named())
	}
}

func TestParseSimple(t *testing.T) {
	doc, err := NewQueryParser(`{ hello }`).Parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Operations) != 1 {
		t.Fatal("ops")
	}
	fs := doc.Operations[0].Selections[0].(*FieldSel)
	if fs.Name != "hello" {
		t.Fatalf("name: %s", fs.Name)
	}
}

func TestParseNested(t *testing.T) {
	doc, err := NewQueryParser(`{ user { name email } }`).Parse()
	if err != nil {
		t.Fatal(err)
	}
	fs := doc.Operations[0].Selections[0].(*FieldSel)
	if len(fs.Selections) != 2 {
		t.Fatalf("sub: %d", len(fs.Selections))
	}
}

func TestParseAlias(t *testing.T) {
	doc, err := NewQueryParser(`{ u: user }`).Parse()
	if err != nil {
		t.Fatal(err)
	}
	fs := doc.Operations[0].Selections[0].(*FieldSel)
	if fs.Alias != "u" || fs.Name != "user" {
		t.Fatalf("alias: %q name: %q", fs.Alias, fs.Name)
	}
}

func TestParseArgs(t *testing.T) {
	doc, err := NewQueryParser(`{ user(id: "42") { name } }`).Parse()
	if err != nil {
		t.Fatal(err)
	}
	fs := doc.Operations[0].Selections[0].(*FieldSel)
	if len(fs.Arguments) != 1 {
		t.Fatal("args")
	}
	if fs.Arguments[0].Name != "id" || fs.Arguments[0].Value.Str != "42" {
		t.Fatal("arg val")
	}
}

func TestParseVars(t *testing.T) {
	doc, err := NewQueryParser(`query get($id: ID!) { user(id: $id) { name } }`).Parse()
	if err != nil {
		t.Fatal(err)
	}
	op := doc.Operations[0]
	if op.Name != "get" {
		t.Fatalf("name: %s", op.Name)
	}
	if len(op.VariableDefs) != 1 {
		t.Fatal("vars")
	}
}

func TestParseFragment(t *testing.T) {
	doc, err := NewQueryParser(`
		fragment f on User { name }
		query { user { ...f } }
	`).Parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Fragments) != 1 {
		t.Fatal("frags")
	}
}

func TestParseInlineFrag(t *testing.T) {
	doc, err := NewQueryParser(`{ node { ... on User { name } } }`).Parse()
	if err != nil {
		t.Fatal(err)
	}
	fs := doc.Operations[0].Selections[0].(*FieldSel)
	fr := fs.Selections[0].(*InlineFrag)
	if fr.TypeCondition != "User" {
		t.Fatalf("tc: %s", fr.TypeCondition)
	}
}

func TestParseMutation(t *testing.T) {
	doc, err := NewQueryParser(`mutation { create(name: "A") { id } }`).Parse()
	if err != nil {
		t.Fatal(err)
	}
	if doc.Operations[0].Type != OpMutation {
		t.Fatal("not mutation")
	}
}

func TestParseEscapes(t *testing.T) {
	doc, err := NewQueryParser(`{ echo(m: "a\nb") }`).Parse()
	if err != nil {
		t.Fatal(err)
	}
	fs := doc.Operations[0].Selections[0].(*FieldSel)
	if fs.Arguments[0].Value.Str != "a\nb" {
		t.Fatalf("esc: %q", fs.Arguments[0].Value.Str)
	}
}

func TestParseNumbers(t *testing.T) {
	doc, err := NewQueryParser(`{ items(n: 10, f: 3.14) }`).Parse()
	if err != nil {
		t.Fatal(err)
	}
	fs := doc.Operations[0].Selections[0].(*FieldSel)
	if fs.Arguments[0].Value.Int != 10 {
		t.Fatalf("int: %d", fs.Arguments[0].Value.Int)
	}
	if fs.Arguments[1].Value.Float != 3.14 {
		t.Fatalf("float: %f", fs.Arguments[1].Value.Float)
	}
}

func TestParseBoolNull(t *testing.T) {
	doc, err := NewQueryParser(`{ f(a: true, b: false, c: null) }`).Parse()
	if err != nil {
		t.Fatal(err)
	}
	fs := doc.Operations[0].Selections[0].(*FieldSel)
	if !fs.Arguments[0].Value.Bool {
		t.Fatal("true")
	}
	if fs.Arguments[1].Value.Bool {
		t.Fatal("false")
	}
	if fs.Arguments[2].Value.Kind != ValNull {
		t.Fatal("null")
	}
}

func TestParseListObj(t *testing.T) {
	doc, err := NewQueryParser(`{ f(a: [1,2], b: {x: "y"}) }`).Parse()
	if err != nil {
		t.Fatal(err)
	}
	fs := doc.Operations[0].Selections[0].(*FieldSel)
	if len(fs.Arguments[0].Value.List) != 2 {
		t.Fatal("list")
	}
	if fs.Arguments[1].Value.Object["x"].Str != "y" {
		t.Fatal("obj")
	}
}

func TestParseDirectives(t *testing.T) {
	doc, err := NewQueryParser(`{ user { name @skip(if: true) } }`).Parse()
	if err != nil {
		t.Fatal(err)
	}
	fs := doc.Operations[0].Selections[0].(*FieldSel)
	nf := fs.Selections[0].(*FieldSel)
	if len(nf.Directives) != 1 {
		t.Fatal("dirs")
	}
}

func TestParseComments(t *testing.T) {
	doc, err := NewQueryParser("# comment\n{ hello }").Parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Operations) != 1 {
		t.Fatal("ops")
	}
}

func TestParseError(t *testing.T) {
	_, err := NewQueryParser(`{ `).Parse()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecSimple(t *testing.T) {
	s := NewSchema()
	s.Query().AddField("hello", StringType, FieldConfig{
		Resolve: func(ctx ResolveCtx) (interface{}, error) { return "world", nil },
	})
	exec := NewExecutor(s)
	exec.MaxParallelism = 1
	result := exec.Execute(ResolveCtx{}, `{ hello }`, nil)
	if len(result.Errors) > 0 {
		t.Fatalf("errors: %v", result.Errors)
	}
	var data map[string]interface{}
	json.Unmarshal(result.Data, &data)
	if data["hello"] != "world" {
		t.Fatalf("got %v", data["hello"])
	}
}

func TestExecNested(t *testing.T) {
	s := NewSchema()
	ut := s.AddObject("User")
	ut.AddField("name", StringType, FieldConfig{
		Resolve: func(ctx ResolveCtx) (interface{}, error) {
			if m, ok := ctx.ParentValue.(map[string]interface{}); ok {
				return m["name"], nil
			}
			return "Alice", nil
		},
	})
	ut.AddField("email", StringType, FieldConfig{
		Resolve: func(ctx ResolveCtx) (interface{}, error) { return "a@b.com", nil },
	})
	s.Query().AddField("user", NamedType("User"), FieldConfig{
		Resolve: func(ctx ResolveCtx) (interface{}, error) {
			return map[string]interface{}{"name": "Alice", "email": "a@b.com"}, nil
		},
	})
	exec := NewExecutor(s)
	exec.MaxParallelism = 1
	result := exec.Execute(ResolveCtx{}, `{ user { name email } }`, nil)
	if len(result.Errors) > 0 {
		t.Fatalf("errors: %v", result.Errors)
	}
	var data map[string]interface{}
	json.Unmarshal(result.Data, &data)
	user := data["user"].(map[string]interface{})
	if user["name"] != "Alice" {
		t.Fatalf("name: %v", user["name"])
	}
	if user["email"] != "a@b.com" {
		t.Fatalf("email: %v", user["email"])
	}
}

func TestExecArgs(t *testing.T) {
	s := NewSchema()
	s.Query().AddField("greet", StringType, FieldConfig{
		Args: []*InputValueDef{{Name: "name", Type: StringType}},
		Resolve: func(ctx ResolveCtx) (interface{}, error) {
			n, _ := ctx.Args["name"].(string)
			return "Hi " + n, nil
		},
	})
	exec := NewExecutor(s)
	exec.MaxParallelism = 1
	result := exec.Execute(ResolveCtx{}, `{ greet(name: "Bob") }`, nil)
	var data map[string]interface{}
	json.Unmarshal(result.Data, &data)
	if data["greet"] != "Hi Bob" {
		t.Fatalf("got %v", data["greet"])
	}
}

func TestExecVars(t *testing.T) {
	s := NewSchema()
	s.Query().AddField("user", StringType, FieldConfig{
		Args: []*InputValueDef{{Name: "id", Type: NonNullOf(IDType)}},
		Resolve: func(ctx ResolveCtx) (interface{}, error) {
			return "U:" + ctx.Args["id"].(string), nil
		},
	})
	exec := NewExecutor(s)
	exec.MaxParallelism = 1
	result := exec.Execute(ResolveCtx{},
		`query($uid: ID!) { user(id: $uid) }`,
		map[string]interface{}{"uid": "42"},
	)
	var data map[string]interface{}
	json.Unmarshal(result.Data, &data)
	if data["user"] != "U:42" {
		t.Fatalf("got %v", data["user"])
	}
}

func TestExecAlias(t *testing.T) {
	s := NewSchema()
	s.Query().AddField("name", StringType, FieldConfig{
		Resolve: func(ctx ResolveCtx) (interface{}, error) { return "Alice", nil },
	})
	exec := NewExecutor(s)
	exec.MaxParallelism = 1
	result := exec.Execute(ResolveCtx{}, `{ n: name }`, nil)
	var data map[string]interface{}
	json.Unmarshal(result.Data, &data)
	if data["n"] != "Alice" {
		t.Fatalf("got %v", data["n"])
	}
}

func TestExecError(t *testing.T) {
	s := NewSchema()
	s.Query().AddField("fail", StringType, FieldConfig{
		Resolve: func(ctx ResolveCtx) (interface{}, error) { return nil, fmt.Errorf("boom") },
	})
	exec := NewExecutor(s)
	exec.MaxParallelism = 1
	result := exec.Execute(ResolveCtx{}, `{ fail }`, nil)
	if len(result.Errors) == 0 {
		t.Fatal("expected errors")
	}
}

func TestExecUnknownField(t *testing.T) {
	s := NewSchema()
	exec := NewExecutor(s)
	exec.MaxParallelism = 1
	result := exec.Execute(ResolveCtx{}, `{ nope }`, nil)
	if len(result.Errors) == 0 {
		t.Fatal("expected error")
	}
}

func TestExecMutation(t *testing.T) {
	s := NewSchema()
	s.Mutation().AddField("set", StringType, FieldConfig{
		Args: []*InputValueDef{{Name: "v", Type: NonNullOf(StringType)}},
		Resolve: func(ctx ResolveCtx) (interface{}, error) { return ctx.Args["v"], nil },
	})
	exec := NewExecutor(s)
	exec.MaxParallelism = 1
	result := exec.Execute(ResolveCtx{},
		`mutation { set(v: "ok") }`, nil)
	var data map[string]interface{}
	json.Unmarshal(result.Data, &data)
	if data["set"] != "ok" {
		t.Fatalf("got %v", data["set"])
	}
}

func TestExecSkip(t *testing.T) {
	s := NewSchema()
	s.Query().AddField("a", StringType, FieldConfig{
		Resolve: func(ctx ResolveCtx) (interface{}, error) { return "A", nil },
	})
	s.Query().AddField("b", StringType, FieldConfig{
		Resolve: func(ctx ResolveCtx) (interface{}, error) { return "B", nil },
	})
	exec := NewExecutor(s)
	exec.MaxParallelism = 1
	result := exec.Execute(ResolveCtx{}, `{ a @skip(if: true) b }`, nil)
	var data map[string]interface{}
	json.Unmarshal(result.Data, &data)
	if _, ok := data["a"]; ok {
		t.Fatal("a should be skipped")
	}
	if data["b"] != "B" {
		t.Fatalf("b: %v", data["b"])
	}
}

func TestExecInclude(t *testing.T) {
	s := NewSchema()
	s.Query().AddField("a", StringType, FieldConfig{
		Resolve: func(ctx ResolveCtx) (interface{}, error) { return "A", nil },
	})
	exec := NewExecutor(s)
	exec.MaxParallelism = 1
	result := exec.Execute(ResolveCtx{}, `{ a @include(if: false) }`, nil)
	var data map[string]interface{}
	json.Unmarshal(result.Data, &data)
	if _, ok := data["a"]; ok {
		t.Fatal("a should not be included")
	}
}

func TestTypename(t *testing.T) {
	s := NewSchema()
	ut := s.AddObject("User")
	ut.AddField("name", StringType, FieldConfig{
		Resolve: func(ctx ResolveCtx) (interface{}, error) { return "Alice", nil },
	})
	RegisterIntrospection(s)
	s.Query().AddField("user", NamedType("User"), FieldConfig{
		Resolve: func(ctx ResolveCtx) (interface{}, error) {
			return map[string]interface{}{"name": "Alice"}, nil
		},
	})
	exec := NewExecutor(s)
	exec.MaxParallelism = 1
	result := exec.Execute(ResolveCtx{}, `{ user { __typename name } }`, nil)
	var data map[string]interface{}
	json.Unmarshal(result.Data, &data)
	user := data["user"].(map[string]interface{})
	if user["__typename"] != "User" {
		t.Fatalf("typename: %v", user["__typename"])
	}
}

func TestIntrospect(t *testing.T) {
	s := NewSchema()
	s.Query().AddField("hello", StringType, FieldConfig{})
	s.AddObject("User")
	intro := IntrospectSchema(s)
	sc := intro["__schema"].(map[string]interface{})
	types := sc["types"].([]map[string]interface{})
	if len(types) < 3 {
		t.Fatalf("types: %d", len(types))
	}
}

type testRes struct {
	f map[string]interface{}
}

func (r *testRes) ResolveField(name string, ctx ResolveCtx) (interface{}, error) {
	if v, ok := r.f[name]; ok {
		return v, nil
	}
	return nil, fmt.Errorf("no field %s", name)
}

func TestResolverIface(t *testing.T) {
	s := NewSchema()
	ut := s.AddObject("User")
	ut.AddField("name", StringType, FieldConfig{})
	ut.AddField("email", StringType, FieldConfig{})
	s.Query().AddField("user", NamedType("User"), FieldConfig{
		Resolve: func(ctx ResolveCtx) (interface{}, error) {
			return &testRes{f: map[string]interface{}{"name": "Alice", "email": "a@b.com"}}, nil
		},
	})
	exec := NewExecutor(s)
	exec.MaxParallelism = 1
	result := exec.Execute(ResolveCtx{}, `{ user { name email } }`, nil)
	var data map[string]interface{}
	json.Unmarshal(result.Data, &data)
	user := data["user"].(map[string]interface{})
	if user["name"] != "Alice" {
		t.Fatalf("name: %v", user["name"])
	}
}

func TestParallel(t *testing.T) {
	s := NewSchema()
	var mu sync.Mutex
	var order []string
	for i := 0; i < 4; i++ {
		name := fmt.Sprintf("f%d", i)
		s.Query().AddField(name, StringType, FieldConfig{
			Resolve: func(ctx ResolveCtx) (interface{}, error) {
				mu.Lock()
				order = append(order, ctx.Field.Name)
				mu.Unlock()
				return ctx.Field.Name, nil
			},
		})
	}
	exec := NewExecutor(s)
	exec.MaxParallelism = 4
	result := exec.Execute(ResolveCtx{}, `{ f0 f1 f2 f3 }`, nil)
	if len(result.Errors) > 0 {
		t.Fatalf("errors: %v", result.Errors)
	}
	var data map[string]interface{}
	json.Unmarshal(result.Data, &data)
	if len(data) != 4 {
		t.Fatalf("fields: %d", len(data))
	}
}

func TestScalarString(t *testing.T) {
	if ScalarString.String() != "String" {
		t.Fatal("bad")
	}
	if Scalar(99).String() != "Unknown" {
		t.Fatal("bad")
	}
}

func TestOpString(t *testing.T) {
	if OpQuery.String() != "query" {
		t.Fatal("bad")
	}
}

func TestExecErrorFmt(t *testing.T) {
	e := &ExecError{Message: "boom", Path: []string{"a", "b"}}
	s := e.Error()
	if !strings.Contains(s, "boom") || !strings.Contains(s, "a.b") {
		t.Fatal("bad fmt")
	}
}

func TestCoerce(t *testing.T) {
	v, err := CoerceValue(StringType, "hello")
	if err != nil || v != "hello" {
		t.Fatal("coerce string")
	}
	v, err = CoerceValue(IntType, float64(42))
	if err != nil || v != int64(42) {
		t.Fatal("coerce int")
	}
	v, err = CoerceValue(BooleanType, true)
	if err != nil || v != true {
		t.Fatal("coerce bool")
	}
}
