// Package mold provides data molding and validation: struct tag-based validation,
// custom validators, error collection with field paths, and i18n error messages.
package mold

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// ---- Validation Errors ----

// ValidationError represents a single validation error on a field.
type ValidationError struct {
	Field   string      `json:"field"`
	Message string      `json:"message"`
	Code    string      `json:"code"`
	Value   interface{} `json:"value,omitempty"`
}

// Error implements the error interface.
func (ve *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", ve.Field, ve.Message)
}

// ValidationErrors is a collection of validation errors.
type ValidationErrors []*ValidationError

// Error implements the error interface.
func (ve ValidationErrors) Error() string {
	if len(ve) == 0 {
		return ""
	}
	msgs := make([]string, len(ve))
	for i, e := range ve {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "; ")
}

// HasErrors returns true if there are any errors.
func (ve ValidationErrors) HasErrors() bool {
	return len(ve) > 0
}

// ByField returns errors for a specific field.
func (ve ValidationErrors) ByField(field string) ValidationErrors {
	var result ValidationErrors
	for _, e := range ve {
		if e.Field == field {
			result = append(result, e)
		}
	}
	return result
}

// Fields returns all unique field names with errors.
func (ve ValidationErrors) Fields() []string {
	seen := make(map[string]bool)
	var fields []string
	for _, e := range ve {
		if !seen[e.Field] {
			seen[e.Field] = true
			fields = append(fields, e.Field)
		}
	}
	return fields
}

// ---- Validator Interface ----

// Validator is the interface that types can implement for custom validation.
type Validator interface {
	Validate() error
}

// FieldValidator validates a single field value.
type FieldValidator func(value interface{}, param string) bool

// ---- Validation Rules ----

// Rule represents a validation rule parsed from a struct tag.
type Rule struct {
	Name  string
	Param string
}

// ParseTag parses a validation tag string like "required,min=3,max=10".
func ParseTag(tag string) []Rule {
	if tag == "" {
		return nil
	}
	parts := strings.Split(tag, ",")
	rules := make([]Rule, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.Index(part, "="); idx >= 0 {
			rules = append(rules, Rule{
				Name:  strings.TrimSpace(part[:idx]),
				Param: strings.TrimSpace(part[idx+1:]),
			})
		} else {
			rules = append(rules, Rule{Name: part})
		}
	}
	return rules
}

// ---- Built-in Field Validators ----

var builtinValidators = map[string]FieldValidator{
	"required":   validateRequired,
	"min":        validateMin,
	"max":        validateMax,
	"len":        validateLen,
	"email":      validateEmail,
	"url":        validateURL,
	"alpha":      validateAlpha,
	"alphanum":   validateAlphanum,
	"numeric":    validateNumeric,
	"pattern":    validatePattern,
	"oneof":      validateOneOf,
	"minlen":     validateMinLen,
	"maxlen":     validateMaxLen,
	"gt":         validateGt,
	"gte":        validateGte,
	"lt":         validateLt,
	"lte":        validateLte,
	"startswith": validateStartsWith,
	"endswith":   validateEndsWith,
	"contains":   validateContains,
}

// RegisterValidator registers a custom field validator.
func RegisterValidator(name string, fn FieldValidator) {
	builtinValidators[name] = fn
}

// ---- Validation functions ----

func validateRequired(value interface{}, _ string) bool {
	if value == nil {
		return false
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.String:
		return v.String() != ""
	case reflect.Slice, reflect.Map, reflect.Array:
		return v.Len() > 0
	case reflect.Ptr, reflect.Interface:
		return !v.IsNil()
	default:
		return true // non-zero values are "present"
	}
}

func validateMin(value interface{}, param string) bool {
	min, err := strconv.ParseFloat(param, 64)
	if err != nil {
		return false
	}
	v := toFloat(value)
	return v >= min
}

func validateMax(value interface{}, param string) bool {
	max, err := strconv.ParseFloat(param, 64)
	if err != nil {
		return false
	}
	v := toFloat(value)
	return v <= max
}

func validateLen(value interface{}, param string) bool {
	n, err := strconv.Atoi(param)
	if err != nil {
		return false
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.String:
		return v.Len() == n
	case reflect.Slice, reflect.Map, reflect.Array:
		return v.Len() == n
	default:
		return false
	}
}

func validateMinLen(value interface{}, param string) bool {
	n, err := strconv.Atoi(param)
	if err != nil {
		return false
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.String:
		return v.Len() >= n
	case reflect.Slice, reflect.Map, reflect.Array:
		return v.Len() >= n
	default:
		return false
	}
}

func validateMaxLen(value interface{}, param string) bool {
	n, err := strconv.Atoi(param)
	if err != nil {
		return false
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.String:
		return v.Len() <= n
	case reflect.Slice, reflect.Map, reflect.Array:
		return v.Len() <= n
	default:
		return false
	}
}

func validateEmail(value interface{}, _ string) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	emailRe := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	return emailRe.MatchString(s)
}

func validateURL(value interface{}, _ string) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	urlRe := regexp.MustCompile(`^https?://[^\s]+$`)
	return urlRe.MatchString(s)
}

func validateAlpha(value interface{}, _ string) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return len(s) > 0
}

func validateAlphanum(value interface{}, _ string) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return len(s) > 0
}

func validateNumeric(value interface{}, _ string) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) && r != '.' && r != '-' {
			return false
		}
	}
	return len(s) > 0
}

func validatePattern(value interface{}, param string) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	re, err := regexp.Compile(param)
	if err != nil {
		return false
	}
	return re.MatchString(s)
}

func validateOneOf(value interface{}, param string) bool {
	options := strings.Split(param, "|")
	s := fmt.Sprintf("%v", value)
	for _, opt := range options {
		if strings.TrimSpace(opt) == s {
			return true
		}
	}
	return false
}

func validateGt(value interface{}, param string) bool {
	n, err := strconv.ParseFloat(param, 64)
	if err != nil {
		return false
	}
	return toFloat(value) > n
}

func validateGte(value interface{}, param string) bool {
	n, err := strconv.ParseFloat(param, 64)
	if err != nil {
		return false
	}
	return toFloat(value) >= n
}

func validateLt(value interface{}, param string) bool {
	n, err := strconv.ParseFloat(param, 64)
	if err != nil {
		return false
	}
	return toFloat(value) < n
}

func validateLte(value interface{}, param string) bool {
	n, err := strconv.ParseFloat(param, 64)
	if err != nil {
		return false
	}
	return toFloat(value) <= n
}

func validateStartsWith(value interface{}, param string) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	return strings.HasPrefix(s, param)
}

func validateEndsWith(value interface{}, param string) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	return strings.HasSuffix(s, param)
}

func validateContains(value interface{}, param string) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	return strings.Contains(s, param)
}

func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case int:
		return float64(val)
	case int8:
		return float64(val)
	case int16:
		return float64(val)
	case int32:
		return float64(val)
	case int64:
		return float64(val)
	case uint:
		return float64(val)
	case uint8:
		return float64(val)
	case uint16:
		return float64(val)
	case uint32:
		return float64(val)
	case uint64:
		return float64(val)
	case float32:
		return float64(val)
	case float64:
		return val
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	default:
		return 0
	}
}

// ---- Molder: struct validation engine ----

// Molder validates structs using struct tags.
type Molder struct {
	mu         sync.RWMutex
	validators map[string]FieldValidator
	tagName    string
	locale     string
	messages   map[string]map[string]string // locale -> code -> message
}

// New creates a new Molder with default settings.
func New() *Molder {
	return &Molder{
		validators: copyValidators(builtinValidators),
		tagName:    "validate",
		locale:     "en",
		messages:   defaultMessages(),
	}
}

// SetTagName sets the struct tag name to look for (default "validate").
func (m *Molder) SetTagName(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tagName = name
}

// SetLocale sets the locale for error messages.
func (m *Molder) SetLocale(locale string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.locale = locale
}

// SetMessages sets custom i18n error messages.
func (m *Molder) SetMessages(locale string, messages map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[locale] = messages
}

// Register adds a custom field validator.
func (m *Molder) Register(name string, fn FieldValidator) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.validators[name] = fn
}

// Validate validates a struct and returns all errors.
func (m *Molder) Validate(v interface{}) ValidationErrors {
	return m.validateValue(reflect.ValueOf(v), "")
}

func (m *Molder) validateValue(val reflect.Value, prefix string) ValidationErrors {
	var errs ValidationErrors

	// Dereference pointers
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return errs
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return errs
	}

	t := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := t.Field(i)
		fieldVal := val.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		fieldPath := field.Name
		if prefix != "" {
			fieldPath = prefix + "." + field.Name
		}

		// Get validation tag
		tag := field.Tag.Get(m.tagName)
		if tag == "" {
			// No validation, but still recurse into nested structs
			if fieldVal.Kind() == reflect.Struct && field.Type != reflect.TypeOf((*interface{})(nil)).Elem() {
				nested := m.validateValue(fieldVal, fieldPath)
				errs = append(errs, nested...)
			}
			continue
		}

		// Handle skip
		if tag == "-" {
			continue
		}

		rules := ParseTag(tag)

		// Handle nested struct validation
		if fieldVal.Kind() == reflect.Struct && field.Type != reflect.TypeOf((*interface{})(nil)).Elem() {
			nested := m.validateValue(fieldVal, fieldPath)
			errs = append(errs, nested...)
		}

		// Apply rules
		for _, rule := range rules {
			fn, ok := m.validators[rule.Name]
			if !ok {
				errs = append(errs, &ValidationError{
					Field:   fieldPath,
					Message: fmt.Sprintf("unknown validator '%s'", rule.Name),
					Code:    "unknown_validator",
				})
				continue
			}

			var actualValue interface{}
			if fieldVal.CanInterface() {
				actualValue = fieldVal.Interface()
			}

			if !fn(actualValue, rule.Param) {
				errs = append(errs, &ValidationError{
					Field:   fieldPath,
					Message: m.formatMessage(rule.Name, fieldPath, rule.Param),
					Code:    rule.Name,
					Value:   actualValue,
				})
			}
		}
	}

	return errs
}

func (m *Molder) formatMessage(rule, field, param string) string {
	locale := m.locale
	if _, ok := m.messages[locale]; !ok {
		locale = "en"
	}
	if msgs, ok := m.messages[locale]; ok {
		if msg, ok := msgs[rule]; ok {
			msg = strings.ReplaceAll(msg, "{field}", field)
			msg = strings.ReplaceAll(msg, "{param}", param)
			return msg
		}
	}
	// Default message
	return fmt.Sprintf("field '%s' failed validation '%s'", field, rule)
}

func defaultMessages() map[string]map[string]string {
	return map[string]map[string]string{
		"en": {
			"required":   "{field} is required",
			"min":        "{field} must be at least {param}",
			"max":        "{field} must be at most {param}",
			"len":        "{field} must have length {param}",
			"email":      "{field} must be a valid email",
			"url":        "{field} must be a valid URL",
			"alpha":      "{field} must contain only letters",
			"alphanum":   "{field} must contain only letters and numbers",
			"numeric":    "{field} must be numeric",
			"pattern":    "{field} must match pattern {param}",
			"oneof":      "{field} must be one of: {param}",
			"minlen":     "{field} must have minimum length {param}",
			"maxlen":     "{field} must have maximum length {param}",
			"gt":         "{field} must be greater than {param}",
			"gte":        "{field} must be greater than or equal to {param}",
			"lt":         "{field} must be less than {param}",
			"lte":        "{field} must be less than or equal to {param}",
			"startswith": "{field} must start with '{param}'",
			"endswith":   "{field} must end with '{param}'",
			"contains":   "{field} must contain '{param}'",
		},
		"es": {
			"required": "{field} es obligatorio",
			"email":    "{field} debe ser un correo válido",
		},
		"fr": {
			"required": "{field} est requis",
			"email":    "{field} doit être un email valide",
		},
	}
}

// Validate is a convenience function that validates using the default Molder.
func Validate(v interface{}) ValidationErrors {
	return defaultMolder.Validate(v)
}

var defaultMolder = New()

// ---- Molding: data transformation ----

// MolderConfig configures data molding behavior.
type MolderConfig struct {
	TrimStrings   bool
	Lowercase     bool
	Uppercase     bool
	DefaultValues map[string]interface{}
	RenameFields  map[string]string
	RemoveFields  []string
	StrictMode    bool // error on unknown fields
}

// Mold applies transformations to a struct based on mold tags.
func Mold(v interface{}, config *MolderConfig) error {
	if config == nil {
		config = &MolderConfig{}
	}

	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Ptr || val.IsNil() {
		return fmt.Errorf("mold: expected non-nil pointer to struct")
	}

	elem := val.Elem()
	if elem.Kind() != reflect.Struct {
		return fmt.Errorf("mold: expected pointer to struct, got %s", elem.Kind())
	}

	return moldStruct(elem, config, "")
}

func moldStruct(val reflect.Value, config *MolderConfig, prefix string) error {
	t := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := t.Field(i)
		fieldVal := val.Field(i)

		if !fieldVal.CanSet() {
			continue
		}

		fieldPath := field.Name
		if prefix != "" {
			fieldPath = prefix + "." + field.Name
		}

		// Read mold tag
		moldTag := field.Tag.Get("mold")
		if moldTag == "-" {
			// Zero out the field
			fieldVal.Set(reflect.Zero(field.Type))
			continue
		}

		// Handle string transformations
		if fieldVal.Kind() == reflect.String && fieldVal.CanSet() {
			s := fieldVal.String()
			if config.TrimStrings {
				s = strings.TrimSpace(s)
			}
			if config.Lowercase {
				s = strings.ToLower(s)
			}
			if config.Uppercase {
				s = strings.ToUpper(s)
			}
			// Apply mold tag
			if moldTag != "" {
				s = applyMoldTag(s, moldTag)
			}
			fieldVal.SetString(s)
		}

		// Apply default values
		if config.DefaultValues != nil {
			if dv, ok := config.DefaultValues[fieldPath]; ok {
				if fieldVal.IsZero() {
					setValue(fieldVal, dv)
				}
			}
		}

		// Recurse into nested structs
		if fieldVal.Kind() == reflect.Struct {
			if err := moldStruct(fieldVal, config, fieldPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func applyMoldTag(s, tag string) string {
	parts := strings.Split(tag, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		switch {
		case part == "trim":
			s = strings.TrimSpace(s)
		case part == "lower":
			s = strings.ToLower(s)
		case part == "upper":
			s = strings.ToUpper(s)
		case part == "title":
			s = strings.Title(s)
		}
	}
	return s
}

func setValue(field reflect.Value, v interface{}) {
	if !field.CanSet() {
		return
	}
	val := reflect.ValueOf(v)
	if val.Type().AssignableTo(field.Type()) {
		field.Set(val)
	} else if val.Type().ConvertibleTo(field.Type()) {
		field.Set(val.Convert(field.Type()))
	}
}

// ---- Schema-based validation ----

// Schema defines validation rules for a data structure.
type Schema struct {
	Fields map[string]FieldSchema `json:"fields"`
}

// FieldSchema defines validation rules for a single field.
type FieldSchema struct {
	Type     string        `json:"type"` // "string", "number", "bool", "array", "object"
	Required bool          `json:"required"`
	Min      *float64      `json:"min,omitempty"`
	Max      *float64      `json:"max,omitempty"`
	MinLen   *int          `json:"min_len,omitempty"`
	MaxLen   *int          `json:"max_len,omitempty"`
	Pattern  string        `json:"pattern,omitempty"`
	OneOf    []string      `json:"one_of,omitempty"`
	Enum     []interface{} `json:"enum,omitempty"`
	Children *Schema       `json:"children,omitempty"` // for nested objects
}

// ValidateSchema validates data against a schema.
func ValidateSchema(data map[string]interface{}, schema *Schema) ValidationErrors {
	var errs ValidationErrors
	if schema == nil {
		return errs
	}

	for fieldName, fieldSchema := range schema.Fields {
		value, exists := data[fieldName]

		// Check required
		if fieldSchema.Required && (!exists || value == nil) {
			errs = append(errs, &ValidationError{
				Field:   fieldName,
				Message: fmt.Sprintf("%s is required", fieldName),
				Code:    "required",
			})
			continue
		}

		if !exists {
			continue
		}

		// Type check
		if fieldSchema.Type != "" && !checkType(value, fieldSchema.Type) {
			errs = append(errs, &ValidationError{
				Field:   fieldName,
				Message: fmt.Sprintf("%s must be of type %s", fieldName, fieldSchema.Type),
				Code:    "type_mismatch",
				Value:   value,
			})
			continue
		}

		// Numeric constraints
		if fieldSchema.Type == "number" {
			if num, ok := toFloat64(value); ok {
				if fieldSchema.Min != nil && num < *fieldSchema.Min {
					errs = append(errs, &ValidationError{
						Field:   fieldName,
						Message: fmt.Sprintf("%s must be at least %v", fieldName, *fieldSchema.Min),
						Code:    "min",
					})
				}
				if fieldSchema.Max != nil && num > *fieldSchema.Max {
					errs = append(errs, &ValidationError{
						Field:   fieldName,
						Message: fmt.Sprintf("%s must be at most %v", fieldName, *fieldSchema.Max),
						Code:    "max",
					})
				}
			}
		}

		// String constraints
		if fieldSchema.Type == "string" {
			s, ok := value.(string)
			if ok {
				if fieldSchema.MinLen != nil && len(s) < *fieldSchema.MinLen {
					errs = append(errs, &ValidationError{
						Field:   fieldName,
						Message: fmt.Sprintf("%s must have at least %d characters", fieldName, *fieldSchema.MinLen),
						Code:    "minlen",
					})
				}
				if fieldSchema.MaxLen != nil && len(s) > *fieldSchema.MaxLen {
					errs = append(errs, &ValidationError{
						Field:   fieldName,
						Message: fmt.Sprintf("%s must have at most %d characters", fieldName, *fieldSchema.MaxLen),
						Code:    "maxlen",
					})
				}
				if fieldSchema.Pattern != "" {
					if re, err := regexp.Compile(fieldSchema.Pattern); err == nil {
						if !re.MatchString(s) {
							errs = append(errs, &ValidationError{
								Field:   fieldName,
								Message: fmt.Sprintf("%s does not match pattern", fieldName),
								Code:    "pattern",
							})
						}
					}
				}
			}
		}

		// OneOf / Enum
		if len(fieldSchema.OneOf) > 0 || len(fieldSchema.Enum) > 0 {
			options := fieldSchema.OneOf
			if len(fieldSchema.Enum) > 0 {
				options = make([]string, len(fieldSchema.Enum))
				for i, e := range fieldSchema.Enum {
					options[i] = fmt.Sprintf("%v", e)
				}
			}
			strVal := fmt.Sprintf("%v", value)
			found := false
			for _, opt := range options {
				if opt == strVal {
					found = true
					break
				}
			}
			if !found {
				errs = append(errs, &ValidationError{
					Field:   fieldName,
					Message: fmt.Sprintf("%s must be one of: %s", fieldName, strings.Join(options, ", ")),
					Code:    "oneof",
				})
			}
		}

		// Nested object
		if fieldSchema.Children != nil {
			if nested, ok := value.(map[string]interface{}); ok {
				nestedErrs := ValidateSchema(nested, fieldSchema.Children)
				for _, ne := range nestedErrs {
					ne.Field = fieldName + "." + ne.Field
					errs = append(errs, ne)
				}
			}
		}
	}

	return errs
}

func checkType(value interface{}, expected string) bool {
	switch expected {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		switch value.(type) {
		case int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64:
			return true
		}
		return false
	case "bool":
		_, ok := value.(bool)
		return ok
	case "array":
		_, ok := value.([]interface{})
		return ok
	case "object":
		_, ok := value.(map[string]interface{})
		return ok
	default:
		return false
	}
}

func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case string:
		f, err := strconv.ParseFloat(val, 64)
		return f, err == nil
	}
	return 0, false
}

// ---- Utility helpers ----

func copyValidators(src map[string]FieldValidator) map[string]FieldValidator {
	dst := make(map[string]FieldValidator, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// ---- Molding helpers ----

// CoerceString attempts to convert a value to a string.
func CoerceString(v interface{}) (string, bool) {
	switch val := v.(type) {
	case string:
		return val, true
	case fmt.Stringer:
		return val.String(), true
	default:
		return fmt.Sprintf("%v", v), true
	}
}

// CoerceInt attempts to convert a value to an int.
func CoerceInt(v interface{}) (int, bool) {
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	case string:
		i, err := strconv.Atoi(val)
		return i, err == nil
	}
	return 0, false
}

// CoerceFloat attempts to convert a value to a float64.
func CoerceFloat(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case string:
		f, err := strconv.ParseFloat(val, 64)
		return f, err == nil
	}
	return 0, false
}

// CoerceBool attempts to convert a value to a bool.
func CoerceBool(v interface{}) (bool, bool) {
	switch val := v.(type) {
	case bool:
		return val, true
	case string:
		b, err := strconv.ParseBool(val)
		return b, err == nil
	case int:
		return val != 0, true
	}
	return false, false
}
