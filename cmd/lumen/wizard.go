// wizard.go — Active onboarding wizard. Instead of a blank prompt,
// the agent interviews the user with structured questions and builds
// a concrete task plan from their answers.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"lumen/internal/control"
	"lumen/internal/permission"
)

// ── Question flow ──────────────────────────────────────────

type wizardStep struct {
	Question string
	Field    string
	Hint     string
	Validate func(string) bool
}

var onboardingFlow = []wizardStep{
	{
		Question: "What do you want to build? (e.g. a website, a mobile app, a data dashboard, an API, a script…)",
		Field:    "project_type",
		Hint:     "Just describe what it is — don't worry about technical terms.",
	},
	{
		Question: "Who will use it? (e.g. just me, my team, the public, customers…)",
		Field:    "audience",
		Hint:     "This helps me pick the right tooling and security level.",
	},
	{
		Question: "What should it actually DO? Describe 2-3 key features.",
		Field:    "features",
		Hint:     "Example: 'Users can sign up, create posts, and comment on posts.'",
	},
	{
		Question: "Any design or style preferences? (e.g. clean and minimal, dark mode, colorful, corporate…)",
		Field:    "style",
		Hint:     "Leave blank if you don't have a preference — I'll pick something clean.",
	},
	{
		Question: "Do you have a preferred language or tech stack? (e.g. Python, Go, React, 'whatever you recommend')",
		Field:    "tech_stack",
		Hint:     "If you're not sure, just say 'recommend something' and I'll pick the best fit.",
	},
	{
		Question: "When does this need to be ready? (e.g. today, this week, next month, no rush)",
		Field:    "deadline",
		Hint:     "This helps me decide how much to build now vs. plan for later.",
	},
	{
		Question: "Is this a brand new project, or are you extending something that already exists?",
		Field:    "existing",
		Hint:     "If there's existing code, tell me where it lives.",
	},
	{
		Question: "Anything else I should know? (constraints, must-haves, dealbreakers…)",
		Field:    "notes",
		Hint:     "Leave blank if nothing else — we'll figure it out together.",
	},
}

// ── Wizard entry ───────────────────────────────────────────

func runWizard(ctrl *control.Controller) error {
	if err := ctrl.Configure(termSink(), nil, ""); err != nil {
		return err
	}
	ctrl.SetPermissionMode(permission.ModeDefault)

	drawBanner(ctrl)

	fmt.Printf("\n  %s\n\n", fg(B, "👋 Welcome! I'll ask a few questions to understand what you need."))
	fmt.Printf("  %s\n", fg(D, "Answer in your own words — skip any question with Enter.\n"))

	answers := map[string]string{}
	sc := bufio.NewScanner(os.Stdin)

	for _, step := range onboardingFlow {
		fmt.Printf("\n  %s\n", fg(B+C, step.Question))
		if step.Hint != "" {
			fmt.Printf("  %s\n", fg(D, "  → "+step.Hint))
		}
		fmt.Printf("  %s ", fg(C, "▸"))
		if !sc.Scan() {
			break
		}
		answer := strings.TrimSpace(sc.Text())
		if answer == "/exit" {
			return nil
		}
		if answer == "" {
			fmt.Printf("  %s\n", fg(D, "  (skipped)"))
			continue
		}
		answers[step.Field] = answer
	}

	// Build prompt from answers
	prompt := buildProjectPrompt(answers)

	fmt.Printf("\n\n  %s\n", fg(B, "── Project Brief ──"))
	fmt.Printf("  %s\n\n", fg(D, prompt))

	fmt.Printf("  %s\n", fg(C, "Shall I start building this? (y/n)"))
	fmt.Printf("  %s ", fg(C, "▸"))
	if !sc.Scan() {
		return nil
	}
	confirm := strings.TrimSpace(sc.Text())
	if confirm != "y" && confirm != "yes" && confirm != "Y" {
		fmt.Printf("\n  %s\n", fg(D, "No problem. Run /wizard again when you're ready."))
		return nil
	}

	// Phase 1: Plan
	fmt.Printf("\n\n  %s\n", fg(B, "── Plan Phase ──"))
	ctrl.SetPermissionMode(permission.ModePlan)
	ctx := context.Background()
	if err := ctrl.Plan(ctx, prompt); err != nil {
		fmt.Printf("  %s\n", fg(Rd, "plan failed: "+err.Error()))
		return err
	}

	// Execute
	fmt.Printf("\n  %s\n", fg(B, "── Building ──"))
	ctrl.SetPermissionMode(permission.ModeBypass)
	if err := ctrl.Run(ctx, prompt); err != nil {
		fmt.Printf("  %s\n", fg(Rd, "execution failed: "+err.Error()))
	}
	fmt.Printf("\n  %s\n", fg(G, "done. type /wizard to start another project."))

	return nil
}

func buildProjectPrompt(answers map[string]string) string {
	var sb strings.Builder
	sb.WriteString("Build a new software project based on the following requirements:\n\n")

	if v, ok := answers["project_type"]; ok {
		sb.WriteString(fmt.Sprintf("Project: %s\n", v))
	}
	if v, ok := answers["audience"]; ok {
		sb.WriteString(fmt.Sprintf("Audience: %s\n", v))
	}
	if v, ok := answers["features"]; ok {
		sb.WriteString(fmt.Sprintf("Features: %s\n", v))
	}
	if v, ok := answers["style"]; ok {
		sb.WriteString(fmt.Sprintf("Style: %s\n", v))
	}
	if v, ok := answers["tech_stack"]; ok {
		sb.WriteString(fmt.Sprintf("Tech stack: %s\n", v))
	}
	if v, ok := answers["deadline"]; ok {
		sb.WriteString(fmt.Sprintf("Deadline: %s\n", v))
	}
	if v, ok := answers["existing"]; ok {
		sb.WriteString(fmt.Sprintf("Existing project: %s\n", v))
	}
	if v, ok := answers["notes"]; ok {
		sb.WriteString(fmt.Sprintf("Notes: %s\n", v))
	}

	sb.WriteString("\nFirst, plan the architecture and file structure. Then implement it step by step.")
	sb.WriteString("\nUse reasonable defaults when details are not specified.")
	sb.WriteString("\nAfter implementation, create a README.md explaining what was built and how to run it.")

	return sb.String()
}
