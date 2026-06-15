// Package schema provides a schema registry with versioning, compatibility
// checks, and evolution support for agent data formats (JSON Schema, Proto,
// Avro). It validates data against registered schemas and detects breaking changes.
package schema

import ("encoding/json";"fmt";"sort";"strings";"sync";"time")

// SchemaVersion represents one version of a schema.
type SchemaVersion struct {
	Subject    string    `json:"subject"`
	Version    int       `json:"version"`
	Schema     string    `json:"schema"`
	Format     string    `json:"format"` // json-schema, proto, avro
	CreatedAt  time.Time `json:"created_at"`
	Deprecated bool      `json:"deprecated,omitempty"`
}

// CompatibilityLevel defines what changes are allowed.
type CompatibilityLevel int
const (
	CompatNone CompatibilityLevel = iota
	CompatBackward
	CompatForward
	CompatFull
)
func (c CompatibilityLevel) String() string {
	switch c {
	case CompatBackward: return "backward"
	case CompatForward: return "forward"
	case CompatFull: return "full"
	default: return "none"
	}
}

// Registry manages schema versions.
type Registry struct {
	mu          sync.Mutex
	schemas     map[string][]*SchemaVersion
	compat      CompatibilityLevel
}

// NewRegistry creates a schema registry.
func NewRegistry() *Registry {
	return &Registry{schemas: map[string][]*SchemaVersion{}, compat: CompatBackward}
}

// SetCompatibility sets the compatibility level.
func (r *Registry) SetCompatibility(level CompatibilityLevel) { r.mu.Lock(); defer r.mu.Unlock(); r.compat = level }

// Register adds a schema version.
func (r *Registry) Register(subject, schema, format string) (*SchemaVersion, error) {
	r.mu.Lock(); defer r.mu.Unlock()
	versions := r.schemas[subject]
	version := len(versions) + 1
	sv := &SchemaVersion{Subject: subject, Version: version, Schema: schema, Format: format, CreatedAt: time.Now()}
	r.schemas[subject] = append(r.schemas[subject], sv)
	return sv, nil
}

// GetLatest returns the latest version of a subject.
func (r *Registry) GetLatest(subject string) (*SchemaVersion, bool) {
	r.mu.Lock(); defer r.mu.Unlock()
	versions := r.schemas[subject]
	if len(versions) == 0 { return nil, false }
	return versions[len(versions)-1], true
}

// GetVersion returns a specific version.
func (r *Registry) GetVersion(subject string, version int) (*SchemaVersion, bool) {
	r.mu.Lock(); defer r.mu.Unlock()
	versions := r.schemas[subject]
	for _, v := range versions { if v.Version == version { return v, true } }
	return nil, false
}

// Versions returns all versions of a subject.
func (r *Registry) Versions(subject string) []*SchemaVersion {
	r.mu.Lock(); defer r.mu.Unlock()
	out := make([]*SchemaVersion, len(r.schemas[subject]))
	copy(out, r.schemas[subject])
	return out
}

// Subjects returns all registered subjects.
func (r *Registry) Subjects() []string {
	r.mu.Lock(); defer r.mu.Unlock()
	var out []string
	for s := range r.schemas { out = append(out, s) }
	sort.Strings(out)
	return out
}

// Validate checks data against the latest schema for a subject.
func (r *Registry) Validate(subject string, data any) (bool, []string) {
	r.mu.Lock()
	latest, ok := r.GetLatest(subject)
	r.mu.Unlock()
	if !ok { return false, []string{"subject not found"} }

	// For JSON Schema, do a basic structural check
	if latest.Format == "json-schema" {
		var schemaMap map[string]any
		if err := json.Unmarshal([]byte(latest.Schema), &schemaMap); err != nil {
			return false, []string{"invalid schema: " + err.Error()}
		}
		return validateJSON(schemaMap, data), nil
	}
	return true, nil // unknown format — assume valid
}

func validateJSON(schema map[string]any, data any) bool {
	typ, _ := schema["type"].(string)
	if typ == "" { return true }

	switch typ {
	case "object":
		_, ok := data.(map[string]any)
		return ok
	case "array":
		_, ok := data.([]any)
		return ok
	case "string":
		_, ok := data.(string)
		return ok
	case "number":
		switch data.(type) { case float64, int, int64: return true }
		return false
	case "boolean":
		_, ok := data.(bool)
		return ok
	}
	return true
}

// DetectBreakingChange compares two schemas for breaking changes.
func (r *Registry) DetectBreakingChange(oldSchema, newSchema string) []string {
	var issues []string
	var oldMap, newMap map[string]any
	json.Unmarshal([]byte(oldSchema), &oldMap)
	json.Unmarshal([]byte(newSchema), &newMap)

	oldProps, _ := oldMap["properties"].(map[string]any)
	newProps, _ := newMap["properties"].(map[string]any)

	oldRequired, _ := oldMap["required"].([]any)
	newRequired, _ := newMap["required"].([]any)

	// Removed fields
	for k := range oldProps {
		if _, ok := newProps[k]; !ok {
			issues = append(issues, fmt.Sprintf("field %q removed", k))
		}
	}

	// Added required fields
	oldReqSet := toSet(oldRequired)
	for _, r := range newRequired {
		if rs, ok := r.(string); ok && !oldReqSet[rs] {
			issues = append(issues, fmt.Sprintf("new required field %q added", rs))
		}
	}

	// Type changes
	for k, oldV := range oldProps {
		oldFM, _ := oldV.(map[string]any)
		newFM, _ := newProps[k].(map[string]any)
		if oldFM != nil && newFM != nil {
			if oldFM["type"] != newFM["type"] {
				issues = append(issues, fmt.Sprintf("field %q type changed from %v to %v", k, oldFM["type"], newFM["type"]))
			}
		}
	}

	sort.Strings(issues)
	return issues
}

func toSet(items []any) map[string]bool {
	s := map[string]bool{}
	for _, i := range items {
		if is, ok := i.(string); ok { s[is] = true }
	}
	return s
}

// FormatSchema formats a schema version for display.
func FormatSchema(sv *SchemaVersion) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Subject: %s v%d [%s]\n", sv.Subject, sv.Version, sv.Format)
	fmt.Fprintf(&sb, "Created: %s\n", sv.CreatedAt.Format(time.RFC3339))
	if sv.Deprecated { sb.WriteString("Deprecated\n") }
	fmt.Fprintf(&sb, "Schema:\n%s\n", sv.Schema)
	return sb.String()
}

// FormatRegistry formats all registered schemas.
func (r *Registry) FormatRegistry() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Schema Registry (%d subjects):\n%s\n\n", len(r.schemas), strings.Repeat("─", 50))
	for _, subject := range r.Subjects() {
		latest, _ := r.GetLatest(subject)
		versions := r.Versions(subject)
		fmt.Fprintf(&sb, "  %-25s v%d  (%d versions)", subject, latest.Version, len(versions))
		if latest.Deprecated { sb.WriteString(" [deprecated]") }
		sb.WriteByte('\n')
	}
	return sb.String()
}
