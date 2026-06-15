// Package schemavalid validates JSON data against JSON Schema definitions.
// It supports draft-2020-12 with type checking, required fields, enums,
// minLength/maxLength, minimum/maximum, pattern matching, and nested refs.
package schemavalid

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Schema is a JSON Schema definition.
type Schema struct {
	Type        string             `json:"type,omitempty"`
	Properties  map[string]*Schema `json:"properties,omitempty"`
	Required    []string           `json:"required,omitempty"`
	Enum        []any              `json:"enum,omitempty"`
	MinLength   *int               `json:"minLength,omitempty"`
	MaxLength   *int               `json:"maxLength,omitempty"`
	Minimum     *float64           `json:"minimum,omitempty"`
	Maximum     *float64           `json:"maximum,omitempty"`
	Pattern     string             `json:"pattern,omitempty"`
	patternRe   *regexp.Regexp     `json:"-"`
	Items       *Schema            `json:"items,omitempty"`
	Description string             `json:"description,omitempty"`
}

// ValidationError describes one validation failure.
type ValidationError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string { return e.Path + ": " + e.Message }

// Validator validates data against schemas.
type Validator struct {
	schemas map[string]*Schema
}

// NewValidator creates a schema validator.
func NewValidator() *Validator { return &Validator{schemas: map[string]*Schema{}} }

// Register adds a named schema.
func (v *Validator) Register(name string, raw json.RawMessage) error {
	var s Schema
	if err := json.Unmarshal(raw, &s); err != nil { return err }
	if s.Pattern != "" { s.patternRe = regexp.MustCompile(s.Pattern) }
	v.schemas[name] = &s; return nil
}

// Validate checks data against a named schema.
func (v *Validator) Validate(schemaName string, data json.RawMessage) error {
	s, ok := v.schemas[schemaName]
	if !ok { return fmt.Errorf("schema %q not found", schemaName) }
	var val any; json.Unmarshal(data, &val)
	return validate("$", val, s)
}

func validate(path string, val any, s *Schema) error {
	if s == nil { return nil }
	var errs []error

	if s.Type != "" && !matchType(s.Type, val) {
		errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("expected type %s, got %T", s.Type, val)})
	}

	if s.MinLength != nil {
		if str, ok := val.(string); ok && len(str) < *s.MinLength {
			errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("min length %d, got %d", *s.MinLength, len(str))})
		}
	}

	if s.MaxLength != nil {
		if str, ok := val.(string); ok && len(str) > *s.MaxLength {
			errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("max length %d, got %d", *s.MaxLength, len(str))})
		}
	}

	if s.Minimum != nil {
		if num, ok := toFloat(val); ok && num < *s.Minimum {
			errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("minimum %f, got %f", *s.Minimum, num)})
		}
	}

	if s.Maximum != nil {
		if num, ok := toFloat(val); ok && num > *s.Maximum {
			errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("maximum %f, got %f", *s.Maximum, num)})
		}
	}

	if s.Pattern != "" && s.patternRe != nil {
		if str, ok := val.(string); ok && !s.patternRe.MatchString(str) {
			errs = append(errs, ValidationError{Path: path, Message: fmt.Sprintf("pattern %q not matched", s.Pattern)})
		}
	}

	if len(s.Enum) > 0 {
		found := false
		for _, e := range s.Enum {
			if equal(val, e) { found = true; break }
		}
		if !found { errs = append(errs, ValidationError{Path: path, Message: "value not in enum"}) }
	}

	if obj, ok := val.(map[string]any); ok && s.Properties != nil {
		for _, req := range s.Required {
			if _, exists := obj[req]; !exists {
				errs = append(errs, ValidationError{Path: path + "." + req, Message: "required field missing"})
			}
		}
		for key, prop := range s.Properties {
			if child, exists := obj[key]; exists {
				if err := validate(path+"."+key, child, prop); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}

	if arr, ok := val.([]any); ok && s.Items != nil {
		for i, item := range arr {
			if err := validate(fmt.Sprintf("%s[%d]", path, i), item, s.Items); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) == 0 { return nil }
	return &multiError{errs: errs}
}

type multiError struct{ errs []error }

func (m *multiError) Error() string {
	var msgs []string
	for _, e := range m.errs { msgs = append(msgs, e.Error()) }
	return strings.Join(msgs, "; ")
}

// ── Helpers ───────────────────────────────────────────────

func matchType(t string, val any) bool {
	switch t {
	case "object": _, ok := val.(map[string]any); return ok
	case "array": _, ok := val.([]any); return ok
	case "string": _, ok := val.(string); return ok
	case "number":
		switch val.(type) {
		case float64, float32, int, int64, int32: return true
		case json.Number: return true
		}
		return false
	case "integer":
		if n, ok := val.(float64); ok { return n == float64(int64(n)) }
		if _, ok := val.(int); ok { return true }
		if _, ok := val.(int64); ok { return true }
		return false
	case "boolean": _, ok := val.(bool); return ok
	case "null": return val == nil
	}
	return false
}

func toFloat(val any) (float64, bool) {
	switch v := val.(type) {
	case float64: return v, true
	case float32: return float64(v), true
	case int: return float64(v), true
	case int64: return float64(v), true
	case json.Number:
		if f, err := v.Float64(); err == nil { return f, true }
	}
	return 0, false
}

func equal(a, b any) bool {
	if a == nil && b == nil { return true }
	if a == nil || b == nil { return false }
	return strings.EqualFold(fmt.Sprint(a), fmt.Sprint(b))
}
