// Package frontmatter splits YAML frontmatter from Markdown content.
// Used by the skill loader and project memory files.
package frontmatter

import "strings"

// Split extracts frontmatter (key: value pairs between --- delimiters) from the
// head of a Markdown document. Returns the parsed map and the body (everything
// after the closing ---). An absent or malformed frontmatter block returns an
// empty map and the full document as body.
func Split(doc string) (map[string]string, string) {
	if !strings.HasPrefix(doc, "---\n") && !strings.HasPrefix(doc, "---\r\n") {
		return map[string]string{}, doc
	}
	// Normalize line endings
	doc = strings.ReplaceAll(doc, "\r\n", "\n")
	// Find closing ---
	end := strings.Index(doc[4:], "\n---")
	if end < 0 {
		// Edge case: empty frontmatter "---\n---\nbody"
		if strings.HasPrefix(doc[4:], "---\n") || strings.HasPrefix(doc[4:], "---") {
			end = 0 // fmBlock is empty, closing delimiter is at position 0 in doc[4:]
		} else {
			return map[string]string{}, doc
		}
	}
	fmBlock := doc[4 : 4+end]
	body := ""
	if 4+end+4 < len(doc) {
		body = strings.TrimPrefix(doc[4+end+4:], "\n")
	}

	fm := map[string]string{}
	for _, line := range strings.Split(fmBlock, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		val := strings.TrimSpace(kv[1])
		// Strip surrounding quotes
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		fm[key] = val
	}
	return fm, body
}
