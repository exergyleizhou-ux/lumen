package swizzle

import (
	"encoding/json"
	"testing"
)

func TestSwizzler_Rename(t *testing.T) {
	s, err := New([]FieldSpec{
		{From: "old_name", To: "new_name"},
		{From: "age", To: "years", Type: "int"},
	}, false)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	input := json.RawMessage(`{"old_name":"Alice","age":"30"}`)
	output, err := s.Swizzle(input)
	if err != nil {
		t.Fatalf("swizzle: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal(output, &result)
	if result["new_name"] != "Alice" {
		t.Fatalf("expected new_name='Alice', got %v", result["new_name"])
	}
	if _, ok := result["old_name"]; ok {
		t.Fatal("expected old_name to be removed")
	}
	if result["years"] != int64(30) {
		t.Fatalf("expected years=30, got %v (%T)", result["years"], result["years"])
	}
}

func TestSwizzler_DefaultFill(t *testing.T) {
	s, err := New([]FieldSpec{
		{From: "name", To: "name"},
		{From: "role", To: "role", Default: "viewer"},
	}, false)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	input := json.RawMessage(`{"name":"Bob"}`)
	output, err := s.Swizzle(input)
	if err != nil {
		t.Fatalf("swizzle: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal(output, &result)
	if result["role"] != "viewer" {
		t.Fatalf("expected role='viewer', got %v", result["role"])
	}
}

func TestSwizzler_NestedPath(t *testing.T) {
	s, err := New([]FieldSpec{
		{From: "user.profile.email", To: "email"},
		{From: "user.name", To: "display_name", Transform: "uppercase"},
	}, false)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	input := json.RawMessage(`{"user":{"name":"alice","profile":{"email":"alice@example.com"}}}`)
	output, err := s.Swizzle(input)
	if err != nil {
		t.Fatalf("swizzle: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal(output, &result)
	if result["email"] != "alice@example.com" {
		t.Fatalf("expected email, got %v", result["email"])
	}
	if result["display_name"] != "ALICE" {
		t.Fatalf("expected ALICE, got %v", result["display_name"])
	}
}

func TestFlatten(t *testing.T) {
	input := json.RawMessage(`{"a":{"b":1,"c":[2,3]},"d":"hello"}`)
	flat, err := Flatten(input)
	if err != nil {
		t.Fatalf("flatten: %v", err)
	}
	if flat["a.b"] != float64(1) { // JSON numbers are float64.
		t.Fatalf("expected a.b=1, got %v", flat["a.b"])
	}
	if flat["d"] != "hello" {
		t.Fatalf("expected d='hello', got %v", flat["d"])
	}
}

func TestUnflatten(t *testing.T) {
	flat := map[string]interface{}{
		"a.b":   42,
		"a.c":   "hello",
		"x[0]":  1,
		"x[1]":  2,
	}
	output, err := Unflatten(flat)
	if err != nil {
		t.Fatalf("unflatten: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal(output, &result)

	a, ok := result["a"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'a' to be object, got %T", result["a"])
	}
	if a["b"] != float64(42) {
		t.Fatalf("expected a.b=42, got %v", a["b"])
	}
}

func TestRename(t *testing.T) {
	input := json.RawMessage(`{"foo":1,"bar":2}`)
	output, err := Rename(input, map[string]string{"foo": "baz"})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal(output, &result)
	if _, ok := result["foo"]; ok {
		t.Fatal("expected 'foo' to be removed")
	}
	if result["baz"] != float64(1) {
		t.Fatalf("expected baz=1, got %v", result["baz"])
	}
	if result["bar"] != float64(2) {
		t.Fatalf("expected bar=2, got %v", result["bar"])
	}
}

func TestFillDefaults(t *testing.T) {
	input := json.RawMessage(`{"x":10}`)
	output, err := FillDefaults(input, map[string]interface{}{"x": 0, "y": "default", "z": true})
	if err != nil {
		t.Fatalf("fill: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal(output, &result)
	if result["x"] != float64(10) {
		t.Fatalf("expected x=10 (not overwritten), got %v", result["x"])
	}
	if result["y"] != "default" {
		t.Fatalf("expected y='default', got %v", result["y"])
	}
}

func TestExtractRegex(t *testing.T) {
	input := json.RawMessage(`{"email":"alice@example.com"}`)
	results, err := ExtractRegex(input, "email", `(.+)@(.+)`)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 match, got %d", len(results))
	}
	if results[0]["$1"] != "alice" {
		t.Fatalf("expected $1=alice, got %v", results[0]["$1"])
	}
	if results[0]["$2"] != "example.com" {
		t.Fatalf("expected $2=example.com, got %v", results[0]["$2"])
	}
}

func TestSwizzler_Transform(t *testing.T) {
	s, err := New([]FieldSpec{
		{From: "title", To: "slug", Transform: "slug"},
		{From: "title", To: "upper", Transform: "uppercase"},
		{From: "desc", To: "trimmed", Transform: "trim"},
	}, false)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	input := json.RawMessage(`{"title":"Hello World!","desc":"  padded  "}`)
	output, err := s.Swizzle(input)
	if err != nil {
		t.Fatalf("swizzle: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal(output, &result)
	if result["slug"] != "hello-world" {
		t.Fatalf("expected slug='hello-world', got %v", result["slug"])
	}
	if result["upper"] != "HELLO WORLD!" {
		t.Fatalf("expected upper='HELLO WORLD!', got %v", result["upper"])
	}
	if result["trimmed"] != "padded" {
		t.Fatalf("expected trimmed='padded', got %v", result["trimmed"])
	}
}

func TestFormatSpecs(t *testing.T) {
	s, _ := New([]FieldSpec{
		{From: "a", To: "b", Type: "int"},
	}, false)

	out := s.FormatSpecs()
	if out == "" {
		t.Fatal("expected non-empty format")
	}
}
