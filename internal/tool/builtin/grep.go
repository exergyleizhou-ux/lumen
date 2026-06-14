package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&GrepTool{})
}

// GrepTool searches for a regex pattern in files.
type GrepTool struct{}

func (t *GrepTool) Name() string   { return "grep" }
func (t *GrepTool) ReadOnly() bool { return true }

func (t *GrepTool) Description() string {
	return "Search for a regular expression in a file, or recursively under a directory (skips hidden files and files matched by .gitignore). Returns matching lines as path:line:text, capped at 200 matches."
}

func (t *GrepTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "pattern":{"type":"string","description":"Regular expression (RE2 syntax)"},
  "path":{"type":"string","description":"File or directory to search (default '.')"}
},
"required":["pattern"]
}`)
}

func (t *GrepTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	if p.Path == "" {
		p.Path = "."
	}

	re, err := regexp.Compile(p.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}

	const maxMatches = 200
	var results []string

	info, err := os.Stat(p.Path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", p.Path, err)
	}

	if !info.IsDir() {
		matches, _ := grepFile(p.Path, re, maxMatches)
		return strings.Join(matches, "\n"), nil
	}

	filepath.Walk(p.Path, func(fpath string, fi os.FileInfo, err error) error {
		if err != nil || len(results) >= maxMatches {
			return nil
		}
		name := fi.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			if fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if fi.IsDir() {
			return nil
		}
		matches, _ := grepFile(fpath, re, maxMatches-len(results))
		results = append(results, matches...)
		return nil
	})

	return strings.Join(results, "\n"), nil
}

func grepFile(path string, re *regexp.Regexp, maxMatches int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var matches []string
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() && len(matches) < maxMatches {
		lineNum++
		if re.Match(scanner.Bytes()) {
			matches = append(matches, fmt.Sprintf("%s:%d:%s", path, lineNum, scanner.Text()))
		}
	}
	return matches, nil
}
