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

// --- Field Spec ---

// FieldSpec describes how to transform a single field.
type FieldSpec struct {
	From      string      `json:"from"`                // Source field path (dot-separated for nested).
	To        string      `json:"to"`                  // Destination field path.
	Type      string      `json:"type,omitempty"`      // Coerce to: "string", "int", "float", "bool", "auto".
	Default   interface{} `json:"default,omitempty"`   // Default value if missing.
	Required  bool        `json:"required,omitempty"`  // If true, error on missing.
	Regex     string      `json:"regex,omitempty"`     // Regex to extract from string value.
	RegexGroup int        `json:"regex_group,omitempty"` // Group to extract (0 = full match).
	Transform string      `json:"transform,omitempty"` // Named transform: "uppercase", "lowercase", "trim", "slug".
}

// --- Swizzler ---

// Swizzler applies a set of field specs to transform a JSON object.
type Swizzler struct {
	specs       []FieldSpec
	compiled    []compiledSpec
	strict      bool // If true, fail on missing required fields.
}

type compiledSpec struct {
	fromPath  []string
	toPath    []string
	spec      FieldSpec
	regex     *regexp.Regexp
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
			cs.regex = re
		}
		s.compiled = append(s.compiled, cs)
	}
	return s, nil
}

func parsePath(p string) []string {
	if p == "" {
		return nil
	}
	// Support both dot and bracket notation.
	// First, normalize: foo.bar[0].baz -> foo.bar.0.baz
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

		// Apply regex extraction.
		if cs.regex != nil {
			str := fmt.Sprintf("%v", val)
			matches := cs.regex.FindStringSubmatch(str)
			if matches != nil {
				if cs.spec.RegexGroup < len(matches) {
					val = matches[cs.spec.RegexGroup]
				} else if len(matches) > 0 {
					val = matches[0]
				}
			}
		}

		// Apply type coercion.
		var err error
		val, err = coerce(val, cs.spec.Type)
		if err != nil {
			return nil, fmt.Errorf("coerce field %q: %w", cs.spec.From, err)
		}

		// Apply transform.
		val = applyTransform(val, cs.spec.Transform)

		// Set in result.
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

// --- Path traversal ---

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
			// Check if next segment is numeric (array).
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
			// Extend array if needed.
			for len(v) <= idx {
				v = append(v, make(map[string]interface{}))
			}
			cur[seg] = v
			// For arrays, the next segment is the index.
			if m, ok := v[idx].(map[string]interface{}); ok {
				cur = m
				i++ // Skip the index segment.
			} else {
				// Create a map at that index.
				m := make(map[string]interface{})
				v[idx] = m
				cur = m
				i++
			}
		default:
			// Overwrite with a new map.
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

// --- Type coercion ---

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

// --- Transforms ---

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

// --- Flatten / Unflatten ---

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
		setPathUnflatten(result, parts, val)
	}
	return json.Marshal(result)
}

func setPathUnflatten(root map[string]interface{}, path []string, value interface{}) {
	if len(path) == 0 {
		return
	}
	cur := root
	for i := 0; i < len(path)-1; i++ {
		seg := path[i]
		next, ok := cur[seg]
		if !ok {
			// Determine if next segment should be array or map.
			if isNumeric(path[i+1]) {
				next = make([]interface{}, 0)
			} else {
				next = make(map[string]interface{})
			}
			cur[seg] = next
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
			} else {
				m := make(map[string]interface{})
				v[idx] = m
				cur = m
			}
			i++ // Skip index.
		default:
			m := make(map[string]interface{})
			cur[seg] = m
			cur = m
		}
	}
	cur[path[len(path)-1]] = value
}

// --- Rename ---

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

// --- Default Filling ---

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

// --- Regex Extraction ---

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

// --- FormatSpecs ---

// FormatSpecs returns a human-readable listing of all field specs.
func (s *Swizzler) FormatSpecs() string {
	out := fmt.Sprintf("Swizzler: %d specs\n", len(s.specs))
	for _, spec := range s.specs {
		out += fmt.Sprintf("  %s -> %s type=%s required=%v\n",
			spec.From, spec.To, spec.Type, spec.Required)
	}
	return out
}
