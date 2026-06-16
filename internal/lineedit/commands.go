// Package lineedit provides an interactive terminal line editor for Lumen's
// chat input: history navigation, cursor editing, multibyte input, and
// Tab-completion of slash-commands and @-file mentions. The pure logic
// (buffer, key decoding, history, completion) is unit-tested; the raw-mode
// driver wraps it.
package lineedit

import (
	"os"
	"sort"
	"strings"
)

// Command is one slash-command offered in the input.
type Command struct {
	Name string
	Help string
}

// builtinCommands is the slash-command catalog surfaced by completion.
var builtinCommands = []Command{
	{"/help", "show help"},
	{"/exit", "quit"},
	{"/quit", "quit"},
	{"/mode", "set permission mode"},
	{"/model", "switch model"},
	{"/diff", "show pending diff"},
	{"/undo", "rewind last edit"},
	{"/compact", "compact the session"},
	{"/cost", "show token cost"},
	{"/resume", "resume a past session"},
	{"/clear", "clear the screen"},
}

// Commands returns the slash-command catalog.
func Commands() []Command { return builtinCommands }

// MatchCommands returns commands whose name matches prefix. A leading slash on
// the prefix is tolerated. Results are sorted by name.
func MatchCommands(prefix string) []Command {
	p := strings.TrimPrefix(strings.TrimSpace(prefix), "/")
	var out []Command
	for _, c := range builtinCommands {
		if strings.HasPrefix(strings.TrimPrefix(c.Name, "/"), p) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// CompletePath returns entries under root whose name starts with prefix,
// sorted, capped, skipping noisy directories. Directories get a trailing slash.
func CompletePath(root, prefix string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	skip := map[string]bool{".git": true, "node_modules": true, ".lumen": true}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if skip[name] {
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if e.IsDir() {
			name += "/"
		}
		out = append(out, name)
		if len(out) >= 50 {
			break
		}
	}
	sort.Strings(out)
	return out
}

// commonPrefix returns the longest shared prefix of all strings.
func commonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	p := ss[0]
	for _, s := range ss[1:] {
		for !strings.HasPrefix(s, p) {
			p = p[:len(p)-1]
			if p == "" {
				return ""
			}
		}
	}
	return p
}
