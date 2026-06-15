// Package manifest implements project manifest management for Lumen.toml
// and YAML project files. It supports dependency declaration, script
// definitions, target environments, version constraints, and manifest
// formatting (TOML output).
//
// Usage:
//
//	m, err := manifest.ParseFile("Lumen.toml")
//	if err != nil { ... }
//	fmt.Println(m.Name, m.Version)
//	for _, dep := range m.Dependencies {
//	    fmt.Printf("  %s %s\n", dep.Name, dep.Version)
//	}
package manifest

import (
	"fmt"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Manifest
// ---------------------------------------------------------------------------

// Manifest is the top-level project descriptor.
type Manifest struct {
	// Name is the project name (required).
	Name string `toml:"name" json:"name" yaml:"name"`
	// Version is the project version (semver recommended).
	Version string `toml:"version" json:"version" yaml:"version"`
	// Description is a short project summary.
	Description string `toml:"description" json:"description" yaml:"description"`
	// License is the SPDX license identifier.
	License string `toml:"license" json:"license" yaml:"license"`
	// Authors lists the project authors.
	Authors []string `toml:"authors" json:"authors" yaml:"authors"`
	// Repository is the source repository URL.
	Repository string `toml:"repository" json:"repository" yaml:"repository"`
	// Homepage is the project homepage URL.
	Homepage string `toml:"homepage" json:"homepage" yaml:"homepage"`
	// Documentation is the docs URL.
	Documentation string `toml:"documentation" json:"documentation" yaml:"documentation"`
	// Keywords are search tags.
	Keywords []string `toml:"keywords" json:"keywords" yaml:"keywords"`
	// Dependencies lists runtime dependencies.
	Dependencies []Dependency `toml:"dependencies" json:"dependencies" yaml:"dependencies"`
	// DevDependencies lists development-only dependencies.
	DevDependencies []Dependency `toml:"dev-dependencies" json:"dev-dependencies" yaml:"dev-dependencies"`
	// Scripts are named command shortcuts.
	Scripts []Script `toml:"scripts" json:"scripts" yaml:"scripts"`
	// Environments define target deployment environments.
	Environments []Environment `toml:"environments" json:"environments" yaml:"environments"`
	// Build contains build configuration.
	Build *BuildConfig `toml:"build" json:"build" yaml:"build"`
}

// Dependency is an external package or module dependency.
type Dependency struct {
	// Name is the package name.
	Name string `toml:"name" json:"name" yaml:"name"`
	// Version is a semver constraint (e.g. "^1.2.3", ">=2.0.0 <3.0.0").
	Version string `toml:"version" json:"version" yaml:"version"`
	// Source is an optional Git URL or registry path.
	Source string `toml:"source" json:"source" yaml:"source"`
	// Optional indicates the dependency is not required.
	Optional bool `toml:"optional" json:"optional" yaml:"optional"`
	// Features enables specific feature flags.
	Features []string `toml:"features" json:"features" yaml:"features"`
}

// Script is a named command that can be run via a task runner.
type Script struct {
	// Name is the script identifier (e.g. "build", "test", "deploy").
	Name string `toml:"name" json:"name" yaml:"name"`
	// Command is the shell command to execute.
	Command string `toml:"command" json:"command" yaml:"command"`
	// Description explains what the script does.
	Description string `toml:"description" json:"description" yaml:"description"`
	// WorkingDir overrides the working directory.
	WorkingDir string `toml:"working_dir" json:"working_dir" yaml:"working_dir"`
	// Env sets additional environment variables.
	Env map[string]string `toml:"env" json:"env" yaml:"env"`
}

// Environment describes a target deployment environment (dev, staging, prod).
type Environment struct {
	// Name is the environment name.
	Name string `toml:"name" json:"name" yaml:"name"`
	// Variables are key-value configuration for this environment.
	Variables map[string]string `toml:"variables" json:"variables" yaml:"variables"`
	// Secrets are references to secret keys (not values).
	Secrets []string `toml:"secrets" json:"secrets" yaml:"secrets"`
	// Regions lists cloud regions for deployment.
	Regions []string `toml:"regions" json:"regions" yaml:"regions"`
	// Endpoints are service endpoint URLs.
	Endpoints map[string]string `toml:"endpoints" json:"endpoints" yaml:"endpoints"`
}

// BuildConfig defines build settings.
type BuildConfig struct {
	// Command is the build command override.
	Command string `toml:"command" json:"command" yaml:"command"`
	// Output is the output binary name.
	Output string `toml:"output" json:"output" yaml:"output"`
	// TargetOS is the target operating system.
	TargetOS string `toml:"target_os" json:"target_os" yaml:"target_os"`
	// TargetArch is the target architecture.
	TargetArch string `toml:"target_arch" json:"target_arch" yaml:"target_arch"`
	// Flags are additional compiler/linker flags.
	Flags []string `toml:"flags" json:"flags" yaml:"flags"`
}

// ---------------------------------------------------------------------------
// Default manifest
// ---------------------------------------------------------------------------

// New creates an empty manifest with default values.
func New(name string) *Manifest {
	return &Manifest{
		Name:    name,
		Version: "0.1.0",
	}
}

// AddDependency adds a runtime dependency.
func (m *Manifest) AddDependency(name, version string) {
	m.Dependencies = append(m.Dependencies, Dependency{
		Name: name, Version: version,
	})
}

// AddDevDependency adds a dev dependency.
func (m *Manifest) AddDevDependency(name, version string) {
	m.DevDependencies = append(m.DevDependencies, Dependency{
		Name: name, Version: version,
	})
}

// AddScript adds a named script.
func (m *Manifest) AddScript(name, command string) {
	m.Scripts = append(m.Scripts, Script{
		Name: name, Command: command,
	})
}

// AddEnvironment adds a deployment environment.
func (m *Manifest) AddEnvironment(name string) *Environment {
	env := Environment{
		Name:      name,
		Variables: make(map[string]string),
		Endpoints: make(map[string]string),
	}
	m.Environments = append(m.Environments, env)
	return &m.Environments[len(m.Environments)-1]
}

// FindScript returns a script by name, or nil.
func (m *Manifest) FindScript(name string) *Script {
	for i := range m.Scripts {
		if m.Scripts[i].Name == name {
			return &m.Scripts[i]
		}
	}
	return nil
}

// FindDependency returns a dependency by name, or nil.
func (m *Manifest) FindDependency(name string) *Dependency {
	for i := range m.Dependencies {
		if m.Dependencies[i].Name == name {
			return &m.Dependencies[i]
		}
	}
	return nil
}

// FindEnvironment returns an environment by name, or nil.
func (m *Manifest) FindEnvironment(name string) *Environment {
	for i := range m.Environments {
		if m.Environments[i].Name == name {
			return &m.Environments[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Version constraint parsing
// ---------------------------------------------------------------------------

// VersionConstraint represents a parsed version requirement.
type VersionConstraint struct {
	Raw     string
	Min     string
	Max     string
	MinIncl bool
	MaxIncl bool
	Caret   string // "^1.2.3" style
	Tilde   string // "~1.2.3" style
	Exact   string // "=1.2.3" or plain "1.2.3"
}

// ParseVersionConstraint parses a version string into a constraint.
func ParseVersionConstraint(raw string) (*VersionConstraint, error) {
	vc := &VersionConstraint{Raw: raw}
	s := strings.TrimSpace(raw)

	if s == "" || s == "*" {
		return vc, nil // any version
	}

	if strings.HasPrefix(s, "^") {
		vc.Caret = s[1:]
		return vc, nil
	}
	if strings.HasPrefix(s, "~") {
		vc.Tilde = s[1:]
		return vc, nil
	}
	if strings.HasPrefix(s, "=") {
		vc.Exact = s[1:]
		return vc, nil
	}

	// Range: ">=1.0.0 <2.0.0" or "1.0.0 - 2.0.0"
	parts := strings.Fields(s)
	for i, p := range parts {
		if p == "-" && i > 0 && i < len(parts)-1 {
			vc.Min = parts[i-1]
			vc.MinIncl = true
			vc.Max = parts[i+1]
			vc.MaxIncl = true
			return vc, nil
		}
	}

	for _, p := range parts {
		switch {
		case strings.HasPrefix(p, ">="):
			vc.Min = p[2:]
			vc.MinIncl = true
		case strings.HasPrefix(p, ">"):
			vc.Min = p[1:]
			vc.MinIncl = false
		case strings.HasPrefix(p, "<="):
			vc.Max = p[2:]
			vc.MaxIncl = true
		case strings.HasPrefix(p, "<"):
			vc.Max = p[1:]
			vc.MaxIncl = false
		default:
			vc.Exact = p
		}
	}

	return vc, nil
}

// SatisfiedBy checks whether a concrete version satisfies this constraint.
// This is a simplified check; full semver comparison is not implemented.
func (vc *VersionConstraint) SatisfiedBy(version string) bool {
	if vc.Raw == "" || vc.Raw == "*" {
		return true
	}
	if vc.Exact != "" {
		return vc.Exact == version
	}
	if vc.Caret != "" {
		return version >= vc.Caret && majorOf(version) == majorOf(vc.Caret)
	}
	if vc.Tilde != "" {
		return version >= vc.Tilde && majorOf(version) == majorOf(vc.Tilde) &&
			minorOf(version) >= minorOf(vc.Tilde)
	}
	if vc.Min != "" {
		if vc.MinIncl && version < vc.Min {
			return false
		}
		if !vc.MinIncl && version <= vc.Min {
			return false
		}
	}
	if vc.Max != "" {
		if vc.MaxIncl && version > vc.Max {
			return false
		}
		if !vc.MaxIncl && version >= vc.Max {
			return false
		}
	}
	return true
}

func majorOf(v string) string {
	parts := strings.SplitN(v, ".", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return v
}

func minorOf(v string) string {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) > 1 {
		return parts[0] + "." + parts[1]
	}
	return v
}

// ---------------------------------------------------------------------------
// FormatManifest — TOML-like output
// ---------------------------------------------------------------------------

// FormatManifestOptions controls formatting.
type FormatManifestOptions struct {
	Indent string
}

// DefaultFormatOptions returns sensible defaults.
func DefaultFormatOptions() FormatManifestOptions {
	return FormatManifestOptions{Indent: "  "}
}

// FormatManifest produces a TOML-like string representation of the manifest.
func FormatManifest(m *Manifest, opts FormatManifestOptions) string {
	var sb strings.Builder
	ind := opts.Indent

	sb.WriteString(fmt.Sprintf("name = %q\n", m.Name))
	sb.WriteString(fmt.Sprintf("version = %q\n", m.Version))

	if m.Description != "" {
		sb.WriteString(fmt.Sprintf("description = %q\n", m.Description))
	}
	if m.License != "" {
		sb.WriteString(fmt.Sprintf("license = %q\n", m.License))
	}

	if len(m.Authors) > 0 {
		sb.WriteString("authors = [")
		for i, a := range m.Authors {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%q", a))
		}
		sb.WriteString("]\n")
	}

	if m.Repository != "" {
		sb.WriteString(fmt.Sprintf("repository = %q\n", m.Repository))
	}
	if m.Homepage != "" {
		sb.WriteString(fmt.Sprintf("homepage = %q\n", m.Homepage))
	}

	if len(m.Keywords) > 0 {
		sb.WriteString("keywords = [")
		for i, k := range m.Keywords {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%q", k))
		}
		sb.WriteString("]\n")
	}

	// Dependencies.
	if len(m.Dependencies) > 0 {
		sb.WriteString("\n[dependencies]\n")
		for _, d := range m.Dependencies {
			sb.WriteString(ind)
			sb.WriteString(fmt.Sprintf("%s = %q\n", d.Name, d.Version))
		}
	}

	// Dev dependencies.
	if len(m.DevDependencies) > 0 {
		sb.WriteString("\n[dev-dependencies]\n")
		for _, d := range m.DevDependencies {
			sb.WriteString(ind)
			sb.WriteString(fmt.Sprintf("%s = %q\n", d.Name, d.Version))
		}
	}

	// Scripts.
	if len(m.Scripts) > 0 {
		sb.WriteString("\n[scripts]\n")
		for _, s := range m.Scripts {
			sb.WriteString(ind)
			sb.WriteString(fmt.Sprintf("%s = %q", s.Name, s.Command))
			if s.Description != "" {
				sb.WriteString(fmt.Sprintf("  # %s", s.Description))
			}
			sb.WriteByte('\n')
		}
	}

	// Environments.
	for _, env := range m.Environments {
		sb.WriteString(fmt.Sprintf("\n[environments.%s]\n", env.Name))
		if len(env.Variables) > 0 {
			for k, v := range env.Variables {
				sb.WriteString(ind)
				sb.WriteString(fmt.Sprintf("%s = %q\n", k, v))
			}
		}
		if len(env.Secrets) > 0 {
			sb.WriteString(ind + "secrets = [")
			for i, s := range env.Secrets {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%q", s))
			}
			sb.WriteString("]\n")
		}
		if len(env.Regions) > 0 {
			sb.WriteString(ind + "regions = [")
			for i, r := range env.Regions {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%q", r))
			}
			sb.WriteString("]\n")
		}
		if len(env.Endpoints) > 0 {
			for k, v := range env.Endpoints {
				sb.WriteString(ind)
				sb.WriteString(fmt.Sprintf("%s = %q\n", k, v))
			}
		}
	}

	if m.Build != nil {
		sb.WriteString("\n[build]\n")
		if m.Build.Command != "" {
			sb.WriteString(ind + fmt.Sprintf("command = %q\n", m.Build.Command))
		}
		if m.Build.Output != "" {
			sb.WriteString(ind + fmt.Sprintf("output = %q\n", m.Build.Output))
		}
		if m.Build.TargetOS != "" {
			sb.WriteString(ind + fmt.Sprintf("target_os = %q\n", m.Build.TargetOS))
		}
		if m.Build.TargetArch != "" {
			sb.WriteString(ind + fmt.Sprintf("target_arch = %q\n", m.Build.TargetArch))
		}
		if len(m.Build.Flags) > 0 {
			sb.WriteString(ind + "flags = [")
			for i, f := range m.Build.Flags {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%q", f))
			}
			sb.WriteString("]\n")
		}
	}

	return sb.String()
}

// ---------------------------------------------------------------------------
// Simple TOML parser (enough for manifest files)
// ---------------------------------------------------------------------------

// ParseTOML parses a simplified TOML string into a Manifest.
// This is a minimal parser that handles the Lumen.toml format.
func ParseTOML(input string) (*Manifest, error) {
	m := &Manifest{}
	lines := strings.Split(input, "\n")
	var currentSection string
	var currentEnv string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Section header.
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			sec := line[1 : len(line)-1]
			parts := strings.SplitN(sec, ".", 2)
			currentSection = parts[0]
			currentEnv = ""
			if currentSection == "environments" && len(parts) > 1 {
				currentEnv = parts[1]
				currentSection = "environments"
			}
			continue
		}

		// Key = value.
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		val = strings.Trim(val, "\"'")

		switch currentSection {
		case "":
			switch key {
			case "name":
				m.Name = val
			case "version":
				m.Version = val
			case "description":
				m.Description = val
			case "license":
				m.License = val
			case "repository":
				m.Repository = val
			case "homepage":
				m.Homepage = val
			}
		case "dependencies":
			m.Dependencies = append(m.Dependencies, Dependency{Name: key, Version: val})
		case "dev-dependencies":
			m.DevDependencies = append(m.DevDependencies, Dependency{Name: key, Version: val})
		case "scripts":
			m.Scripts = append(m.Scripts, Script{Name: key, Command: val})
		case "environments":
			env := m.FindEnvironment(currentEnv)
			if env == nil {
				env = m.AddEnvironment(currentEnv)
			}
			env.Variables[key] = val
		case "build":
			if m.Build == nil {
				m.Build = &BuildConfig{}
			}
			switch key {
			case "command":
				m.Build.Command = val
			case "output":
				m.Build.Output = val
			case "target_os":
				m.Build.TargetOS = val
			case "target_arch":
				m.Build.TargetArch = val
			}
		}
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// ParseFile — convenience
// ---------------------------------------------------------------------------

// ParseFile reads and parses a manifest file. Currently only supports TOML
// format. The caller should read the file content and pass it here.
func ParseFile(path string, content string) (*Manifest, error) {
	if strings.HasSuffix(path, ".toml") {
		return ParseTOML(content)
	}
	return nil, fmt.Errorf("manifest: unsupported format for %q", path)
}

// ---------------------------------------------------------------------------
// Sort helpers
// ---------------------------------------------------------------------------

// SortDependencies sorts dependencies alphabetically by name.
func SortDependencies(deps []Dependency) {
	sort.Slice(deps, func(i, j int) bool {
		return deps[i].Name < deps[j].Name
	})
}

// SortScripts sorts scripts alphabetically by name.
func SortScripts(scripts []Script) {
	sort.Slice(scripts, func(i, j int) bool {
		return scripts[i].Name < scripts[j].Name
	})
}
