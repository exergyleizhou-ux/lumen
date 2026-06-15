package mold

import (
	"strings"
	"testing"
)

// ---- Test structs ----

type User struct {
	Name  string `validate:"required,minlen=2,maxlen=50"`
	Email string `validate:"required,email"`
	Age   int    `validate:"min=0,max=150"`
	Bio   string `validate:"maxlen=500"`
	Role  string `validate:"oneof=admin|user|guest"`
	URL   string `validate:"url"`
}

type Address struct {
	Street string `validate:"required"`
	City   string `validate:"required"`
	Zip    string `validate:"pattern=^\\d{5}$"`
}

type Person struct {
	Name    string  `validate:"required"`
	Address Address `validate:"required"`
}

type SkipField struct {
	Public  string `validate:"required"`
	Private string `validate:"-"`
}

// ---- Tests ----

func TestNew(t *testing.T) {
	m := New()
	if m == nil {
		t.Fatal("New returned nil")
	}
	if m.tagName != "validate" {
		t.Errorf("expected tagName 'validate', got '%s'", m.tagName)
	}
}

func TestValidate_Required(t *testing.T) {
	type TestStruct struct {
		Name string `validate:"required"`
	}
	errs := Validate(TestStruct{})
	if !errs.HasErrors() {
		t.Error("expected validation error for missing required field")
	}
	if len(errs) != 1 {
		t.Errorf("expected 1 error, got %d", len(errs))
	}
}

func TestValidate_Required_Present(t *testing.T) {
	type TestStruct struct {
		Name string `validate:"required"`
	}
	errs := Validate(TestStruct{Name: "John"})
	if errs.HasErrors() {
		t.Error("expected no errors when required field is present")
	}
}

func TestValidate_Email(t *testing.T) {
	type TestStruct struct {
		Email string `validate:"email"`
	}
	tests := []struct {
		email string
		valid bool
	}{
		{"test@example.com", true},
		{"invalid", false},
		{"a@b.co", true},
	}
	for _, tt := range tests {
		errs := Validate(TestStruct{Email: tt.email})
		if tt.valid && errs.HasErrors() {
			t.Errorf("email %q should be valid, got: %v", tt.email, errs)
		}
		if !tt.valid && !errs.HasErrors() {
			t.Errorf("email %q should be invalid", tt.email)
		}
	}
}

func TestValidate_MinMax(t *testing.T) {
	type TestStruct struct {
		Age int `validate:"min=18,max=100"`
	}
	tests := []struct {
		age   int
		valid bool
	}{
		{18, true},
		{50, true},
		{100, true},
		{17, false},
		{101, false},
	}
	for _, tt := range tests {
		errs := Validate(TestStruct{Age: tt.age})
		if tt.valid && errs.HasErrors() {
			t.Errorf("age %d should be valid", tt.age)
		}
		if !tt.valid && !errs.HasErrors() {
			t.Errorf("age %d should be invalid", tt.age)
		}
	}
}

func TestValidate_MinLenMaxLen(t *testing.T) {
	type TestStruct struct {
		Code string `validate:"minlen=3,maxlen=6"`
	}
	tests := []struct {
		code  string
		valid bool
	}{
		{"abc", true},
		{"abcdef", true},
		{"ab", false},
		{"abcdefg", false},
	}
	for _, tt := range tests {
		errs := Validate(TestStruct{Code: tt.code})
		if tt.valid && errs.HasErrors() {
			t.Errorf("code %q should be valid", tt.code)
		}
		if !tt.valid && !errs.HasErrors() {
			t.Errorf("code %q should be invalid", tt.code)
		}
	}
}

func TestValidate_Len(t *testing.T) {
	type TestStruct struct {
		Token string `validate:"len=8"`
	}
	errs := Validate(TestStruct{Token: "12345678"})
	if errs.HasErrors() {
		t.Error("exact length 8 should be valid")
	}
	errs = Validate(TestStruct{Token: "123"})
	if !errs.HasErrors() {
		t.Error("length 3 should be invalid for len=8")
	}
}

func TestValidate_OneOf(t *testing.T) {
	type TestStruct struct {
		Color string `validate:"oneof=red|green|blue"`
	}
	if Validate(TestStruct{Color: "red"}).HasErrors() {
		t.Error("'red' should be valid")
	}
	if Validate(TestStruct{Color: "yellow"}).HasErrors() == false {
		t.Error("'yellow' should be invalid")
	}
}

func TestValidate_URL(t *testing.T) {
	type TestStruct struct {
		Link string `validate:"url"`
	}
	if Validate(TestStruct{Link: "https://example.com"}).HasErrors() {
		t.Error("valid URL should pass")
	}
	if !Validate(TestStruct{Link: "not-a-url"}).HasErrors() {
		t.Error("invalid URL should fail")
	}
}

func TestValidate_Pattern(t *testing.T) {
	type TestStruct struct {
		Code string `validate:"pattern=^[A-Z]{3}-\\d{3}$"`
	}
	if Validate(TestStruct{Code: "ABC-123"}).HasErrors() {
		t.Error("valid pattern should pass")
	}
	if !Validate(TestStruct{Code: "abc-123"}).HasErrors() {
		t.Error("invalid pattern should fail")
	}
}

func TestValidate_Alpha(t *testing.T) {
	type TestStruct struct {
		Word string `validate:"alpha"`
	}
	if Validate(TestStruct{Word: "Hello"}).HasErrors() {
		t.Error("alpha-only should pass")
	}
	if !Validate(TestStruct{Word: "Hello123"}).HasErrors() {
		t.Error("non-alpha should fail")
	}
}

func TestValidate_Alphanum(t *testing.T) {
	type TestStruct struct {
		Word string `validate:"alphanum"`
	}
	if Validate(TestStruct{Word: "Hello123"}).HasErrors() {
		t.Error("alphanumeric should pass")
	}
	if !Validate(TestStruct{Word: "Hello 123"}).HasErrors() {
		t.Error("spaces should fail")
	}
}

func TestValidate_Numeric(t *testing.T) {
	type TestStruct struct {
		Val string `validate:"numeric"`
	}
	if Validate(TestStruct{Val: "12345"}).HasErrors() {
		t.Error("numeric string should pass")
	}
	if !Validate(TestStruct{Val: "12a45"}).HasErrors() {
		t.Error("non-numeric should fail")
	}
}

func TestValidate_GtGteLtLte(t *testing.T) {
	type TestStruct struct {
		Val int `validate:"gt=10,lt=20"`
	}
	if Validate(TestStruct{Val: 15}).HasErrors() {
		t.Error("15 should be valid (10 < 15 < 20)")
	}
	if !Validate(TestStruct{Val: 10}).HasErrors() {
		t.Error("10 should fail (not > 10)")
	}
	if !Validate(TestStruct{Val: 20}).HasErrors() {
		t.Error("20 should fail (not < 20)")
	}
}

func TestValidate_StartsWithEndsWith(t *testing.T) {
	type TestStruct struct {
		Value string `validate:"startswith=pre,contains=middle,endswith=post"`
	}
	if Validate(TestStruct{Value: "pre-middle-post"}).HasErrors() {
		t.Error("valid string should pass")
	}
	if !Validate(TestStruct{Value: "nope"}).HasErrors() {
		t.Error("invalid string should fail")
	}
}

func TestValidate_NestedStruct(t *testing.T) {
	p := Person{
		Name: "Alice",
		Address: Address{
			Street: "123 Main St",
			City:   "Springfield",
			Zip:    "12345",
		},
	}
	if Validate(p).HasErrors() {
		t.Error("valid nested struct should pass")
	}

	p2 := Person{Name: ""} // missing name and address fields
	errs := Validate(p2)
	if !errs.HasErrors() {
		t.Error("invalid nested struct should fail")
	}
}

func TestValidate_SkipField(t *testing.T) {
	s := SkipField{Public: "", Private: ""}
	errs := Validate(s)
	if !errs.HasErrors() {
		t.Error("required Public should fail")
	}
	// Private should be skipped
	for _, e := range errs {
		if e.Field == "Private" {
			t.Error("Private field should be skipped")
		}
	}
}

func TestValidationErrors_ByField(t *testing.T) {
	type S struct {
		A string `validate:"required"`
		B string `validate:"required"`
	}
	errs := Validate(S{})
	aErrs := errs.ByField("A")
	if len(aErrs) != 1 {
		t.Errorf("expected 1 error for A, got %d", len(aErrs))
	}
}

func TestValidationErrors_Fields(t *testing.T) {
	type S struct {
		A string `validate:"required"`
		B string `validate:"required"`
	}
	errs := Validate(S{})
	fields := errs.Fields()
	if len(fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(fields))
	}
}

func TestValidationErrors_Error(t *testing.T) {
	type S struct {
		A string `validate:"required"`
	}
	errs := Validate(S{})
	msg := errs.Error()
	if !strings.Contains(msg, "A") {
		t.Errorf("error message should contain field name: %s", msg)
	}
}

func TestParseTag(t *testing.T) {
	tag := "required,min=3,max=10"
	rules := ParseTag(tag)
	if len(rules) != 3 {
		t.Errorf("expected 3 rules, got %d", len(rules))
	}
	if rules[0].Name != "required" || rules[0].Param != "" {
		t.Errorf("unexpected rule: %+v", rules[0])
	}
	if rules[1].Name != "min" || rules[1].Param != "3" {
		t.Errorf("unexpected rule: %+v", rules[1])
	}
	if rules[2].Name != "max" || rules[2].Param != "10" {
		t.Errorf("unexpected rule: %+v", rules[2])
	}
}

func TestParseTag_Empty(t *testing.T) {
	rules := ParseTag("")
	if rules != nil {
		t.Errorf("expected nil for empty tag, got %v", rules)
	}
}

func TestSetLocale(t *testing.T) {
	m := New()
	m.SetLocale("es")
	m.SetMessages("es", map[string]string{
		"required": "{field} es obligatorio",
	})

	type S struct {
		Name string `validate:"required"`
	}
	errs := m.Validate(S{})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if !strings.Contains(errs[0].Message, "obligatorio") {
		t.Errorf("expected Spanish message, got: %s", errs[0].Message)
	}
}

func TestSetTagName(t *testing.T) {
	m := New()
	m.SetTagName("check")

	type S struct {
		Name string `check:"required"`
	}
	errs := m.Validate(S{})
	if !errs.HasErrors() {
		t.Error("should validate with custom tag name")
	}
}

func TestCustomValidator(t *testing.T) {
	m := New()
	m.Register("ispalindrome", func(value interface{}, _ string) bool {
		s, ok := value.(string)
		if !ok {
			return false
		}
		for i := 0; i < len(s)/2; i++ {
			if s[i] != s[len(s)-1-i] {
				return false
			}
		}
		return true
	})

	type S struct {
		Word string `validate:"ispalindrome"`
	}
	if m.Validate(S{Word: "racecar"}).HasErrors() {
		t.Error("'racecar' should be a valid palindrome")
	}
	if !m.Validate(S{Word: "hello"}).HasErrors() {
		t.Error("'hello' should not be a palindrome")
	}
}

func TestMold_TrimStrings(t *testing.T) {
	type S struct {
		Name string
	}
	s := S{Name: "  John  "}
	err := Mold(&s, &MolderConfig{TrimStrings: true})
	if err != nil {
		t.Fatalf("Mold error: %v", err)
	}
	if s.Name != "John" {
		t.Errorf("expected 'John', got '%s'", s.Name)
	}
}

func TestMold_Lowercase(t *testing.T) {
	type S struct {
		Name string
	}
	s := S{Name: "JOHN"}
	_ = Mold(&s, &MolderConfig{Lowercase: true})
	if s.Name != "john" {
		t.Errorf("expected 'john', got '%s'", s.Name)
	}
}

func TestMold_Uppercase(t *testing.T) {
	type S struct {
		Name string
	}
	s := S{Name: "john"}
	_ = Mold(&s, &MolderConfig{Uppercase: true})
	if s.Name != "JOHN" {
		t.Errorf("expected 'JOHN', got '%s'", s.Name)
	}
}

func TestMold_MoldTag(t *testing.T) {
	type S struct {
		Name string `mold:"trim,upper"`
	}
	s := S{Name: "  hello  "}
	_ = Mold(&s, nil)
	if s.Name != "HELLO" {
		t.Errorf("expected 'HELLO', got '%s'", s.Name)
	}
}

func TestMold_NonPtrError(t *testing.T) {
	type S struct {
		Name string
	}
	s := S{Name: "test"}
	err := Mold(s, nil) // not a pointer
	if err == nil {
		t.Error("expected error for non-pointer")
	}
}

func TestValidateSchema_Required(t *testing.T) {
	schema := &Schema{
		Fields: map[string]FieldSchema{
			"name": {Type: "string", Required: true},
		},
	}
	data := map[string]interface{}{}
	errs := ValidateSchema(data, schema)
	if !errs.HasErrors() {
		t.Error("expected error for missing required field")
	}
}

func TestValidateSchema_TypeCheck(t *testing.T) {
	schema := &Schema{
		Fields: map[string]FieldSchema{
			"age": {Type: "number"},
		},
	}
	data := map[string]interface{}{"age": "not-a-number"}
	errs := ValidateSchema(data, schema)
	if !errs.HasErrors() {
		t.Error("expected type mismatch error")
	}
	data2 := map[string]interface{}{"age": 42}
	errs2 := ValidateSchema(data2, schema)
	if errs2.HasErrors() {
		t.Error("expected no error for correct type")
	}
}

func TestValidateSchema_MinMax(t *testing.T) {
	min := 18.0
	max := 65.0
	schema := &Schema{
		Fields: map[string]FieldSchema{
			"age": {Type: "number", Min: &min, Max: &max},
		},
	}
	data := map[string]interface{}{"age": 17}
	if !ValidateSchema(data, schema).HasErrors() {
		t.Error("17 should fail min=18")
	}
	data["age"] = 30
	if ValidateSchema(data, schema).HasErrors() {
		t.Error("30 should pass")
	}
	data["age"] = 70
	if !ValidateSchema(data, schema).HasErrors() {
		t.Error("70 should fail max=65")
	}
}

func TestValidateSchema_StringConstraints(t *testing.T) {
	minLen := 3
	maxLen := 10
	schema := &Schema{
		Fields: map[string]FieldSchema{
			"code": {Type: "string", MinLen: &minLen, MaxLen: &maxLen, Pattern: "^[A-Z]+$"},
		},
	}
	data := map[string]interface{}{"code": "AB"}
	if !ValidateSchema(data, schema).HasErrors() {
		t.Error("'AB' too short")
	}
	data["code"] = "ABCDEFGHIJKLMNOP" // too long
	if !ValidateSchema(data, schema).HasErrors() {
		t.Error("too long should fail")
	}
	data["code"] = "abc" // pattern mismatch
	if !ValidateSchema(data, schema).HasErrors() {
		t.Error("pattern mismatch should fail")
	}
	data["code"] = "ABC"
	if ValidateSchema(data, schema).HasErrors() {
		t.Error("'ABC' should pass all checks")
	}
}

func TestValidateSchema_OneOf(t *testing.T) {
	schema := &Schema{
		Fields: map[string]FieldSchema{
			"color": {OneOf: []string{"red", "green", "blue"}},
		},
	}
	data := map[string]interface{}{"color": "red"}
	if ValidateSchema(data, schema).HasErrors() {
		t.Error("'red' should be valid")
	}
	data["color"] = "yellow"
	if !ValidateSchema(data, schema).HasErrors() {
		t.Error("'yellow' should be invalid")
	}
}

func TestValidateSchema_Nested(t *testing.T) {
	schema := &Schema{
		Fields: map[string]FieldSchema{
			"address": {
				Type: "object",
				Children: &Schema{
					Fields: map[string]FieldSchema{
						"street": {Type: "string", Required: true},
					},
				},
			},
		},
	}
	// "street" not present in nested object — but required=true means it should fail
	data2 := map[string]interface{}{"address": map[string]interface{}{}}
	errs := ValidateSchema(data2, schema)
	if !errs.HasErrors() {
		t.Error("missing required nested field should fail")
	}
}

func TestValidateSchema_NilSchema(t *testing.T) {
	errs := ValidateSchema(nil, nil)
	if errs.HasErrors() {
		t.Error("nil schema should produce no errors")
	}
}

func TestCoerceString(t *testing.T) {
	if s, ok := CoerceString("hello"); !ok || s != "hello" {
		t.Error("CoerceString should work for strings")
	}
	if s, ok := CoerceString(42); !ok || s != "42" {
		t.Errorf("CoerceString for int: got %q", s)
	}
}

func TestCoerceInt(t *testing.T) {
	if i, ok := CoerceInt(42); !ok || i != 42 {
		t.Error("CoerceInt(42) failed")
	}
	if i, ok := CoerceInt("123"); !ok || i != 123 {
		t.Error("CoerceInt('123') failed")
	}
	if _, ok := CoerceInt("abc"); ok {
		t.Error("CoerceInt('abc') should fail")
	}
}

func TestCoerceFloat(t *testing.T) {
	if f, ok := CoerceFloat(3.14); !ok || f != 3.14 {
		t.Error("CoerceFloat(3.14) failed")
	}
	if f, ok := CoerceFloat("2.5"); !ok || f != 2.5 {
		t.Error("CoerceFloat('2.5') failed")
	}
}

func TestCoerceBool(t *testing.T) {
	if b, ok := CoerceBool(true); !ok || !b {
		t.Error("CoerceBool(true) failed")
	}
	if b, ok := CoerceBool("true"); !ok || !b {
		t.Error("CoerceBool('true') failed")
	}
	if b, ok := CoerceBool(1); !ok || !b {
		t.Error("CoerceBool(1) should be true")
	}
}

func TestRegisterValidator_Global(t *testing.T) {
	m := New()
	m.Register("even", func(value interface{}, _ string) bool {
		n, ok := value.(int)
		if !ok {
			return false
		}
		return n%2 == 0
	})

	type S struct {
		Num int `validate:"even"`
	}
	if m.Validate(S{Num: 2}).HasErrors() {
		t.Error("2 should be even")
	}
	if !m.Validate(S{Num: 3}).HasErrors() {
		t.Error("3 should not be even")
	}
}

func TestDefaultMessages(t *testing.T) {
	msgs := defaultMessages()
	if _, ok := msgs["en"]; !ok {
		t.Error("should have English messages")
	}
	if _, ok := msgs["es"]; !ok {
		t.Error("should have Spanish messages")
	}
	if _, ok := msgs["fr"]; !ok {
		t.Error("should have French messages")
	}
}

func TestValidationError_Error(t *testing.T) {
	ve := &ValidationError{Field: "Name", Message: "is required"}
	if !strings.Contains(ve.Error(), "Name") {
		t.Error("error string should contain field name")
	}
}
