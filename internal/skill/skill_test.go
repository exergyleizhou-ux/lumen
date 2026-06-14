package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreListProjectSkills(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, "skills")
	os.Mkdir(skillsDir, 0o755)

	// Write a skill .md file (flat layout)
	os.WriteFile(filepath.Join(skillsDir, "my-skill.md"), []byte(`---
name: my-skill
description: Does something useful
---
# My Skill
This is the body.`), 0o644)

	// Write a subagent skill
	os.WriteFile(filepath.Join(skillsDir, "explore.md"), []byte(`---
name: explore
description: Explore the codebase
runAs: subagent
allowed-tools: read_file, grep
---
# Explore
You are an explorer.`), 0o644)

	store := New(Options{ProjectRoot: dir})
	skills := store.List()

	if len(skills) < 2 {
		t.Fatalf("expected at least 2 skills, got %d", len(skills))
	}

	var foundMySkill, foundExplore bool
	for _, sk := range skills {
		if sk.Name == "my-skill" {
			foundMySkill = true
			if sk.Scope != ScopeProject {
				t.Errorf("my-skill scope: want project, got %s", sk.Scope)
			}
			if sk.Description != "Does something useful" {
				t.Errorf("description mismatch: %q", sk.Description)
			}
		}
		if sk.Name == "explore" {
			foundExplore = true
			if sk.RunAs != RunSubagent {
				t.Errorf("explore runAs: want subagent, got %s", sk.RunAs)
			}
			if len(sk.AllowedTools) != 2 {
				t.Errorf("explore allowed-tools: want 2, got %d", len(sk.AllowedTools))
			}
		}
	}
	if !foundMySkill || !foundExplore {
		t.Error("missing expected skills")
	}
}

func TestStoreGet(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, "skills")
	os.Mkdir(skillsDir, 0o755)
	os.WriteFile(filepath.Join(skillsDir, "my-skill.md"), []byte(`---
name: my-skill
description: Does something useful
---
# My Skill
Body here.`), 0o644)

	store := New(Options{ProjectRoot: dir})
	sk, ok := store.Get("my-skill")
	if !ok {
		t.Fatal("Get should find my-skill")
	}
	if sk.Body != "# My Skill\nBody here." {
		t.Errorf("body mismatch: %q", sk.Body)
	}
}

func TestStoreGetMissing(t *testing.T) {
	store := New(Options{ProjectRoot: t.TempDir()})
	_, ok := store.Get("nonexistent")
	if ok {
		t.Error("Get should return false for missing skill")
	}
}

func TestStoreDirectoryLayout(t *testing.T) {
	dir := t.TempDir()
	// .reasonix/skills/my-skill/SKILL.md layout
	reasonixSkills := filepath.Join(dir, ".reasonix", "skills", "my-skill")
	os.MkdirAll(reasonixSkills, 0o755)
	os.WriteFile(filepath.Join(reasonixSkills, "SKILL.md"), []byte(`---
name: dir-skill
description: From directory layout
---
# Dir Skill`), 0o644)

	store := New(Options{ProjectRoot: dir})
	sk, ok := store.Get("dir-skill")
	if !ok {
		t.Fatal("should find directory-layout skill")
	}
	if sk.Description != "From directory layout" {
		t.Errorf("description: %q", sk.Description)
	}
}

func TestStoreOverrideName(t *testing.T) {
	// frontmatter `name:` overrides filename stem
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, "skills")
	os.Mkdir(skillsDir, 0o755)
	os.WriteFile(filepath.Join(skillsDir, "file-stem.md"), []byte(`---
name: frontmatter-name
description: Test
---
Body`), 0o644)

	store := New(Options{ProjectRoot: dir})
	sk, ok := store.Get("frontmatter-name")
	if !ok {
		t.Error("should find by frontmatter name")
	}
	_ = sk
	// The stem name should NOT be discoverable
	_, ok2 := store.Get("file-stem")
	if ok2 {
		t.Error("stem name should not be discoverable when frontmatter overrides")
	}
}

func TestParseRunAs(t *testing.T) {
	if parseRunAs("subagent", "", "") != RunSubagent {
		t.Error("runAs: subagent")
	}
	if parseRunAs("inline", "", "") != RunInline {
		t.Error("runAs: inline")
	}
	if parseRunAs("", "fork", "") != RunSubagent {
		t.Error("context=fork → subagent")
	}
	if parseRunAs("", "", "my-agent") != RunSubagent {
		t.Error("agent field → subagent")
	}
	if parseRunAs("", "", "") != RunInline {
		t.Error("default → inline")
	}
}

func TestParseAllowedTools(t *testing.T) {
	tools := parseAllowedTools("read_file,grep, glob")
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d: %v", len(tools), tools)
	}
	if tools[2] != "glob" {
		t.Errorf("third tool: want glob, got %q", tools[2])
	}

	tools2 := parseAllowedTools("")
	if tools2 != nil {
		t.Error("empty string should return nil")
	}

	tools3 := parseAllowedTools("  ,  ,  ")
	if tools3 != nil {
		t.Error("whitespace-only should return nil")
	}
}

func TestIsValidName(t *testing.T) {
	if !IsValidName("test") {
		t.Error("test should be valid")
	}
	if !IsValidName("bug-hunt") {
		t.Error("bug-hunt should be valid")
	}
	if IsValidName("") {
		t.Error("empty should be invalid")
	}
	if IsValidName("9invalid") {
		t.Error("starts with digit should be invalid")
	}
}

func TestStoreSkipsLooseMarkdownWithoutDescription(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, "skills")
	os.Mkdir(skillsDir, 0o755)

	// A real skill: frontmatter declares a description.
	os.WriteFile(filepath.Join(skillsDir, "good.md"), []byte(`---
name: good
description: A real, invokable skill
---
# Good
Body.`), 0o644)

	// A plain documentation file masquerading as a skill: no frontmatter,
	// no description. This is what CHANGELOG.md / README.md / OPENCLAW.md
	// look like — they must NOT be loaded as skills.
	os.WriteFile(filepath.Join(skillsDir, "notes.md"),
		[]byte("# Project Notes\n\nJust documentation, not a skill.\n"), 0o644)

	// Isolate from the host's ~/.claude skills so the test is hermetic.
	store := New(Options{ProjectRoot: dir, HomeDir: t.TempDir()})

	if _, ok := store.Get("good"); !ok {
		t.Error("good.md (has description) should load as a skill")
	}
	if _, ok := store.Get("notes"); ok {
		t.Error("notes.md (no frontmatter/description) must NOT load as a skill")
	}
}

func TestStoreExcludesGlobalSkillsByDefault(t *testing.T) {
	// A skill installed in the host's global ~/.claude/skills.
	home := t.TempDir()
	globalDir := filepath.Join(home, ".claude", "skills")
	os.MkdirAll(globalDir, 0o755)
	os.WriteFile(filepath.Join(globalDir, "global-skill.md"), []byte(`---
name: global-skill
description: From the host global install
---
Body.`), 0o644)

	// A project skill.
	project := t.TempDir()
	projSkills := filepath.Join(project, "skills")
	os.Mkdir(projSkills, 0o755)
	os.WriteFile(filepath.Join(projSkills, "proj-skill.md"), []byte(`---
name: proj-skill
description: A project skill
---
Body.`), 0o644)

	// Default is project-only: the host's global skills are NOT inherited.
	store := New(Options{ProjectRoot: project, HomeDir: home})
	if _, ok := store.Get("proj-skill"); !ok {
		t.Error("project skill should load")
	}
	if _, ok := store.Get("global-skill"); ok {
		t.Error("global skill must NOT load by default (project-only)")
	}
}

func TestStoreIncludesGlobalSkillsWhenOptedIn(t *testing.T) {
	home := t.TempDir()
	globalDir := filepath.Join(home, ".claude", "skills")
	os.MkdirAll(globalDir, 0o755)
	os.WriteFile(filepath.Join(globalDir, "global-skill.md"), []byte(`---
name: global-skill
description: From the host global install
---
Body.`), 0o644)

	store := New(Options{ProjectRoot: t.TempDir(), HomeDir: home, IncludeGlobal: true})
	if _, ok := store.Get("global-skill"); !ok {
		t.Error("global skill should load when IncludeGlobal is set")
	}
}
