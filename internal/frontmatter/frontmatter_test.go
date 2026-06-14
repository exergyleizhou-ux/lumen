package frontmatter

import "testing"

func TestSplitNoFrontmatter(t *testing.T) {
	fm, body := Split("just a regular text\nno frontmatter here")
	if len(fm) != 0 {
		t.Errorf("expected empty frontmatter, got %d entries", len(fm))
	}
	if body != "just a regular text\nno frontmatter here" {
		t.Errorf("body should be unchanged, got %q", body)
	}
}

func TestSplitBasic(t *testing.T) {
	doc := `---
name: test-skill
description: A test skill
---
# Skill Body

This is the body content.`
	fm, body := Split(doc)
	if fm["name"] != "test-skill" {
		t.Errorf("name: want test-skill, got %q", fm["name"])
	}
	if fm["description"] != "A test skill" {
		t.Errorf("description: want 'A test skill', got %q", fm["description"])
	}
	if body != "# Skill Body\n\nThis is the body content." {
		t.Errorf("body should start with '# Skill Body', got %q", body)
	}
}

func TestSplitRunAs(t *testing.T) {
	doc := `---
name: subagent-skill
runAs: subagent
allowed-tools: read_file, grep, glob
---
You are a subagent.`
	fm, body := Split(doc)
	if fm["name"] != "subagent-skill" {
		t.Errorf("name mismatch: %q", fm["name"])
	}
	if fm["runas"] != "subagent" {
		t.Errorf("runAs: want subagent, got %q", fm["runas"])
	}
	allowed := fm["allowed-tools"]
	if allowed != "read_file, grep, glob" {
		t.Errorf("allowed-tools mismatch: %q", fm["allowed-tools"])
	}
	if body != "You are a subagent." {
		t.Errorf("body mismatch: %q", body)
	}
}

func TestSplitQuotedValues(t *testing.T) {
	doc := `---
name: "quoted-name"
description: 'single quoted desc'
---
body`
	fm, _ := Split(doc)
	if fm["name"] != "quoted-name" {
		t.Errorf("quoted name: want quoted-name, got %q", fm["name"])
	}
	if fm["description"] != "single quoted desc" {
		t.Errorf("quoted desc: want 'single quoted desc', got %q", fm["description"])
	}
}

func TestSplitEmptyFrontmatter(t *testing.T) {
	doc := "---\n---\nbody after empty"
	fm, body := Split(doc)
	if len(fm) != 0 {
		t.Errorf("empty frontmatter should produce empty map, got %d entries", len(fm))
	}
	if body != "body after empty" {
		t.Errorf("body mismatch: want 'body after empty', got %q", body)
	}
}

func TestSplitNoClosing(t *testing.T) {
	doc := `---
name: incomplete
body here`
	fm, body := Split(doc)
	// No closing --- means everything is body
	if len(fm) != 0 {
		t.Errorf("no closing delimiter: expected empty fm, got %d entries", len(fm))
	}
	if body != doc {
		t.Errorf("no closing: body should be full doc")
	}
}

func TestSplitWindowsLineEndings(t *testing.T) {
	doc := "---\r\nname: win-skill\r\n---\r\nbody on windows"
	fm, body := Split(doc)
	if fm["name"] != "win-skill" {
		t.Errorf("CRLF: name mismatch: %q", fm["name"])
	}
	if body != "body on windows" {
		t.Errorf("CRLF: body mismatch: %q", body)
	}
}
