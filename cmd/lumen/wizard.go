package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"lumen/internal/config"
	"lumen/internal/control"
	"lumen/internal/permission"
)

// runWizard — Codex/Claude Code style onboarding.
// One open-ended starter, then dynamic follow-ups.
// The AI infers most details; user only fills knowledge gaps.
// scaffoldDefaultConfig writes a starter lumen.toml (DeepSeek preset, API key via
// env) at path, so a fresh install with no config isn't a dead end.
func scaffoldDefaultConfig(path string) error {
	const tmpl = `default_model = "deepseek-chat"

[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com/v1"
model = "deepseek-chat"
# API key is read from the environment (or .env). Set DEEPSEEK_API_KEY.
api_key_env = "DEEPSEEK_API_KEY"

[agent]
max_steps = 30
temperature = 0.7
`
	return os.WriteFile(path, []byte(tmpl), 0o644)
}

func runWizard(ctrl *control.Controller) error {
	// Fresh install: scaffold a starter config instead of dead-ending on
	// "no providers configured" inside Configure.
	if config.FindConfig() == "" {
		if err := scaffoldDefaultConfig("./lumen.toml"); err != nil {
			return fmt.Errorf("could not create lumen.toml: %w", err)
		}
		fmt.Printf("\n  %s\n", fg(G, "✓ created lumen.toml (DeepSeek)"))
		if os.Getenv("DEEPSEEK_API_KEY") == "" {
			fmt.Printf("  %s\n", fg(D, "Set DEEPSEEK_API_KEY (export it or add it to .env), then run 'lumen' again."))
			return nil
		}
	}
	if err := ctrl.Configure(termSink(), nil, ""); err != nil {
		return err
	}
	ctrl.SetPermissionMode(permission.ModeDefault)

	drawBanner(ctrl)
	fmt.Printf("\n  %s\n\n", fg(B, "hey! what are you working on?"))

	sc := bufio.NewScanner(os.Stdin)

	// ── Round 1: open-ended starter ──────────────────────────
	fmt.Printf("  %s ", fg(C, "▸"))
	if !sc.Scan() { return nil }
	firstAnswer := strings.TrimSpace(sc.Text())
	if firstAnswer == "/exit" { return nil }
	if firstAnswer == "" {
		fmt.Printf("\n  %s\n", fg(D, "no worries — just describe it whenever you're ready. /wizard to restart."))
		return nil
	}

	answers := []string{firstAnswer}

	// ── Round 2: follow up based on what they said ────────────
	followUp := inferFollowUp(firstAnswer)
	if followUp != "" {
		fmt.Printf("\n  %s\n", fg(D, followUp))
		fmt.Printf("  %s ", fg(C, "▸"))
		if !sc.Scan() { return nil }
		a2 := strings.TrimSpace(sc.Text())
		if a2 == "/exit" { return nil }
		if a2 != "" { answers = append(answers, a2) }
	}

	// ── Round 3: one more if needed ──────────────────────────
	if len(answers) < 2 || len(strings.Join(answers, " ")) < 60 {
		fmt.Printf("\n  %s\n", fg(D, "anything else I should know? (skip if you're good)"))
		fmt.Printf("  %s ", fg(C, "▸"))
		if !sc.Scan() { return nil }
		a3 := strings.TrimSpace(sc.Text())
		if a3 == "/exit" { return nil }
		if a3 != "" && a3 != "no" && a3 != "nope" { answers = append(answers, a3) }
	}

	// ── Build prompt ─────────────────────────────────────────
	prompt := buildPrompt(answers)
	fmt.Printf("\n\n  %s\n", fg(B, "ok, here's what i'm thinking:"))
	fmt.Printf("  %s\n\n", fg(D, prompt))
	fmt.Printf("  %s %s\n", fg(C, "look good?"), fg(D, "(y/n)"))
	fmt.Printf("  %s ", fg(C, "▸"))
	if !sc.Scan() { return nil }
	if strings.TrimSpace(sc.Text()) != "y" {
		fmt.Printf("\n  %s\n", fg(D, "no problem. /wizard to start over."))
		return nil
	}

	// ── Plan ─────────────────────────────────────────────────
	fmt.Printf("\n  %s\n", fg(B, "exploring..."))
	ctrl.SetPermissionMode(permission.ModePlan)
	ctx := context.Background()
	if err := ctrl.Plan(ctx, prompt); err != nil {
		fmt.Printf("  %s\n", fg(Rd, "hmm, something went wrong: "+err.Error()))
		return err
	}

	// ── Execute ──────────────────────────────────────────────
	fmt.Printf("\n  %s\n", fg(B, "building now..."))
	ctrl.Agent().SetPlanMode(false) // clear plan mode flag from Plan() call
	ctrl.SetPermissionMode(permission.ModeBypass)
	if err := ctrl.Run(ctx, prompt); err != nil {
		fmt.Printf("  %s\n", fg(Rd, "error: "+err.Error()))
	}
	fmt.Printf("\n  %s\n", fg(G, "done! check the README for how to run it."))

	return nil
}

// inferFollowUp picks a natural second question based on what the user said.
func inferFollowUp(answer string) string {
	lower := strings.ToLower(answer)

	switch {
	case strings.Contains(lower, "website") || strings.Contains(lower, "site") || strings.Contains(lower, "web app"):
		return "nice. is this for yourself, a team, or the public?"
	case strings.Contains(lower, "app") || strings.Contains(lower, "mobile") || strings.Contains(lower, "ios") || strings.Contains(lower, "android"):
		return "cool. what's the one thing it absolutely has to do well?"
	case strings.Contains(lower, "api") || strings.Contains(lower, "backend") || strings.Contains(lower, "server"):
		return "got it. what kind of data or service are you exposing?"
	case strings.Contains(lower, "data") || strings.Contains(lower, "dashboard") || strings.Contains(lower, "analytics"):
		return "makes sense. where's the data coming from? files, database, api?"
	case strings.Contains(lower, "script") || strings.Contains(lower, "automation") || strings.Contains(lower, "bot"):
		return "nice. what's the trigger — do you run it manually, on a schedule, or when something happens?"
	case strings.Contains(lower, "game") || strings.Contains(lower, "animation"):
		return "fun. what platform or tech are you thinking?"
	case strings.Contains(lower, "cli") || strings.Contains(lower, "terminal") || strings.Contains(lower, "command"):
		return "nice. what's the main workflow — does it take input, process files, talk to APIs?"
	case len(lower) < 30:
		return "tell me a bit more — what should it actually do?"
	default:
		return "anything else i should know before i start?"
	}
}

func buildPrompt(answers []string) string {
	joined := strings.Join(answers, " ")
	var sb strings.Builder

	fmt.Fprintf(&sb, "Build a project based on this description: %s\n\n", joined)
	sb.WriteString("Rules:\n")
	sb.WriteString("- Infer reasonable defaults for anything unspecified (tech stack, framework, styling, dependencies)\n")
	sb.WriteString("- If the description is vague, pick the simplest thing that satisfies it\n")
	sb.WriteString("- Plan the file structure first, then build each file\n")
	sb.WriteString("- Use modern best practices but don't over-engineer\n")
	sb.WriteString("- After building, write a README.md with setup and run instructions\n")
	sb.WriteString("- The user is new to this — make it easy to understand and run\n")

	return sb.String()
}
