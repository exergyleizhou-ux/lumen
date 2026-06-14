package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&GlobTool{})
	tool.RegisterBuiltin(&LsTool{})
}

// GlobTool finds files matching a glob pattern.
type GlobTool struct{}

func (t *GlobTool) Name() string   { return "glob" }
func (t *GlobTool) ReadOnly() bool { return true }

func (t *GlobTool) Description() string {
	return "Find files matching a glob pattern (e.g. \"*.go\", \"internal/*/*.go\", \"**/*.test.ts\"). Supports shell metacharacters * ? [] and the recursive ** pattern."
}

func (t *GlobTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "pattern":{"type":"string","description":"Glob pattern (supports ** for recursive matching)"}
},
"required":["pattern"]
}`)
}

func (t *GlobTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}

	// Handle ** recursive matching
	if strings.Contains(p.Pattern, "**") {
		parts := strings.SplitN(p.Pattern, "**", 2)
		root := "."
		if parts[0] != "" {
			root = strings.TrimRight(parts[0], "/")
		}
		suffix := ""
		if len(parts) > 1 {
			suffix = strings.TrimLeft(parts[1], "/")
		}

		var files []string
		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				name := info.Name()
				if strings.HasPrefix(name, ".") || name == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(".", path)
			if match, _ := filepath.Match(suffix, filepath.Base(rel)); match || suffix == "" {
				files = append(files, rel)
			}
			return nil
		})
		return strings.Join(files, "\n"), nil
	}

	files, err := filepath.Glob(p.Pattern)
	if err != nil {
		return "", err
	}
	return strings.Join(files, "\n"), nil
}

// LsTool lists directory entries.
type LsTool struct{}

func (t *LsTool) Name() string   { return "ls" }
func (t *LsTool) ReadOnly() bool { return true }

func (t *LsTool) Description() string {
	return "List the entries of a directory. Directories are shown with a trailing slash; files show their byte size. Set recursive=true to list all nested files depth-first."
}

func (t *LsTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"Directory path (default '.')"},
  "recursive":{"type":"boolean","description":"When true, recursively list all nested files (default false)"}
}
}`)
}

func (t *LsTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		p.Path = "."
	}

	if !p.Recursive {
		entries, err := os.ReadDir(p.Path)
		if err != nil {
			return "", err
		}
		var lines []string
		for _, e := range entries {
			if e.IsDir() {
				lines = append(lines, e.Name()+"/")
			} else {
				info, _ := e.Info()
				size := int64(0)
				if info != nil {
					size = info.Size()
				}
				lines = append(lines, fmt.Sprintf("%s (%d)", e.Name(), size))
			}
		}
		return strings.Join(lines, "\n"), nil
	}

	var lines []string
	filepath.Walk(p.Path, func(fpath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(p.Path, fpath)
		if rel == "." {
			return nil
		}
		name := info.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			lines = append(lines, rel+"/")
		} else {
			lines = append(lines, fmt.Sprintf("%s (%d)", rel, info.Size()))
		}
		return nil
	})
	return strings.Join(lines, "\n"), nil
}
