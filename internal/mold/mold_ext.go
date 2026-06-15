// Package mold - extensions: JSON schema validation, conditional validation,
// cross-field validation, type coercion, sanitization, deep merging.
package mold

import (
	"fmt"
	"reflect"
	"strings"
)

// ---- Condition-based Validation ----

// ConditionalRule applies validation only when a condition is met.
type ConditionalRule struct {
	Field     string
	Condition string // "eq", "neq", "gt", "empty", "notempty"
	Value     interface{}
	ThenRules []Rule
	ElseRules []Rule
}

// ValidateConditional applies conditional validation rules.
func (m *Molder) ValidateConditional(v interface{}, rules []ConditionalRule) ValidationErrors {
	var errs ValidationErrors

	val := reflect.ValueOf(v)
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return errs
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return errs
	}

	for _, cr := range rules {
		// Find the field value
		fieldVal := val.FieldByName(cr.Field)
		if !fieldVal.IsValid() {
			continue
		}

		conditionMet := evaluateCondition(fieldVal.Interface(), cr.Condition, cr.Value)

		var rulesToApply []Rule
		if conditionMet {
			rulesToApply = cr.ThenRules
		} else {
			rulesToApply = cr.ElseRules
		}

		for _, rule := range rulesToApply {
			fn, ok := m.validators[rule.Name]
			if !ok {
				errs = append(errs, &ValidationError{
					Field:   cr.Field,
					Message: fmt.Sprintf("unknown validator '%s'", rule.Name),
					Code:    "unknown_validator",
				})
				continue
			}
			if !fn(fieldVal.Interface(), rule.Param) {
				errs = append(errs, &ValidationError{
					Field:   cr.Field,
					Message: m.formatMessage(rule.Name, cr.Field, rule.Param),
					Code:    rule.Name,
				})
			}
		}
	}

	return errs
}

func evaluateCondition(value interface{}, condition string, expected interface{}) bool {
	switch condition {
	case "eq":
		return fmt.Sprintf("%v", value) == fmt.Sprintf("%v", expected)
	case "neq":
		return fmt.Sprintf("%v", value) != fmt.Sprintf("%v", expected)
	case "gt":
		return toFloat(value) > toFloat(expected)
	case "lt":
		return toFloat(value) < toFloat(expected)
	case "empty":
		return !validateRequired(value, "")
	case "notempty":
		return validateRequired(value, "")
	default:
		return false
	}
}

// ---- Cross-Field Validation ----

// CrossFieldValidator validates two fields together.
type CrossFieldValidator struct {
	rules []CrossFieldRule
}

// CrossFieldRule defines a validation across two fields.
type CrossFieldRule struct {
	FieldA   string
	FieldB   string
	Relation string // "eq", "neq", "gt", "gte", "lt", "lte"
	Message  string
}

// NewCrossFieldValidator creates a cross-field validator.
func NewCrossFieldValidator() *CrossFieldValidator {
	return &CrossFieldValidator{
		rules: make([]CrossFieldRule, 0),
	}
}

// AddRule adds a cross-field validation rule.
func (cfv *CrossFieldValidator) AddRule(fieldA, fieldB, relation, message string) {
	cfv.rules = append(cfv.rules, CrossFieldRule{
		FieldA: fieldA, FieldB: fieldB, Relation: relation, Message: message,
	})
}

// Validate checks cross-field constraints on a struct.
func (cfv *CrossFieldValidator) Validate(v interface{}) ValidationErrors {
	var errs ValidationErrors

	val := reflect.ValueOf(v)
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return errs
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return errs
	}

	for _, rule := range cfv.rules {
		fa := val.FieldByName(rule.FieldA)
		fb := val.FieldByName(rule.FieldB)

		if !fa.IsValid() || !fb.IsValid() {
			continue
		}

		a := toFloat(fa.Interface())
		b := toFloat(fb.Interface())

		valid := false
		switch rule.Relation {
		case "eq":
			valid = a == b
		case "neq":
			valid = a != b
		case "gt":
			valid = a > b
		case "gte":
			valid = a >= b
		case "lt":
			valid = a < b
		case "lte":
			valid = a <= b
		}

		if !valid {
			msg := rule.Message
			if msg == "" {
				msg = fmt.Sprintf("%s must be %s %s", rule.FieldA, rule.Relation, rule.FieldB)
			}
			errs = append(errs, &ValidationError{
				Field:   rule.FieldA,
				Message: msg,
				Code:    "cross_field",
			})
		}
	}

	return errs
}

// ---- Type Coercion / Sanitization ----

// Sanitizer applies sanitization rules to struct fields.
type Sanitizer struct {
	rules map[string]SanitizeRule
}

// SanitizeRule defines a sanitization operation.
type SanitizeRule struct {
	Trim    bool
	Lower   bool
	Upper   bool
	Default interface{}
}

// NewSanitizer creates a sanitizer.
func NewSanitizer() *Sanitizer {
	return &Sanitizer{
		rules: make(map[string]SanitizeRule),
	}
}

// AddRule adds a sanitization rule for a field.
func (s *Sanitizer) AddRule(fieldName string, rule SanitizeRule) {
	s.rules[fieldName] = rule
}

// Sanitize applies sanitization rules to a struct.
func (s *Sanitizer) Sanitize(v interface{}) error {
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Ptr || val.IsNil() {
		return fmt.Errorf("sanitize: expected non-nil pointer to struct")
	}

	elem := val.Elem()
	if elem.Kind() != reflect.Struct {
		return fmt.Errorf("sanitize: expected pointer to struct")
	}

	t := elem.Type()
	for i := 0; i < elem.NumField(); i++ {
		field := t.Field(i)
		fieldVal := elem.Field(i)

		if !fieldVal.CanSet() {
			continue
		}

		rule, ok := s.rules[field.Name]
		if !ok {
			continue
		}

		if fieldVal.Kind() == reflect.String {
			str := fieldVal.String()
			if rule.Trim {
				str = strings.TrimSpace(str)
			}
			if rule.Lower {
				str = strings.ToLower(str)
			}
			if rule.Upper {
				str = strings.ToUpper(str)
			}
			fieldVal.SetString(str)
		}

		if fieldVal.IsZero() && rule.Default != nil {
			dVal := reflect.ValueOf(rule.Default)
			if dVal.Type().AssignableTo(field.Type) {
				fieldVal.Set(dVal)
			}
		}
	}

	return nil
}

// ---- Deep Merge ----

// DeepMerge merges src into dst recursively.
func DeepMerge(dst, src map[string]interface{}) map[string]interface{} {
	for key, srcVal := range src {
		if dstVal, ok := dst[key]; ok {
			srcMap, srcIsMap := srcVal.(map[string]interface{})
			dstMap, dstIsMap := dstVal.(map[string]interface{})

			if srcIsMap && dstIsMap {
				dst[key] = DeepMerge(dstMap, srcMap)
				continue
			}
		}
		dst[key] = deepCopyIface(srcVal)
	}
	return dst
}

func deepCopyIface(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		cpy := make(map[string]interface{})
		for k, vv := range val {
			cpy[k] = deepCopyIface(vv)
		}
		return cpy
	case []interface{}:
		cpy := make([]interface{}, len(val))
		for i, vv := range val {
			cpy[i] = deepCopyIface(vv)
		}
		return cpy
	default:
		return v
	}
}

// ---- Diff Maps ----

// MapDiff represents a difference between two maps.
type MapDiff struct {
	Added    map[string]interface{}
	Removed  map[string]interface{}
	Modified map[string]struct{ Old, New interface{} }
}

// DiffMaps computes the difference between two maps.
func DiffMaps(old, new map[string]interface{}) *MapDiff {
	diff := &MapDiff{
		Added:    make(map[string]interface{}),
		Removed:  make(map[string]interface{}),
		Modified: make(map[string]struct{ Old, New interface{} }),
	}

	for k, v := range new {
		if _, ok := old[k]; !ok {
			diff.Added[k] = v
		}
	}

	for k, v := range old {
		if newV, ok := new[k]; !ok {
			diff.Removed[k] = v
		} else if fmt.Sprintf("%v", v) != fmt.Sprintf("%v", newV) {
			diff.Modified[k] = struct{ Old, New interface{} }{v, newV}
		}
	}

	return diff
}

// ---- Struct tag utilities ----

// ExtractTag extracts a tag value from a struct field.
func ExtractTag(v interface{}, fieldName, tagKey string) (string, bool) {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return "", false
	}
	field, ok := t.FieldByName(fieldName)
	if !ok {
		return "", false
	}
	val := field.Tag.Get(tagKey)
	return val, val != ""
}

// GetAllTags extracts all values for a given tag key from a struct.
func GetAllTags(v interface{}, tagKey string) map[string]string {
	result := make(map[string]string)
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return result
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if val := field.Tag.Get(tagKey); val != "" {
			result[field.Name] = val
		}
	}
	return result
}
