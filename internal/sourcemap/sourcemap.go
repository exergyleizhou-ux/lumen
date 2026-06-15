// Package sourcemap generates source maps for transpiled code, mapping
// generated code positions back to original source locations.
package sourcemap

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Mapping struct {
	GeneratedLine, GeneratedColumn int
	OriginalLine, OriginalColumn   int
	Source                         string
	Name                           string
}
type SourceMap struct {
	Version        int
	File           string
	SourceRoot     string
	Sources        []string
	SourcesContent []string
	Names          []string
	Mappings       []Mapping
	mu             sync.Mutex
}

func NewSourceMap(file string) *SourceMap { return &SourceMap{Version: 3, File: file} }
func (sm *SourceMap) AddMapping(genLine, genCol, origLine, origCol int, source, name string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.Mappings = append(sm.Mappings, Mapping{GeneratedLine: genLine, GeneratedColumn: genCol, OriginalLine: origLine, OriginalColumn: origCol, Source: source, Name: name})
	found := false
	for _, s := range sm.Sources {
		if s == source {
			found = true
			break
		}
	}
	if !found {
		sm.Sources = append(sm.Sources, source)
	}
	if name != "" {
		foundName := false
		for _, n := range sm.Names {
			if n == name {
				foundName = true
				break
			}
		}
		if !foundName {
			sm.Names = append(sm.Names, name)
		}
	}
}
func (sm *SourceMap) Lookup(genLine, genCol int) *Mapping {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	var best *Mapping
	for i := range sm.Mappings {
		m := &sm.Mappings[i]
		if m.GeneratedLine == genLine && m.GeneratedColumn <= genCol {
			if best == nil || m.GeneratedColumn > best.GeneratedColumn {
				best = m
			}
		}
	}
	return best
}
func (sm *SourceMap) ToJSON() ([]byte, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	type smJSON struct {
		Version        int      `json:"version"`
		File           string   `json:"file"`
		SourceRoot     string   `json:"sourceRoot,omitempty"`
		Sources        []string `json:"sources"`
		SourcesContent []string `json:"sourcesContent,omitempty"`
		Names          []string `json:"names,omitempty"`
		Mappings       string   `json:"mappings"`
	}
	m := smJSON{Version: sm.Version, File: sm.File, SourceRoot: sm.SourceRoot, Sources: sm.Sources, SourcesContent: sm.SourcesContent, Names: sm.Names, Mappings: encodeMappings(sm.Mappings)}
	return json.MarshalIndent(m, "", "  ")
}
func encodeMappings(mappings []Mapping) string {
	sort.Slice(mappings, func(i, j int) bool {
		if mappings[i].GeneratedLine != mappings[j].GeneratedLine {
			return mappings[i].GeneratedLine < mappings[j].GeneratedLine
		}
		return mappings[i].GeneratedColumn < mappings[j].GeneratedColumn
	})
	var parts []string
	lastGenCol, lastSrcIdx, lastOrigLine, lastOrigCol, lastNameIdx := 0, 0, 0, 0, 0
	for _, m := range mappings {
		srcIdx := indexOf(sourcesList, m.Source)
		nameIdx := -1
		parts = append(parts, encodeVLQ(m.GeneratedColumn-lastGenCol)+encodeVLQ(srcIdx-lastSrcIdx)+encodeVLQ(m.OriginalLine-lastOrigLine)+encodeVLQ(m.OriginalColumn-lastOrigCol))
		if m.Name != "" {
			nameIdx = indexOf(namesList, m.Name)
			parts[len(parts)-1] += encodeVLQ(nameIdx - lastNameIdx)
			lastNameIdx = nameIdx
		}
		lastGenCol = m.GeneratedColumn
		lastSrcIdx = srcIdx
		lastOrigLine = m.OriginalLine
		lastOrigCol = m.OriginalColumn
	}
	return strings.Join(parts, ",")
}

var sourcesList []string
var namesList []string

func indexOf(list []string, s string) int {
	for i, item := range list {
		if item == s {
			return i
		}
	}
	return len(list)
}
func encodeVLQ(v int) string {
	minus := v < 0
	if minus {
		v = -v
	}
	vlq := v & 31
	v >>= 5
	var result []byte
	for {
		if v > 0 {
			result = append(result, byte(vlq|32))
		} else {
			result = append(result, byte(vlq))
			break
		}
		vlq = v & 31
		v >>= 5
	}
	if minus {
		result[len(result)-1] |= 64
	}
	s := base64Encode(result)
	return s
}
func base64Encode(data []byte) string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var sb strings.Builder
	for _, b := range data {
		sb.WriteByte(chars[b&63])
	}
	return sb.String()
}
func (sm *SourceMap) FormatSummary() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Source Map: %s\n%s\n\n", sm.File, strings.Repeat("─", 40))
	fmt.Fprintf(&sb, "  Sources: %d\n  Names: %d\n  Mappings: %d\n", len(sm.Sources), len(sm.Names), len(sm.Mappings))
	return sb.String()
}
