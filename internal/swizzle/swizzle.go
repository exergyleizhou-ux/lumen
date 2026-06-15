// Package swizzle implements data swizzling/munging: field rename, type coercion,
// nested flatten/unflatten, regex extraction, and default filling.
package swizzle

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// FieldSpec describes how to transform a single field.
type FieldSpec struct {
	From       string      `json:"from"`
	To         string      `json:"to"`
	Type       string      `json:"type,omitempty"`
	Default    interface{} `json:"default,omitempty"`
	Required   bool        `json:"required,omitempty"`
	Regex      string      `json:"regex,omitempty"`
	RegexGroup int         `json:"regex_group,omitempty"`
	Transform  string      `json:"transform,omitempty"`
}

// Swizzler applies a set of field specs to transform a JSON object.
type Swizzler struct {
	specs    []FieldSpec
	compiled []compiledSpec
	strict   bool
}

type compiledSpec struct {
	fromPath []string
	toPath   []string
	spec     FieldSpec
	re       *regexp.Regexp
}

// New creates a new Swizzler from field specs.
func New(specs []FieldSpec, strict bool) (*Swizzler, error) {
	s := &Swizzler{specs: specs, strict: strict}
	for _, spec := range specs {
		cs := compiledSpec{
			fromPath: parsePath(spec.From),
			toPath:   parsePath(spec.To),
			spec:     spec,
		}
		if spec.Regex != "" {
			re, err := regexp.Compile(spec.Regex)
			if err != nil {
				return nil, fmt.Errorf("invalid regex %q: %w", spec.Regex, err)
			}
			cs.re = re
		}
		s.compiled = append(s.compiled, cs)
	}
	return s, nil
}

func parsePath(p string) []string {
	if p == "" {
		return nil
	}
	p = strings.ReplaceAll(p, "[", ".")
	p = strings.ReplaceAll(p, "]", "")
	parts := strings.Split(p, ".")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// Swizzle applies all field specs to the input JSON and returns transformed JSON.
func (s *Swizzler) Swizzle(input json.RawMessage) (json.RawMessage, error) {
	var root interface{}
	if err := json.Unmarshal(input, &root); err != nil {
		return nil, fmt.Errorf("unmarshal input: %w", err)
	}

	result := make(map[string]interface{})

	for _, cs := range s.compiled {
		val, found := getPath(root, cs.fromPath)
		if !found {
			if cs.spec.Required && s.strict {
				return nil, fmt.Errorf("required field %q not found", cs.spec.From)
			}
			if cs.spec.Default != nil {
				val = cs.spec.Default
			} else {
				continue
			}
		}

		if cs.re != nil {
			str := fmt.Sprintf("%v", val)
			matches := cs.re.FindStringSubmatch(str)
			if matches != nil {
				if cs.spec.RegexGroup < len(matches) {
					val = matches[cs.spec.RegexGroup]
				} else if len(matches) > 0 {
					val = matches[0]
				}
			}
		}

		var err error
		val, err = coerce(val, cs.spec.Type)
		if err != nil {
			return nil, fmt.Errorf("coerce field %q: %w", cs.spec.From, err)
		}

		val = applyTransform(val, cs.spec.Transform)
		setPath(result, cs.toPath, val)
	}

	out, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return out, nil
}

// SwizzleBatch applies swizzling to multiple inputs.
func (s *Swizzler) SwizzleBatch(inputs []json.RawMessage) ([]json.RawMessage, []error) {
	results := make([]json.RawMessage, len(inputs))
	errs := make([]error, len(inputs))
	for i, input := range inputs {
		results[i], errs[i] = s.Swizzle(input)
	}
	return results, errs
}

func getPath(root interface{}, path []string) (interface{}, bool) {
	if len(path) == 0 {
		return root, true
	}
	cur := root
	for _, seg := range path {
		switch v := cur.(type) {
		case map[string]interface{}:
			next, ok := v[seg]
			if !ok {
				return nil, false
			}
			cur = next
		case []interface{}:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(v) {
				return nil, false
			}
			cur = v[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

func setPath(root map[string]interface{}, path []string, value interface{}) {
	if len(path) == 0 {
		return
	}
	cur := root
	for i := 0; i < len(path)-1; i++ {
		seg := path[i]
		next, ok := cur[seg]
		if !ok {
			if isNumeric(path[i+1]) {
				cur[seg] = make([]interface{}, 0)
				next = cur[seg]
			} else {
				cur[seg] = make(map[string]interface{})
				next = cur[seg]
			}
		}
		switch v := next.(type) {
		case map[string]interface{}:
			cur = v
		case []interface{}:
			idx, _ := strconv.Atoi(path[i+1])
			for len(v) <= idx {
				v = append(v, make(map[string]interface{}))
			}
			cur[seg] = v
			if m, ok := v[idx].(map[string]interface{}); ok {
				cur = m
				i++
			} else {
				m := make(map[string]interface{})
				v[idx] = m
				cur = m
				i++
			}
		default:
			m := make(map[string]interface{})
			cur[seg] = m
			cur = m
		}
	}
	cur[path[len(path)-1]] = value
}

func isNumeric(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func coerce(val interface{}, targetType string) (interface{}, error) {
	if targetType == "" || targetType == "auto" {
		return val, nil
	}
	switch targetType {
	case "string":
		return fmt.Sprintf("%v", val), nil
	case "int":
		switch v := val.(type) {
		case float64:
			return int64(v), nil
		case string:
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil, err
			}
			return n, nil
		case json.Number:
			n, err := v.Int64()
			if err != nil {
				return nil, err
			}
			return n, nil
		default:
			return int64(0), nil
		}
	case "float":
		switch v := val.(type) {
		case float64:
			return v, nil
		case string:
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, err
			}
			return f, nil
		case json.Number:
			f, err := v.Float64()
			if err != nil {
				return nil, err
			}
			return f, nil
		default:
			return float64(0), nil
		}
	case "bool":
		switch v := val.(type) {
		case bool:
			return v, nil
		case string:
			b, err := strconv.ParseBool(v)
			if err != nil {
				return nil, err
			}
			return b, nil
		case float64:
			return v != 0, nil
		default:
			return false, nil
		}
	default:
		return val, nil
	}
}

func applyTransform(val interface{}, transform string) interface{} {
	switch transform {
	case "uppercase":
		if s, ok := val.(string); ok {
			return strings.ToUpper(s)
		}
	case "lowercase":
		if s, ok := val.(string); ok {
			return strings.ToLower(s)
		}
	case "trim":
		if s, ok := val.(string); ok {
			return strings.TrimSpace(s)
		}
	case "slug":
		if s, ok := val.(string); ok {
			return slugify(s)
		}
	}
	return val
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	re := regexp.MustCompile(`[^a-z0-9]+`)
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// Flatten flattens a nested JSON object into a single-level map with dot-separated keys.
func Flatten(input json.RawMessage) (map[string]interface{}, error) {
	var root interface{}
	if err := json.Unmarshal(input, &root); err != nil {
		return nil, err
	}
	out := make(map[string]interface{})
	flattenRecursive(root, "", out)
	return out, nil
}

func flattenRecursive(val interface{}, prefix string, out map[string]interface{}) {
	switch v := val.(type) {
	case map[string]interface{}:
		for k, sub := range v {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			flattenRecursive(sub, key, out)
		}
	case []interface{}:
		for i, sub := range v {
			key := fmt.Sprintf("%s[%d]", prefix, i)
			flattenRecursive(sub, key, out)
		}
	default:
		out[prefix] = v
	}
}

// Unflatten expands a flat dot-separated map back into a nested object.
func Unflatten(flat map[string]interface{}) (json.RawMessage, error) {
	result := make(map[string]interface{})
	for key, val := range flat {
		parts := parsePath(key)
		setPath(result, parts, val)
	}
	return json.Marshal(result)
}

// Rename renames top-level keys in a JSON object.
func Rename(input json.RawMessage, renames map[string]string) (json.RawMessage, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(input, &obj); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	for oldKey, newKey := range renames {
		if val, ok := obj[oldKey]; ok {
			delete(obj, oldKey)
			obj[newKey] = val
		}
	}
	return json.Marshal(obj)
}

// FillDefaults sets default values for missing keys in a JSON object.
func FillDefaults(input json.RawMessage, defaults map[string]interface{}) (json.RawMessage, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(input, &obj); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	for key, defVal := range defaults {
		if _, ok := obj[key]; !ok {
			obj[key] = defVal
		}
	}
	return json.Marshal(obj)
}

// ExtractRegex returns all regex match groups from string fields.
func ExtractRegex(input json.RawMessage, fieldPath string, pattern string) ([]map[string]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}

	var root interface{}
	if err := json.Unmarshal(input, &root); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	path := parsePath(fieldPath)
	val, found := getPath(root, path)
	if !found {
		return nil, fmt.Errorf("field %q not found", fieldPath)
	}

	str := fmt.Sprintf("%v", val)
	allMatches := re.FindAllStringSubmatch(str, -1)
	names := re.SubexpNames()

	var results []map[string]string
	for _, match := range allMatches {
		entry := make(map[string]string)
		for i, m := range match {
			name := fmt.Sprintf("$%d", i)
			if i < len(names) && names[i] != "" {
				name = names[i]
			}
			entry[name] = m
		}
		results = append(results, entry)
	}
	return results, nil
}

// FormatSpecs returns a human-readable listing of all field specs.
func (s *Swizzler) FormatSpecs() string {
	out := fmt.Sprintf("Swizzler: %d specs\n", len(s.specs))
	for _, spec := range s.specs {
		out += fmt.Sprintf("  %s -> %s type=%s required=%v\n",
			spec.From, spec.To, spec.Type, spec.Required)
	}
	return out
}

// --- Validation ---

// ValidationRule defines a rule to validate a value.
type ValidationRule struct {
	Field    string `json:"field"`
	Required bool   `json:"required,omitempty"`
	MinLen   int    `json:"min_len,omitempty"`
	MaxLen   int    `json:"max_len,omitempty"`
	Min      float64 `json:"min,omitempty"`
	Max      float64 `json:"max,omitempty"`
	Pattern  string `json:"pattern,omitempty"`
	OneOf    []interface{} `json:"one_of,omitempty"`
}

// Validator validates data against rules.
type Validator struct {
	rules []ValidationRule
	compiled []compiledRule
}

type compiledRule struct {
	rule    ValidationRule
	re      *regexp.Regexp
}

// NewValidator creates a validator from rules.
func NewValidator(rules []ValidationRule) (*Validator, error) {
	v := &Validator{rules: rules}
	for _, r := range rules {
		cr := compiledRule{rule: r}
		if r.Pattern != "" {
			re, err := regexp.Compile(r.Pattern)
			if err != nil { return nil, fmt.Errorf("pattern %q: %w", r.Pattern, err) }
			cr.re = re
		}
		v.compiled = append(v.compiled, cr)
	}
	return v, nil
}

// Validate checks input data against all rules. Returns nil if valid.
func (v *Validator) Validate(input map[string]interface{}) []string {
	var errs []string
	for _, cr := range v.compiled {
		path := parsePath(cr.rule.Field)
		val, found := getPath(input, path)
		if cr.rule.Required && !found {
			errs = append(errs, fmt.Sprintf("%s: required", cr.rule.Field))
			continue
		}
		if !found { continue }
		s := fmt.Sprintf("%v", val)
		if cr.rule.MinLen > 0 && len(s) < cr.rule.MinLen {
			errs = append(errs, fmt.Sprintf("%s: min length %d", cr.rule.Field, cr.rule.MinLen))
		}
		if cr.rule.MaxLen > 0 && len(s) > cr.rule.MaxLen {
			errs = append(errs, fmt.Sprintf("%s: max length %d", cr.rule.Field, cr.rule.MaxLen))
		}
		if cr.re != nil && !cr.re.MatchString(s) {
			errs = append(errs, fmt.Sprintf("%s: pattern mismatch", cr.rule.Field))
		}
		if len(cr.rule.OneOf) > 0 {
			match := false
			for _, o := range cr.rule.OneOf {
				if fmt.Sprintf("%v", o) == s { match = true; break }
			}
			if !match { errs = append(errs, fmt.Sprintf("%s: not in allowed values", cr.rule.Field)) }
		}
		// Numeric range checks.
		if cr.rule.Min != 0 || cr.rule.Max != 0 {
			if f, ok := toFloat(val); ok {
				if cr.rule.Min != 0 && f < cr.rule.Min {
					errs = append(errs, fmt.Sprintf("%s: min %v", cr.rule.Field, cr.rule.Min))
				}
				if cr.rule.Max != 0 && f > cr.rule.Max {
					errs = append(errs, fmt.Sprintf("%s: max %v", cr.rule.Field, cr.rule.Max))
				}
			}
		}
	}
	return errs
}

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64: return n, true
	case int64: return float64(n), true
	case int: return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	default: return 0, false
	}
}

// --- Schema ---

// Schema defines the structure of expected data.
type Schema struct {
	Fields map[string]FieldSchema `json:"fields"`
}

// FieldSchema describes a single field in a schema.
type FieldSchema struct {
	Type     string      `json:"type"`     // string, int, float, bool, array, object.
	Required bool        `json:"required"`
	Default  interface{} `json:"default,omitempty"`
	Items    *FieldSchema `json:"items,omitempty"` // For arrays.
}

// ApplySchema applies a schema to input, filling defaults and coercing types.
func ApplySchema(input json.RawMessage, schema *Schema) (json.RawMessage, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(input, &obj); err != nil {
		return nil, err
	}
	for name, fs := range schema.Fields {
		val, ok := obj[name]
		if !ok {
			if fs.Required { return nil, fmt.Errorf("required field %q missing", name) }
			if fs.Default != nil { obj[name] = fs.Default }
			continue
		}
		coerced, err := coerce(val, fs.Type)
		if err != nil { return nil, fmt.Errorf("field %q: %w", name, err) }
		obj[name] = coerced
	}
	return json.Marshal(obj)
}

// --- JSON Path Query ---

// Query extracts values from JSON using simple path expressions.
func Query(input json.RawMessage, path string) (interface{}, error) {
	var root interface{}
	if err := json.Unmarshal(input, &root); err != nil { return nil, err }
	parts := parsePath(path)
	val, found := getPath(root, parts)
	if !found { return nil, fmt.Errorf("path %q not found", path) }
	return val, nil
}

// QueryString is like Query but always returns a string.
func QueryString(input json.RawMessage, path string) (string, error) {
	v, err := Query(input, path)
	if err != nil { return "", err }
	return fmt.Sprintf("%v", v), nil
}

// --- Merge ---

// MergeJSON shallow-merges src into dst.
func MergeJSON(dst, src json.RawMessage) (json.RawMessage, error) {
	var d, s map[string]interface{}
	if err := json.Unmarshal(dst, &d); err != nil { return nil, err }
	if err := json.Unmarshal(src, &s); err != nil { return nil, err }
	for k, v := range s { d[k] = v }
	return json.Marshal(d)
}

// --- Omit / Pick ---

// Pick returns a new JSON object containing only the specified keys.
func Pick(input json.RawMessage, keys []string) (json.RawMessage, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(input, &obj); err != nil { return nil, err }
	out := make(map[string]interface{})
	for _, k := range keys {
		if v, ok := obj[k]; ok { out[k] = v }
	}
	return json.Marshal(out)
}

// Omit returns a new JSON object without the specified keys.
func Omit(input json.RawMessage, keys []string) (json.RawMessage, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(input, &obj); err != nil { return nil, err }
	drop := make(map[string]bool)
	for _, k := range keys { drop[k] = true }
	out := make(map[string]interface{})
	for k, v := range obj {
		if !drop[k] { out[k] = v }
	}
	return json.Marshal(out)
}

// --- Format ---

// FormatValidationErrors returns a human-readable string of validation errors.
func FormatValidationErrors(errs []string) string {
	if len(errs) == 0 { return "no errors" }
	s := fmt.Sprintf("%d validation errors:\n", len(errs))
	for _, e := range errs { s += "  - " + e + "\n" }
	return s
}

// --- Type Detection ---

// DetectType tries to infer the JSON type of a raw message.
func DetectType(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if len(s) == 0 { return "null" }
	switch s[0] {
	case '{': return "object"
	case '[': return "array"
	case '"': return "string"
	case 't', 'f': return "boolean"
	case 'n': return "null"
	default:
		if _, err := strconv.ParseFloat(s, 64); err == nil { return "number" }
		return "string"
	}
}

// --- Array Operations ---

// MapArray applies a transform function to each element of a JSON array.
func MapArray(input json.RawMessage, fn func(json.RawMessage) (json.RawMessage, error)) (json.RawMessage, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(input, &arr); err != nil { return nil, fmt.Errorf("not an array: %w", err) }
	out := make([]json.RawMessage, len(arr))
	for i, elem := range arr {
		var err error
		out[i], err = fn(elem)
		if err != nil { return nil, fmt.Errorf("element %d: %w", i, err) }
	}
	return json.Marshal(out)
}

// FilterArray filters a JSON array, keeping elements where fn returns true.
func FilterArray(input json.RawMessage, fn func(json.RawMessage) bool) (json.RawMessage, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(input, &arr); err != nil { return nil, fmt.Errorf("not an array: %w", err) }
	var out []json.RawMessage
	for _, elem := range arr {
		if fn(elem) { out = append(out, elem) }
	}
	return json.Marshal(out)
}

// --- Key Casing ---

// CamelToSnake converts camelCase keys to snake_case in a JSON object.
func CamelToSnake(input json.RawMessage) (json.RawMessage, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(input, &obj); err != nil { return nil, err }
	out := make(map[string]interface{})
	for k, v := range obj { out[toSnake(k)] = v }
	return json.Marshal(out)
}

// SnakeToCamel converts snake_case keys to camelCase in a JSON object.
func SnakeToCamel(input json.RawMessage) (json.RawMessage, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(input, &obj); err != nil { return nil, err }
	out := make(map[string]interface{})
	for k, v := range obj { out[toCamel(k)] = v }
	return json.Marshal(out)
}

var camelRE = regexp.MustCompile(`[A-Z]`)
func toSnake(s string) string { return strings.ToLower(camelRE.ReplaceAllStringFunc(s, func(m string) string { return "_" + m })) }
func toCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 { parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:] }
	}
	return strings.Join(parts, "")
}

// --- Deep Clone ---

// DeepCloneJSON returns a deep copy of a JSON value.
func DeepCloneJSON(input json.RawMessage) (json.RawMessage, error) {
	var v interface{}
	if err := json.Unmarshal(input, &v); err != nil { return nil, err }
	return json.Marshal(v)
}

// --- Format Helpers ---

// PrettyJSON reformats JSON with indentation.
func PrettyJSON(input json.RawMessage) (string, error) {
	var v interface{}
	if err := json.Unmarshal(input, &v); err != nil { return "", err }
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil { return "", err }
	return string(b), nil
}

// CompactJSON removes whitespace from JSON.
func CompactJSON(input json.RawMessage) (json.RawMessage, error) {
	var v interface{}
	if err := json.Unmarshal(input, &v); err != nil { return nil, err }
	return json.Marshal(v)
}
