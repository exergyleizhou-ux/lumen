// Package linter implements a code linter engine with rule registration,
// AST-based checks, severity levels, auto-fix suggestions, configuration
// via .linter.yml, and formatted file:line output.
package linter

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Severity represents the severity level of a lint issue.
type Severity int

const (
	// SevInfo is informational only.
	SevInfo Severity = iota
	// SevWarning indicates a potential problem.
	SevWarning
	// SevError indicates a definite problem.
	SevError
	// SevFatal indicates a must-fix issue.
	SevFatal
)

var severityStrings = map[Severity]string{
	SevInfo:    "info",
	SevWarning: "warning",
	SevError:   "error",
	SevFatal:   "fatal",
}

func (s Severity) String() string {
	if str, ok := severityStrings[s]; ok {
		return str
	}
	return "unknown"
}

// MarshalYAML implements yaml.Marshaler.
func (s Severity) MarshalYAML() (interface{}, error) {
	return s.String(), nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (s *Severity) UnmarshalYAML(value *yaml.Node) error {
	var str string
	if err := value.Decode(&str); err != nil {
		return err
	}
	switch strings.ToLower(str) {
	case "info":
		*s = SevInfo
	case "warning":
		*s = SevWarning
	case "error":
		*s = SevError
	case "fatal":
		*s = SevFatal
	default:
		return fmt.Errorf("unknown severity: %s", str)
	}
	return nil
}

// Issue represents a single lint issue found in code.
type Issue struct {
	Rule     string   `json:"rule" yaml:"rule"`
	Severity Severity `json:"severity" yaml:"severity"`
	File     string   `json:"file" yaml:"file"`
	Line     int      `json:"line" yaml:"line"`
	Column   int      `json:"column" yaml:"column"`
	Message  string   `json:"message" yaml:"message"`
	Snippet  string   `json:"snippet,omitempty" yaml:"snippet,omitempty"`
	Fix      *Fix     `json:"fix,omitempty" yaml:"fix,omitempty"`
}

// Fix represents an auto-fix suggestion for a lint issue.
type Fix struct {
	Description string `json:"description" yaml:"description"`
	Replacement string `json:"replacement" yaml:"replacement"`
	StartLine   int    `json:"start_line" yaml:"start_line"`
	EndLine     int    `json:"end_line" yaml:"end_line"`
	StartCol    int    `json:"start_col" yaml:"start_col"`
	EndCol      int    `json:"end_col" yaml:"end_col"`
}

// Rule defines a lint check that can be applied to source files.
type Rule struct {
	Name        string
	Description string
	Severity    Severity
	Enabled     bool
	Check       func(file *ast.File, fset *token.FileSet, src []byte) []Issue
	Config      map[string]interface{}
}

// RuleRegistry holds all registered lint rules.
type RuleRegistry struct {
	mu    sync.RWMutex
	rules map[string]*Rule
	order []string
}

// NewRuleRegistry creates a new rule registry.
func NewRuleRegistry() *RuleRegistry {
	return &RuleRegistry{
		rules: make(map[string]*Rule),
		order: make([]string, 0),
	}
}

// Register adds a rule to the registry.
func (rr *RuleRegistry) Register(rule *Rule) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	if _, exists := rr.rules[rule.Name]; !exists {
		rr.order = append(rr.order, rule.Name)
	}
	rr.rules[rule.Name] = rule
}

// Get returns a rule by name.
func (rr *RuleRegistry) Get(name string) (*Rule, bool) {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	r, ok := rr.rules[name]
	return r, ok
}

// List returns all registered rule names in registration order.
func (rr *RuleRegistry) List() []string {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	result := make([]string, len(rr.order))
	copy(result, rr.order)
	return result
}

// All returns all registered rules.
func (rr *RuleRegistry) All() []*Rule {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	result := make([]*Rule, 0, len(rr.order))
	for _, name := range rr.order {
		result = append(result, rr.rules[name])
	}
	return result
}

// Enable enables a rule by name.
func (rr *RuleRegistry) Enable(name string) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	if r, ok := rr.rules[name]; ok {
		r.Enabled = true
	}
}

// Disable disables a rule by name.
func (rr *RuleRegistry) Disable(name string) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	if r, ok := rr.rules[name]; ok {
		r.Enabled = false
	}
}

// LinterConfig represents the .linter.yml configuration.
type LinterConfig struct {
	Version   string                 `yaml:"version"`
	Rules     map[string]RuleConfig  `yaml:"rules"`
	Exclude   []string               `yaml:"exclude"`
	Include   []string               `yaml:"include"`
	Settings  map[string]interface{} `yaml:"settings"`
	MaxIssues int                    `yaml:"max_issues"`
}

// RuleConfig holds per-rule configuration.
type RuleConfig struct {
	Enabled  bool                   `yaml:"enabled"`
	Severity string                 `yaml:"severity"`
	Options  map[string]interface{} `yaml:"options"`
}

// DefaultConfig returns a default linter configuration.
func DefaultConfig() *LinterConfig {
	return &LinterConfig{
		Version:   "1",
		MaxIssues: 1000,
		Rules:     make(map[string]RuleConfig),
		Exclude:   make([]string, 0),
		Include:   []string{"."},
		Settings:  make(map[string]interface{}),
	}
}

// LoadConfig loads configuration from a YAML reader.
func LoadConfig(r io.Reader) (*LinterConfig, error) {
	cfg := DefaultConfig()
	dec := yaml.NewDecoder(r)
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	return cfg, nil
}

// LoadConfigFile loads configuration from a file path.
func LoadConfigFile(path string) (*LinterConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, err
	}
	defer f.Close()
	return LoadConfig(f)
}

// SaveConfig writes configuration to a YAML writer.
func (c *LinterConfig) SaveConfig(w io.Writer) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	return enc.Encode(c)
}

// Linter is the main linting engine.
type Linter struct {
	registry *RuleRegistry
	config   *LinterConfig
	issues   []Issue
	mu       sync.Mutex
}

// New creates a new Linter with the given configuration.
func New(cfg *LinterConfig) *Linter {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	l := &Linter{
		registry: NewRuleRegistry(),
		config:   cfg,
		issues:   make([]Issue, 0),
	}
	l.registerBuiltinRules()
	l.applyConfig()
	return l
}

// Registry returns the rule registry.
func (l *Linter) Registry() *RuleRegistry {
	return l.registry
}

// Config returns the current configuration.
func (l *Linter) Config() *LinterConfig {
	return l.config
}

// applyConfig applies configuration to the rule registry.
func (l *Linter) applyConfig() {
	for name, rc := range l.config.Rules {
		if _, ok := l.registry.Get(name); ok {
			if rc.Enabled {
				l.registry.Enable(name)
			} else {
				l.registry.Disable(name)
			}
		}
	}
}

// LintDir lints all Go files in a directory recursively.
func (l *Linter) LintDir(dir string) ([]Issue, error) {
	var allIssues []Issue

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base != "." && strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			if base == "vendor" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if l.shouldExclude(path) {
			return nil
		}
		issues, err := l.LintFile(path)
		if err != nil {
			return nil // skip files that can't be parsed
		}
		allIssues = append(allIssues, issues...)
		return nil
	})

	if err != nil {
		return nil, err
	}

	l.mu.Lock()
	if len(allIssues) > l.config.MaxIssues {
		allIssues = allIssues[:l.config.MaxIssues]
	}
	l.issues = allIssues
	l.mu.Unlock()

	sort.Slice(allIssues, func(i, j int) bool {
		if allIssues[i].File != allIssues[j].File {
			return allIssues[i].File < allIssues[j].File
		}
		return allIssues[i].Line < allIssues[j].Line
	})

	return allIssues, nil
}

// LintFiles lints a list of specific files.
func (l *Linter) LintFiles(files []string) ([]Issue, error) {
	var allIssues []Issue
	for _, f := range files {
		if l.shouldExclude(f) {
			continue
		}
		issues, err := l.LintFile(f)
		if err != nil {
			continue
		}
		allIssues = append(allIssues, issues...)
	}
	l.mu.Lock()
	l.issues = allIssues
	l.mu.Unlock()
	return allIssues, nil
}

// LintFile lints a single Go source file.
func (l *Linter) LintFile(filename string) ([]Issue, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	src, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return l.lintAST(f, fset, src, filename), nil
}

// lintAST runs all enabled rules against a parsed file.
func (l *Linter) lintAST(f *ast.File, fset *token.FileSet, src []byte, filename string) []Issue {
	var issues []Issue
	for _, rule := range l.registry.All() {
		if !rule.Enabled {
			continue
		}
		ruleIssues := rule.Check(f, fset, src)
		for i := range ruleIssues {
			ruleIssues[i].File = filename
			ruleIssues[i].Rule = rule.Name
			if ruleIssues[i].Severity == SevInfo {
				ruleIssues[i].Severity = rule.Severity
			}
		}
		issues = append(issues, ruleIssues...)
		if len(issues) >= l.config.MaxIssues {
			break
		}
	}
	return issues
}

// shouldExclude checks if a path matches the exclude patterns.
func (l *Linter) shouldExclude(path string) bool {
	for _, pattern := range l.config.Exclude {
		matched, err := filepath.Match(pattern, filepath.Base(path))
		if err == nil && matched {
			return true
		}
		// Also try glob matching
		matched, err = filepath.Match(pattern, path)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// FormatReport formats issues as a human-readable report with file:line output.
func FormatReport(issues []Issue, w io.Writer) {
	if len(issues) == 0 {
		fmt.Fprintln(w, "No issues found.")
		return
	}

	fmt.Fprintf(w, "Found %d issue(s):\n\n", len(issues))

	// Group by file
	byFile := make(map[string][]Issue)
	for _, issue := range issues {
		byFile[issue.File] = append(byFile[issue.File], issue)
	}

	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)

	for _, file := range files {
		fileIssues := byFile[file]
		fmt.Fprintf(w, "%s:\n", file)
		for _, issue := range fileIssues {
			fmt.Fprintf(w, "  %d:%d [%s] %s (%s)\n",
				issue.Line, issue.Column,
				issue.Severity.String(), issue.Message, issue.Rule)
			if issue.Snippet != "" {
				for _, line := range strings.Split(issue.Snippet, "\n") {
					fmt.Fprintf(w, "    > %s\n", line)
				}
			}
			if issue.Fix != nil {
				fmt.Fprintf(w, "    Fix: %s\n", issue.Fix.Description)
			}
		}
		fmt.Fprintln(w)
	}
}

// FormatReportJSON outputs issues as JSON.
func FormatReportJSON(issues []Issue, w io.Writer) error {
	// Simple JSON encoding without importing encoding/json
	// (encoding/json is in stdlib so it's fine, but let me use a manual approach)
	fmt.Fprintf(w, "[\n")
	for i, issue := range issues {
		comma := ","
		if i == len(issues)-1 {
			comma = ""
		}
		fmt.Fprintf(w, `  {"rule":%q,"severity":%q,"file":%q,"line":%d,"column":%d,"message":%q}%s`,
			issue.Rule, issue.Severity.String(), issue.File,
			issue.Line, issue.Column, issue.Message, comma)
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "]\n")
	return nil
}

// registerBuiltinRules adds all built-in lint rules to the registry.
func (l *Linter) registerBuiltinRules() {
	// 1. unused-var
	l.registry.Register(&Rule{
		Name:        "unused-var",
		Description: "Detects declared but unused variables",
		Severity:    SevWarning,
		Enabled:     true,
		Check:       checkUnusedVar,
	})

	// 2. too-complex
	l.registry.Register(&Rule{
		Name:        "too-complex",
		Description: "Detects functions with high cyclomatic complexity",
		Severity:    SevWarning,
		Enabled:     true,
		Config:      map[string]interface{}{"max_complexity": 15},
		Check:       checkTooComplex,
	})

	// 3. missing-doc
	l.registry.Register(&Rule{
		Name:        "missing-doc",
		Description: "Exported identifiers should have documentation comments",
		Severity:    SevInfo,
		Enabled:     true,
		Check:       checkMissingDoc,
	})

	// 4. long-line
	l.registry.Register(&Rule{
		Name:        "long-line",
		Description: "Detects lines exceeding a maximum length",
		Severity:    SevInfo,
		Enabled:     true,
		Config:      map[string]interface{}{"max_length": 120},
		Check:       checkLongLine,
	})

	// 5. deep-nesting
	l.registry.Register(&Rule{
		Name:        "deep-nesting",
		Description: "Detects deeply nested control flow",
		Severity:    SevWarning,
		Enabled:     true,
		Config:      map[string]interface{}{"max_depth": 4},
		Check:       checkDeepNesting,
	})

	// 6. magic-number
	l.registry.Register(&Rule{
		Name:        "magic-number",
		Description: "Detects unexplained numeric literals",
		Severity:    SevInfo,
		Enabled:     true,
		Check:       checkMagicNumber,
	})

	// 7. error-check
	l.registry.Register(&Rule{
		Name:        "error-check",
		Description: "Detects unchecked error return values",
		Severity:    SevError,
		Enabled:     true,
		Check:       checkErrorCheck,
	})

	// 8. naming-convention
	l.registry.Register(&Rule{
		Name:        "naming-convention",
		Description: "Checks identifier naming conventions",
		Severity:    SevInfo,
		Enabled:     true,
		Check:       checkNamingConvention,
	})

	// 9. import-order
	l.registry.Register(&Rule{
		Name:        "import-order",
		Description: "Checks that imports are properly grouped and sorted",
		Severity:    SevInfo,
		Enabled:     true,
		Check:       checkImportOrder,
	})

	// 10. empty-catch
	l.registry.Register(&Rule{
		Name:        "empty-block",
		Description: "Detects empty blocks that may indicate missing code",
		Severity:    SevInfo,
		Enabled:     true,
		Check:       checkEmptyBlock,
	})

	// 11. var-decl
	l.registry.Register(&Rule{
		Name:        "var-decl",
		Description: "Prefer short variable declarations where possible",
		Severity:    SevInfo,
		Enabled:     true,
		Check:       checkVarDecl,
	})

	// 12. range-issue
	l.registry.Register(&Rule{
		Name:        "range-issue",
		Description: "Detects common range loop pitfalls",
		Severity:    SevWarning,
		Enabled:     true,
		Check:       checkRangeIssue,
	})

	// 13. context-propagation
	l.registry.Register(&Rule{
		Name:        "context-propagation",
		Description: "Checks that context.Context is properly propagated",
		Severity:    SevInfo,
		Enabled:     true,
		Check:       checkContextPropagation,
	})
}

// ---- Built-in rule implementations ----

func checkUnusedVar(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	// Simple heuristic: check for declared variables in short form that are never used
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			if node.Tok == token.DEFINE {
				for _, lhs := range node.Lhs {
					if ident, ok := lhs.(*ast.Ident); ok {
						if ident.Name == "_" {
							continue
						}
						// Check if the identifier is used after this point
						used := isIdentUsedAfter(file, ident, node.Pos())
						if !used && ident.Name != "err" {
							pos := fset.Position(ident.Pos())
							issues = append(issues, Issue{
								Line:    pos.Line,
								Column:  pos.Column,
								Message: fmt.Sprintf("variable '%s' is declared but may not be used", ident.Name),
								Fix:     nil,
							})
						}
					}
				}
			}
		}
		return true
	})
	return issues
}

func isIdentUsedAfter(file *ast.File, target *ast.Ident, after token.Pos) bool {
	used := false
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		if n.Pos() <= after {
			return true
		}
		if ident, ok := n.(*ast.Ident); ok {
			if ident.Name == target.Name && ident != target && ident.Obj == target.Obj {
				used = true
				return false
			}
		}
		return !used
	})
	return used
}

func checkTooComplex(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	maxComplexity := 15

	ast.Inspect(file, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncDecl); ok {
			complexity := computeCyclomaticComplexity(fn)
			if complexity > maxComplexity {
				pos := fset.Position(fn.Pos())
				name := fn.Name.Name
				if fn.Recv != nil && len(fn.Recv.List) > 0 {
					recv := typeExprToString(fn.Recv.List[0].Type)
					name = recv + "." + name
				}
				issues = append(issues, Issue{
					Line:   pos.Line,
					Column: pos.Column,
					Message: fmt.Sprintf("function '%s' has cyclomatic complexity %d (max %d)",
						name, complexity, maxComplexity),
				})
			}
		}
		return true
	})
	return issues
}

func computeCyclomaticComplexity(fn *ast.FuncDecl) int {
	complexity := 1 // base complexity
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			complexity++
		case *ast.CaseClause:
			if n.(*ast.CaseClause).List != nil {
				complexity++
			}
		case *ast.BinaryExpr:
			be := n.(*ast.BinaryExpr)
			if be.Op == token.LAND || be.Op == token.LOR {
				complexity++
			}
		case *ast.CommClause:
			complexity++
		}
		return true
	})
	return complexity
}

func typeExprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + typeExprToString(e.X)
	case *ast.SelectorExpr:
		return typeExprToString(e.X) + "." + e.Sel.Name
	default:
		return "type"
	}
}

func checkMissingDoc(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name.IsExported() && d.Doc == nil {
				pos := fset.Position(d.Pos())
				issues = append(issues, Issue{
					Line:    pos.Line,
					Column:  pos.Column,
					Message: fmt.Sprintf("exported function '%s' is missing documentation", d.Name.Name),
				})
			}
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					ts := spec.(*ast.TypeSpec)
					if ts.Name.IsExported() && d.Doc == nil {
						pos := fset.Position(ts.Pos())
						issues = append(issues, Issue{
							Line:    pos.Line,
							Column:  pos.Column,
							Message: fmt.Sprintf("exported type '%s' is missing documentation", ts.Name.Name),
						})
					}
				}
			}
		}
	}
	return issues
}

func checkLongLine(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	maxLength := 120
	lines := strings.Split(string(src), "\n")
	for i, line := range lines {
		if len(line) > maxLength {
			pos := fset.Position(fset.File(file.Pos()).LineStart(i + 1))
			issues = append(issues, Issue{
				Line:    pos.Line,
				Column:  maxLength + 1,
				Message: fmt.Sprintf("line is %d characters long (max %d)", len(line), maxLength),
				Snippet: line[:min(len(line), 80)] + "...",
			})
		}
	}
	return issues
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func checkDeepNesting(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	maxDepth := 4

	var walk func(n ast.Node, depth int)
	walk = func(n ast.Node, depth int) {
		if n == nil {
			return
		}
		switch node := n.(type) {
		case *ast.IfStmt:
			if depth > maxDepth {
				pos := fset.Position(node.Pos())
				issues = append(issues, Issue{
					Line:    pos.Line,
					Column:  pos.Column,
					Message: fmt.Sprintf("nesting depth %d exceeds maximum %d", depth, maxDepth),
				})
			}
			walk(node.Body, depth+1)
			if node.Else != nil {
				walk(node.Else, depth)
			}
			return
		case *ast.ForStmt:
			if depth > maxDepth {
				pos := fset.Position(node.Pos())
				issues = append(issues, Issue{
					Line:    pos.Line,
					Column:  pos.Column,
					Message: fmt.Sprintf("nesting depth %d exceeds maximum %d", depth, maxDepth),
				})
			}
			walk(node.Body, depth+1)
			return
		case *ast.RangeStmt:
			if depth > maxDepth {
				pos := fset.Position(node.Pos())
				issues = append(issues, Issue{
					Line:    pos.Line,
					Column:  pos.Column,
					Message: fmt.Sprintf("nesting depth %d exceeds maximum %d", depth, maxDepth),
				})
			}
			walk(node.Body, depth+1)
			return
		case *ast.BlockStmt:
			for _, stmt := range node.List {
				walk(stmt, depth)
			}
			return
		case *ast.CaseClause:
			for _, stmt := range node.Body {
				walk(stmt, depth)
			}
			return
		}
		// For other nodes, continue walking
		ast.Inspect(n, func(child ast.Node) bool {
			if child != n {
				walk(child, depth)
			}
			return false
		})
	}
	walk(file, 0)
	return issues
}

func checkMagicNumber(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	// Allowed magic numbers
	allowed := map[string]bool{
		"0": true, "1": true, "2": true, "-1": true,
	}

	ast.Inspect(file, func(n ast.Node) bool {
		bl, ok := n.(*ast.BasicLit)
		if !ok || bl.Kind != token.INT {
			return true
		}
		if allowed[bl.Value] {
			return true
		}
		// Check context: is it in a const/var declaration?
		// Skip if parent is a const or var spec or iota
		pos := fset.Position(bl.Pos())
		issues = append(issues, Issue{
			Line:    pos.Line,
			Column:  pos.Column,
			Message: fmt.Sprintf("magic number %s should be replaced with a named constant", bl.Value),
		})
		return true
	})
	return issues
}

func checkErrorCheck(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue

	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		// Look for function calls that return error as last value
		for i, rhs := range assign.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if !ok {
				continue
			}
			// Check if the result includes an error type
			if hasErrorReturn(call) {
				// Check if the corresponding LHS has the error checked
				if i < len(assign.Lhs) {
					lhs := assign.Lhs[i]
					if ident, ok := lhs.(*ast.Ident); ok {
						if ident.Name == "_" {
							pos := fset.Position(assign.Pos())
							issues = append(issues, Issue{
								Line:     pos.Line,
								Column:   pos.Column,
								Message:  "error return value is being discarded with '_'",
								Severity: SevError,
							})
						}
					}
				}
			}
		}
		return true
	})

	// Also check for standalone calls that return errors
	ast.Inspect(file, func(n ast.Node) bool {
		exprStmt, ok := n.(*ast.ExprStmt)
		if !ok {
			return true
		}
		call, ok := exprStmt.X.(*ast.CallExpr)
		if !ok {
			return true
		}
		if hasErrorReturn(call) {
			pos := fset.Position(call.Pos())
			issues = append(issues, Issue{
				Line:     pos.Line,
				Column:   pos.Column,
				Message:  "error return value from function call is not checked",
				Severity: SevError,
			})
		}
		return true
	})

	return issues
}

func hasErrorReturn(call *ast.CallExpr) bool {
	// Try to detect if function is known to return an error
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		// Common patterns: fmt.Fprintf, os.Create, etc.
		name := sel.Sel.Name
		knownErrorFuncs := map[string]bool{
			"Open": true, "Create": true, "ReadFile": true, "WriteFile": true,
			"ReadAll": true, "Parse": true, "Decode": true, "Encode": true,
			"Write": true, "Read": true, "Close": true, "Marshal": true,
			"Unmarshal": true, "Fprintf": true, "Sprintf": false,
			"Append": true, "Scan": true, "Execute": true, "Run": true,
			"Dial": true, "Listen": true, "Accept": true, "Do": true,
			"Get": true, "Post": true, "Send": true, "Receive": true,
			"Commit": true, "Rollback": true, "Query": true, "Exec": true,
		}
		if known, exists := knownErrorFuncs[name]; exists && known {
			return true
		}
	}
	return false
}

func checkNamingConvention(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	snakePattern := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	camelPattern := regexp.MustCompile(`^[A-Z][a-zA-Z0-9]*$`)

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			name := node.Name.Name
			if node.Name.IsExported() {
				if !camelPattern.MatchString(name) {
					pos := fset.Position(node.Pos())
					issues = append(issues, Issue{
						Line:    pos.Line,
						Column:  pos.Column,
						Message: fmt.Sprintf("exported function '%s' should use CamelCase naming", name),
					})
				}
			} else {
				// Unexported: camelCase or snake_case
				if !regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`).MatchString(name) && !snakePattern.MatchString(name) {
					pos := fset.Position(node.Pos())
					issues = append(issues, Issue{
						Line:    pos.Line,
						Column:  pos.Column,
						Message: fmt.Sprintf("unexported function '%s' naming convention not followed", name),
					})
				}
			}
		case *ast.GenDecl:
			if node.Tok == token.CONST || node.Tok == token.VAR {
				for _, spec := range node.Specs {
					vs := spec.(*ast.ValueSpec)
					for _, ident := range vs.Names {
						if ident.IsExported() {
							if !camelPattern.MatchString(ident.Name) {
								pos := fset.Position(ident.Pos())
								issues = append(issues, Issue{
									Line:    pos.Line,
									Column:  pos.Column,
									Message: fmt.Sprintf("exported identifier '%s' should use CamelCase", ident.Name),
								})
							}
						}
					}
				}
			}
		}
		return true
	})
	return issues
}

func checkImportOrder(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	if len(file.Imports) == 0 {
		return issues
	}

	var stdlib, thirdParty, local []string
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if !strings.Contains(path, ".") {
			stdlib = append(stdlib, path)
		} else if strings.Contains(path, "internal") || strings.Contains(path, "pkg") {
			local = append(local, path)
		} else {
			thirdParty = append(thirdParty, path)
		}
	}

	// Check grouping: should be stdlib, blank line, third-party, blank line, local
	pos := fset.Position(file.Imports[0].Pos())
	if len(stdlib) > 0 && len(thirdParty) > 0 {
		// Check if there's a proper separation
		lastStdlib := stdlib[len(stdlib)-1]
		firstThirdParty := thirdParty[0]
		// Simple check: the order should be stdlib then third-party
		lastStdlibIdx := indexOfImport(file, lastStdlib)
		firstThirdPartyIdx := indexOfImport(file, firstThirdParty)
		if lastStdlibIdx >= 0 && firstThirdPartyIdx >= 0 && lastStdlibIdx > firstThirdPartyIdx {
			issues = append(issues, Issue{
				Line:    pos.Line,
				Column:  pos.Column,
				Message: "imports should be grouped: stdlib first, then third-party, then local",
			})
		}
	}

	return issues
}

func indexOfImport(file *ast.File, path string) int {
	for i, imp := range file.Imports {
		impPath := strings.Trim(imp.Path.Value, `"`)
		if impPath == path {
			return i
		}
	}
	return -1
}

func checkEmptyBlock(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BlockStmt:
			if len(node.List) == 0 {
				pos := fset.Position(node.Pos())
				issues = append(issues, Issue{
					Line:    pos.Line,
					Column:  pos.Column,
					Message: "empty block detected; may indicate missing implementation",
				})
			}
		}
		return true
	})
	return issues
}

func checkVarDecl(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.GenDecl:
			if node.Tok == token.VAR {
				for _, spec := range node.Specs {
					vs := spec.(*ast.ValueSpec)
					if vs.Type != nil && len(vs.Values) > 0 {
						// var x Type = value — could be x := value
						pos := fset.Position(node.Pos())
						issues = append(issues, Issue{
							Line:    pos.Line,
							Column:  pos.Column,
							Message: fmt.Sprintf("consider using short declaration ':=' for '%s'", vs.Names[0].Name),
						})
					}
				}
			}
		}
		return true
	})
	return issues
}

func checkRangeIssue(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	ast.Inspect(file, func(n ast.Node) bool {
		rangeStmt, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}
		// Check for range over slice/array where the value variable is unused
		if rangeStmt.Key != nil && rangeStmt.Value != nil {
			if ident, ok := rangeStmt.Value.(*ast.Ident); ok {
				if ident.Name == "_" {
					// Good — explicitly ignoring value
				}
			}
		}
		// Check for range over map where the key is unused
		// Check for mutation of the range variable
		if rangeStmt.Tok == token.DEFINE {
			// Check body for modifications to the range key/value
			ast.Inspect(rangeStmt.Body, func(bodyN ast.Node) bool {
				if assign, ok := bodyN.(*ast.AssignStmt); ok {
					for _, lhs := range assign.Lhs {
						if ident, ok := lhs.(*ast.Ident); ok {
							if rangeStmt.Key != nil {
								if keyIdent, ok := rangeStmt.Key.(*ast.Ident); ok {
									if ident.Name == keyIdent.Name && ident.Obj == keyIdent.Obj {
										pos := fset.Position(assign.Pos())
										issues = append(issues, Issue{
											Line:    pos.Line,
											Column:  pos.Column,
											Message: "modifying range key variable inside loop is ineffective",
										})
									}
								}
							}
							if rangeStmt.Value != nil {
								if valIdent, ok := rangeStmt.Value.(*ast.Ident); ok {
									if ident.Name == valIdent.Name && ident.Obj == valIdent.Obj {
										pos := fset.Position(assign.Pos())
										issues = append(issues, Issue{
											Line:    pos.Line,
											Column:  pos.Column,
											Message: "modifying range value variable inside loop is ineffective",
										})
									}
								}
							}
						}
					}
				}
				return true
			})
		}
		return true
	})
	return issues
}

func checkContextPropagation(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}
		if fn.Type.Params == nil {
			return true
		}
		// Check if function takes context.Context and if it passes it to calls
		hasCtx := false
		ctxParamName := ""
		for _, param := range fn.Type.Params.List {
			if se, ok := param.Type.(*ast.SelectorExpr); ok {
				if ident, ok := se.X.(*ast.Ident); ok {
					if ident.Name == "context" && se.Sel.Name == "Context" {
						hasCtx = true
						if len(param.Names) > 0 {
							ctxParamName = param.Names[0].Name
						}
					}
				}
			}
		}

		if !hasCtx || ctxParamName == "" || fn.Body == nil {
			return true
		}

		// Check if context is passed to function calls inside the body
		ctxUsed := false
		ast.Inspect(fn.Body, func(bodyN ast.Node) bool {
			if call, ok := bodyN.(*ast.CallExpr); ok {
				for _, arg := range call.Args {
					if ident, ok := arg.(*ast.Ident); ok {
						if ident.Name == ctxParamName {
							ctxUsed = true
							return false
						}
					}
				}
			}
			return true
		})

		if !ctxUsed {
			pos := fset.Position(fn.Pos())
			issues = append(issues, Issue{
				Line:    pos.Line,
				Column:  pos.Column,
				Message: fmt.Sprintf("context.Context parameter '%s' is not propagated to any call", ctxParamName),
			})
		}

		return true
	})
	return issues
}

// ---- Helper utilities ----

// SimpleIssuesToString returns a plain text representation of issues.
func SimpleIssuesToString(issues []Issue) string {
	var sb strings.Builder
	FormatReport(issues, &sb)
	return sb.String()
}

// FilterBySeverity returns issues matching a given severity.
func FilterBySeverity(issues []Issue, sev Severity) []Issue {
	var result []Issue
	for _, issue := range issues {
		if issue.Severity == sev {
			result = append(result, issue)
		}
	}
	return result
}

// FilterByRule returns issues for a specific rule.
func FilterByRule(issues []Issue, rule string) []Issue {
	var result []Issue
	for _, issue := range issues {
		if issue.Rule == rule {
			result = append(result, issue)
		}
	}
	return result
}

// CountBySeverity counts issues per severity level.
func CountBySeverity(issues []Issue) map[Severity]int {
	counts := make(map[Severity]int)
	for _, issue := range issues {
		counts[issue.Severity]++
	}
	return counts
}

// CountByRule counts issues per rule.
func CountByRule(issues []Issue) map[string]int {
	counts := make(map[string]int)
	for _, issue := range issues {
		counts[issue.Rule]++
	}
	return counts
}

// HasErrors returns true if there are any error or fatal issues.
func HasErrors(issues []Issue) bool {
	for _, issue := range issues {
		if issue.Severity == SevError || issue.Severity == SevFatal {
			return true
		}
	}
	return false
}

// MergeIssues combines two issue slices, removing duplicates.
func MergeIssues(a, b []Issue) []Issue {
	seen := make(map[string]bool)
	var result []Issue
	for _, issue := range a {
		key := fmt.Sprintf("%s:%d:%d:%s", issue.File, issue.Line, issue.Column, issue.Rule)
		if !seen[key] {
			seen[key] = true
			result = append(result, issue)
		}
	}
	for _, issue := range b {
		key := fmt.Sprintf("%s:%d:%d:%s", issue.File, issue.Line, issue.Column, issue.Rule)
		if !seen[key] {
			seen[key] = true
			result = append(result, issue)
		}
	}
	return result
}

// ApplyFix attempts to apply a fix to source code.
func ApplyFix(src []byte, fix *Fix) ([]byte, error) {
	lines := strings.Split(string(src), "\n")
	if fix.StartLine < 1 || fix.EndLine > len(lines) {
		return nil, fmt.Errorf("fix line range %d-%d is out of bounds (file has %d lines)",
			fix.StartLine, fix.EndLine, len(lines))
	}
	var result []string
	result = append(result, lines[:fix.StartLine-1]...)
	result = append(result, fix.Replacement)
	if fix.EndLine < len(lines) {
		result = append(result, lines[fix.EndLine:]...)
	}
	return []byte(strings.Join(result, "\n")), nil
}

// StyleGuide holds coding style preferences.
type StyleGuide struct {
	IndentSize   int
	UseTabs      bool
	MaxLineLen   int
	RequireDoc   bool
	MaxFuncLines int
}

// DefaultStyleGuide returns sensible defaults.
func DefaultStyleGuide() *StyleGuide {
	return &StyleGuide{
		IndentSize:   4,
		UseTabs:      true,
		MaxLineLen:   120,
		RequireDoc:   true,
		MaxFuncLines: 80,
	}
}

// BatchFix holds a set of fixes that can be applied together.
type BatchFix struct {
	Fixes       []*Fix
	Description string
}

// BatchFixer groups fixes by file.
type BatchFixer struct {
	byFile map[string][]*Fix
}

// NewBatchFixer creates a BatchFixer from issues.
func NewBatchFixer(issues []Issue) *BatchFixer {
	bf := &BatchFixer{byFile: make(map[string][]*Fix)}
	for _, issue := range issues {
		if issue.Fix != nil {
			bf.byFile[issue.File] = append(bf.byFile[issue.File], issue.Fix)
		}
	}
	return bf
}

// ApplyAll applies all fixes to all files.
func (bf *BatchFixer) ApplyAll() map[string][]byte {
	results := make(map[string][]byte)
	for filename, fixes := range bf.byFile {
		src, err := os.ReadFile(filename)
		if err != nil {
			continue
		}
		for _, fix := range fixes {
			var applyErr error
			src, applyErr = ApplyFix(src, fix)
			if applyErr != nil {
				break
			}
		}
		results[filename] = src
	}
	return results
}

// ---- LintResult for structured output ----

// LintResult aggregates all lint output for a run.
type LintResult struct {
	Issues     []Issue     `json:"issues" yaml:"issues"`
	Summary    LintSummary `json:"summary" yaml:"summary"`
	ConfigFile string      `json:"config_file,omitempty" yaml:"config_file,omitempty"`
	Files      int         `json:"files" yaml:"files"`
}

// LintSummary provides a high-level overview.
type LintSummary struct {
	Total      int            `json:"total" yaml:"total"`
	BySeverity map[string]int `json:"by_severity" yaml:"by_severity"`
	ByRule     map[string]int `json:"by_rule" yaml:"by_rule"`
	Errors     int            `json:"errors" yaml:"errors"`
	Warnings   int            `json:"warnings" yaml:"warnings"`
	Infos      int            `json:"infos" yaml:"infos"`
}

// NewLintResult creates a structured result from issues.
func NewLintResult(issues []Issue, configFile string, files int) *LintResult {
	summary := LintSummary{
		Total:      len(issues),
		BySeverity: make(map[string]int),
		ByRule:     make(map[string]int),
	}
	for _, issue := range issues {
		summary.BySeverity[issue.Severity.String()]++
		summary.ByRule[issue.Rule]++
		switch issue.Severity {
		case SevError, SevFatal:
			summary.Errors++
		case SevWarning:
			summary.Warnings++
		case SevInfo:
			summary.Infos++
		}
	}
	return &LintResult{
		Issues:     issues,
		Summary:    summary,
		ConfigFile: configFile,
		Files:      files,
	}
}

// ---- Ignore directive support ----

// IgnoreDirective represents a //nolint comment.
type IgnoreDirective struct {
	Line int
	Rule string // empty means ignore all rules
}

// ParseIgnoreDirectives extracts nolint directives from source.
func ParseIgnoreDirectives(src []byte) []IgnoreDirective {
	var directives []IgnoreDirective
	lines := strings.Split(string(src), "\n")
	nolintRe := regexp.MustCompile(`//\s*nolint:?(\S*)`)
	for i, line := range lines {
		matches := nolintRe.FindStringSubmatch(line)
		if matches != nil {
			directives = append(directives, IgnoreDirective{
				Line: i + 1,
				Rule: matches[1],
			})
		}
	}
	return directives
}

// FilterIgnored removes issues that are on lines with matching nolint directives.
func FilterIgnored(issues []Issue, src []byte) []Issue {
	directives := ParseIgnoreDirectives(src)
	if len(directives) == 0 {
		return issues
	}

	ignoreMap := make(map[int]string) // line -> rule (empty = all)
	for _, d := range directives {
		ignoreMap[d.Line] = d.Rule
	}

	var filtered []Issue
	for _, issue := range issues {
		if rule, ok := ignoreMap[issue.Line]; ok {
			if rule == "" || rule == issue.Rule {
				continue
			}
		}
		filtered = append(filtered, issue)
	}
	return filtered
}

// ---- Running the linter programmatically ----

// RunOptions controls how the linter executes.
type RunOptions struct {
	ConfigPath   string
	Paths        []string
	OutputFormat string // "text" or "json"
	MaxIssues    int
	FailOnError  bool
}

// Run executes the linter with the given options.
func Run(opts *RunOptions) (*LintResult, error) {
	if opts == nil {
		opts = &RunOptions{}
	}

	cfg, err := LoadConfigFile(opts.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	if opts.MaxIssues > 0 {
		cfg.MaxIssues = opts.MaxIssues
	}

	linter := New(cfg)

	var allIssues []Issue
	fileCount := 0

	for _, path := range opts.Paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("accessing %s: %w", path, err)
		}
		if info.IsDir() {
			issues, err := linter.LintDir(path)
			if err != nil {
				return nil, err
			}
			allIssues = append(allIssues, issues...)
		} else {
			issues, err := linter.LintFile(path)
			if err != nil {
				continue
			}
			allIssues = append(allIssues, issues...)
			fileCount++
		}
	}

	result := NewLintResult(allIssues, opts.ConfigPath, fileCount)
	return result, nil
}

// ---- Report formatting helpers ----

// SeverityCount is a helper for quick counting.
func SeverityCount(issues []Issue, sev Severity) int {
	count := 0
	for _, issue := range issues {
		if issue.Severity == sev {
			count++
		}
	}
	return count
}

// WorstSeverity returns the highest severity among issues.
func WorstSeverity(issues []Issue) Severity {
	worst := SevInfo
	for _, issue := range issues {
		if issue.Severity > worst {
			worst = issue.Severity
		}
	}
	return worst
}

// IsClean returns true if there are zero issues.
func IsClean(issues []Issue) bool {
	return len(issues) == 0
}

// ---- Severity parsing utilities ----

// ParseSeverity converts a string to a Severity.
func ParseSeverity(s string) (Severity, error) {
	switch strings.ToLower(s) {
	case "info":
		return SevInfo, nil
	case "warning":
		return SevWarning, nil
	case "error":
		return SevError, nil
	case "fatal":
		return SevFatal, nil
	default:
		return SevInfo, fmt.Errorf("unknown severity: %s", s)
	}
}

// AllSeverities returns all severity levels.
func AllSeverities() []Severity {
	return []Severity{SevInfo, SevWarning, SevError, SevFatal}
}

// ---- Code snippet extraction ----

// ExtractSnippet gets the source code around a position.
func ExtractSnippet(src []byte, line, context int) string {
	lines := strings.Split(string(src), "\n")
	start := line - context - 1
	if start < 0 {
		start = 0
	}
	end := line + context
	if end > len(lines) {
		end = len(lines)
	}
	var sb strings.Builder
	for i := start; i < end; i++ {
		sb.WriteString(lines[i])
		if i < end-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// ---- Configuration validation ----

// ValidateConfig checks a configuration for semantic errors.
func ValidateConfig(cfg *LinterConfig) []string {
	var errs []string
	if cfg.Version == "" {
		errs = append(errs, "version is required")
	}
	if cfg.MaxIssues < 0 {
		errs = append(errs, "max_issues must be non-negative")
	}
	for name, rc := range cfg.Rules {
		if rc.Severity != "" {
			if _, err := ParseSeverity(rc.Severity); err != nil {
				errs = append(errs, fmt.Sprintf("rule '%s': %v", name, err))
			}
		}
	}
	return errs
}

// ---- RuleSet for bulk operations ----

// RuleSet is a named collection of rules.
type RuleSet struct {
	Name        string
	Description string
	RuleNames   []string
}

// NewRuleSet creates a RuleSet.
func NewRuleSet(name, description string, rules ...string) *RuleSet {
	return &RuleSet{
		Name:        name,
		Description: description,
		RuleNames:   rules,
	}
}

// ApplyRuleSet enables the named rules and disables all others.
func (l *Linter) ApplyRuleSet(rs *RuleSet) {
	// Disable all first
	for _, rule := range l.registry.All() {
		rule.Enabled = false
	}
	// Enable selected
	ruleSet := make(map[string]bool)
	for _, name := range rs.RuleNames {
		ruleSet[name] = true
	}
	for _, name := range rs.RuleNames {
		if r, ok := l.registry.Get(name); ok {
			r.Enabled = true
		}
	}
}

// ---- Linting source from reader (for testing/programmatic use) ----

// LintSource lints Go source from a reader, using a virtual filename.
func (l *Linter) LintSource(filename string, r io.Reader) ([]Issue, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return l.lintAST(f, fset, src, filename), nil
}

// ---- Custom rule registration helpers ----

// RegisterCustomRule adds a user-defined rule from configuration.
func (l *Linter) RegisterCustomRule(name, desc string, sev Severity, checkFn func(file *ast.File, fset *token.FileSet, src []byte) []Issue) {
	l.registry.Register(&Rule{
		Name:        name,
		Description: desc,
		Severity:    sev,
		Enabled:     true,
		Check:       checkFn,
	})
}

// ---- Linting phases ----

// LintPhase represents a phase of linting (e.g., syntax, style, security).
type LintPhase int

const (
	// PhaseSyntax checks syntax-level issues.
	PhaseSyntax LintPhase = iota
	// PhaseStyle checks style and formatting.
	PhaseStyle
	// PhaseSecurity checks security issues.
	PhaseSecurity
	// PhasePerformance checks performance issues.
	PhasePerformance
)

var phaseNames = map[LintPhase]string{
	PhaseSyntax:      "syntax",
	PhaseStyle:       "style",
	PhaseSecurity:    "security",
	PhasePerformance: "performance",
}

func (p LintPhase) String() string {
	if name, ok := phaseNames[p]; ok {
		return name
	}
	return "unknown"
}

// PhaseResult holds results for a single phase.
type PhaseResult struct {
	Phase  LintPhase
	Issues []Issue
}

// LintPhased runs linting in phases.
func (l *Linter) LintPhased(filename string) ([]PhaseResult, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var results []PhaseResult
	phases := []struct {
		phase LintPhase
		rules []string
	}{
		{PhaseSyntax, []string{"unused-var", "var-decl", "range-issue"}},
		{PhaseStyle, []string{"long-line", "naming-convention", "missing-doc", "import-order"}},
		{PhaseSecurity, []string{"error-check", "context-propagation"}},
		{PhasePerformance, []string{"too-complex", "deep-nesting", "magic-number", "empty-block"}},
	}

	for _, p := range phases {
		// Run only the rules for this phase
		var phaseIssues []Issue
		for _, ruleName := range p.rules {
			rule, ok := l.registry.Get(ruleName)
			if !ok || !rule.Enabled {
				continue
			}
			issues := rule.Check(f, fset, src)
			for i := range issues {
				issues[i].File = filename
				issues[i].Rule = rule.Name
			}
			phaseIssues = append(phaseIssues, issues...)
		}
		results = append(results, PhaseResult{Phase: p.phase, Issues: phaseIssues})
	}

	return results, nil
}

// ---- Progress reporting ----

// ProgressReporter receives progress updates during linting.
type ProgressReporter interface {
	OnStart(totalFiles int)
	OnFile(file string, issues int)
	OnDone(totalIssues int)
}

// ConsoleReporter prints progress to stdout.
type ConsoleReporter struct {
	Out io.Writer
}

// OnStart implements ProgressReporter.
func (cr *ConsoleReporter) OnStart(totalFiles int) {
	fmt.Fprintf(cr.Out, "Linting %d files...\n", totalFiles)
}

// OnFile implements ProgressReporter.
func (cr *ConsoleReporter) OnFile(file string, issues int) {
	fmt.Fprintf(cr.Out, "  %s: %d issues\n", file, issues)
}

// OnDone implements ProgressReporter.
func (cr *ConsoleReporter) OnDone(totalIssues int) {
	fmt.Fprintf(cr.Out, "Done. %d total issues found.\n", totalIssues)
}

// ---- Caching support ----

// LintCache provides a simple in-memory cache for lint results.
type LintCache struct {
	mu    sync.RWMutex
	cache map[string]cachedResult
}

type cachedResult struct {
	Issues []Issue
	Hash   string
}

// NewLintCache creates a new lint cache.
func NewLintCache() *LintCache {
	return &LintCache{
		cache: make(map[string]cachedResult),
	}
}

// Get returns cached issues for a file if available and unchanged.
func (lc *LintCache) Get(filename, hash string) ([]Issue, bool) {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	if cr, ok := lc.cache[filename]; ok && cr.Hash == hash {
		return cr.Issues, true
	}
	return nil, false
}

// Set stores lint results for a file.
func (lc *LintCache) Set(filename, hash string, issues []Issue) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.cache[filename] = cachedResult{Issues: issues, Hash: hash}
}

// Invalidate removes a file from the cache.
func (lc *LintCache) Invalidate(filename string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	delete(lc.cache, filename)
}

// Clear removes all cached entries.
func (lc *LintCache) Clear() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.cache = make(map[string]cachedResult)
}

// ---- Source hash computation ----

// SourceHash computes a simple hash of source bytes.
func SourceHash(src []byte) string {
	h := 0
	for _, b := range src {
		h = h*31 + int(b)
	}
	return strconv.Itoa(h)
}

// ---- Issue comparison and sorting ----

// IssueByPosition implements sort.Interface for issues sorted by position.
type IssueByPosition []Issue

func (a IssueByPosition) Len() int      { return len(a) }
func (a IssueByPosition) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a IssueByPosition) Less(i, j int) bool {
	if a[i].File != a[j].File {
		return a[i].File < a[j].File
	}
	if a[i].Line != a[j].Line {
		return a[i].Line < a[j].Line
	}
	return a[i].Column < a[j].Column
}

// IssueBySeverity implements sort.Interface for issues sorted by severity (desc).
type IssueBySeverity []Issue

func (a IssueBySeverity) Len() int      { return len(a) }
func (a IssueBySeverity) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a IssueBySeverity) Less(i, j int) bool {
	return a[i].Severity > a[j].Severity // higher severity first
}

// ---- Rule presets ----

// PresetAll enables all rules.
func PresetAll() map[string]bool {
	return map[string]bool{
		"unused-var":          true,
		"too-complex":         true,
		"missing-doc":         true,
		"long-line":           true,
		"deep-nesting":        true,
		"magic-number":        true,
		"error-check":         true,
		"naming-convention":   true,
		"import-order":        true,
		"empty-block":         true,
		"var-decl":            true,
		"range-issue":         true,
		"context-propagation": true,
	}
}

// PresetMinimal enables only error-level rules.
func PresetMinimal() map[string]bool {
	return map[string]bool{
		"error-check": true,
	}
}

// PresetStyle enables style-focused rules.
func PresetStyle() map[string]bool {
	return map[string]bool{
		"missing-doc":       true,
		"long-line":         true,
		"naming-convention": true,
		"import-order":      true,
		"var-decl":          true,
	}
}

// PresetQuality enables code quality rules.
func PresetQuality() map[string]bool {
	return map[string]bool{
		"unused-var":          true,
		"too-complex":         true,
		"deep-nesting":        true,
		"magic-number":        true,
		"empty-block":         true,
		"range-issue":         true,
		"context-propagation": true,
	}
}
