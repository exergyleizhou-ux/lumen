package lab

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"lumen/internal/config"
	"lumen/internal/control"
	"lumen/internal/event"
	"lumen/internal/permission"
	"lumen/internal/runstate"
	"lumen/internal/science/lab/project"
	"lumen/internal/science/lab/provenance"
	labruntime "lumen/internal/science/lab/runtime"
	"lumen/internal/science/lab/tools"
	labworkspace "lumen/internal/science/lab/workspace"
	"lumen/internal/skill"
	runworkspace "lumen/internal/workspace"
)

//go:embed prompts/science_system.txt
var scienceSystemPrompt string

// Controller wraps the Lumen agent for lab chat turns.
type Controller struct {
	mu         sync.Mutex
	sciDir     string
	fleet      *labruntime.FleetManager
	projects   *project.Store
	ctrl       *control.Controller
	slug       string
	sessID     string
	workspace  string
	guard      *labworkspace.Guard
	provenance *provenance.Recorder
	provider   *config.ProviderConfig
	basePATH   string
}

// NewController builds a lab agent controller.
func NewController(sciDir string, fleet *labruntime.FleetManager, projects *project.Store) *Controller {
	return &Controller{
		sciDir:   sciDir,
		fleet:    fleet,
		projects: projects,
		ctrl:     control.New(),
		basePATH: os.Getenv("PATH"),
	}
}

func newControllerWithPlatformProvider(sciDir string, fleet *labruntime.FleetManager, projects *project.Store, pc *config.ProviderConfig, basePATH string) *Controller {
	c := NewController(sciDir, fleet, projects)
	c.basePATH = basePATH
	if pc != nil {
		copy := *pc
		c.provider = &copy
	}
	return c
}

func (c *Controller) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctrl != nil {
		c.ctrl.Close()
	}
}
func (c *Controller) WorkspaceContext() runworkspace.Context {
	if c == nil || c.ctrl == nil {
		return runworkspace.Context{}
	}
	return c.ctrl.WorkspaceContext()
}

func (c *Controller) ProviderConfig() *config.ProviderConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctrl == nil {
		return nil
	}
	return c.ctrl.ProviderConfig()
}

func (c *Controller) SetMaxSteps(v int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctrl != nil {
		c.ctrl.SetMaxSteps(v)
	}
}

// Configure prepares the agent for a project workspace.
func (c *Controller) Configure(slug, sessionID string, sink event.Sink, approver permission.Asker) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.slug = slug
	c.sessID = sessionID

	lumenCfgPath := config.FindConfig()
	ws, err := c.projects.WorkspacePath(slug)
	if err != nil {
		return err
	}
	c.workspace = ws
	g, err := labworkspace.NewGuard(ws)
	if err != nil {
		return err
	}
	c.guard = g
	runWS, err := runworkspace.NewLocal(slug, ws, "", map[string]string{
		"PATH": labruntime.LabPath(c.sciDir, c.runtimePATH()),
	})
	if err != nil {
		return err
	}

	projDir, err := c.projects.ProjectDir(slug)
	if err != nil {
		return err
	}
	var providerCfg config.ProviderConfig
	if c.provider != nil {
		providerCfg = *c.provider
	} else {
		sciCfg, err := scienceConfig(c.sciDir)
		if err != nil {
			return err
		}
		providerCfg, _, _, err = ScienceProviderConfig(sciCfg)
		if err != nil {
			return err
		}
	}
	rec, err := provenance.NewRecorder(projDir, sessionID, providerCfg.Model)
	if err != nil {
		return err
	}
	c.provenance = rec

	// Build system prompt: science base + enabled project skills (bodies).
	mem := scienceSystemPrompt
	if skillBlock := c.buildEnabledSkillsPrompt(slug, ws, projDir); skillBlock != "" {
		mem = mem + "\n\n" + skillBlock
	}
	c.ctrl.SetExtraMemoryPrompt(mem)
	sink = wrapProvenanceSink(sink, c.provenance, g)
	if err := c.ctrl.ConfigureWithOptions(sink, nil, lumenCfgPath, control.ConfigureOptions{
		Workspace:           runWS,
		ToolsProfile:        defaultToolProfile,
		Provider:            &providerCfg,
		ProcessEnvImmutable: true,
		ProviderOnly:        c.provider != nil,
	}); err != nil {
		return err
	}
	c.ctrl.SetPermissionMode(permission.ModePlan)
	if approver != nil {
		c.ctrl.SetApprover(approver)
	}

	extra := tools.RegisterFleet(c.fleet, c.provenance)
	briefTool := &tools.BriefGenerateTool{
		SciDir:      c.sciDir,
		ProjectRoot: ws,
		Projects:    c.projects,
		Guard:       g,
	}
	extra = append(extra, briefTool)
	c.ctrl.AddExtraTools(extra)
	return nil
}

func (c *Controller) runtimePATH() string { return c.basePATH }

// buildEnabledSkillsPrompt injects enabled skill bodies into the system prompt.
// If no enable list is saved, injects a short catalog of names only (not full bodies)
// to avoid huge prompts; when enable list is set, inject full body for each enabled skill.
func (c *Controller) buildEnabledSkillsPrompt(slug, ws, projDir string) string {
	home, _ := os.UserHomeDir()
	skillPaths := []string{
		filepath.Join(home, ".lumen", "skills"),
		filepath.Join(home, ".lumen", "imported-skills"),
		filepath.Join(c.sciDir, "skills"),
		filepath.Join(projDir, ".lumen", "skills"),
	}
	if packSkills := labruntime.SkillsDir(c.sciDir); packSkills != "" {
		skillPaths = append(skillPaths, packSkills)
	}
	store := skill.New(skill.Options{
		HomeDir:     home,
		ProjectRoot: ws,
		CustomPaths: skillPaths,
	})
	list := store.List()
	if len(list) == 0 {
		return ""
	}
	enabled, _ := c.projects.LoadEnabledSkills(slug)
	enSet := map[string]bool{}
	for _, n := range enabled {
		enSet[n] = true
	}
	filter := len(enabled) > 0

	var b strings.Builder
	if filter {
		b.WriteString("## Enabled science skills (follow these when relevant)\n")
		for _, sk := range list {
			if !enSet[sk.Name] {
				continue
			}
			b.WriteString("\n### Skill: ")
			b.WriteString(sk.Name)
			b.WriteString("\n")
			if sk.Description != "" {
				b.WriteString(sk.Description)
				b.WriteString("\n")
			}
			body := strings.TrimSpace(sk.Body)
			if body != "" {
				// Cap each skill body to keep prompt bounded
				runes := []rune(body)
				if len(runes) > 6000 {
					body = string(runes[:6000]) + "\n…[skill truncated]…"
				}
				b.WriteString(body)
				b.WriteString("\n")
			}
		}
	} else {
		b.WriteString("## Available science skills (catalog)\n")
		b.WriteString("User has not filtered skills; prefer these when the task matches:\n")
		n := 0
		for _, sk := range list {
			b.WriteString("- **")
			b.WriteString(sk.Name)
			b.WriteString("**: ")
			b.WriteString(sk.Description)
			b.WriteString("\n")
			n++
			if n >= 40 {
				b.WriteString("…\n")
				break
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// Run executes one chat turn. File and shell tools receive the immutable project
// workspace through context.Context; no process-wide chdir/env mutation occurs.
func (c *Controller) Run(ctx context.Context, prompt, mode string) error {
	c.mu.Lock()
	ctrl := c.ctrl
	c.mu.Unlock()
	if ctrl == nil {
		return fmt.Errorf("lab controller not configured")
	}
	switch mode {
	case "plan", "":
		ctrl.SetPermissionMode(permission.ModePlan)
		return ctrl.Plan(ctx, prompt)
	case "bypass":
		ctrl.SetPermissionMode(permission.ModeBypass)
		return ctrl.Run(ctx, prompt)
	case "agent", "default":
		ctrl.SetPermissionMode(permission.ModeDefault)
		return ctrl.Run(ctx, prompt)
	default:
		ctrl.SetPermissionMode(permission.ParseMode(mode))
		return ctrl.Run(ctx, prompt)
	}
}

// BindRun redirects subsequent events through the shared Runtime sink while
// preserving Lab provenance capture for this project controller.
func (c *Controller) BindRun(runID string, sink event.Sink) {
	c.mu.Lock()
	ctrl := c.ctrl
	rec := c.provenance
	guard := c.guard
	c.mu.Unlock()
	if rec != nil {
		rec.SetRunID(runID)
	}
	if ctrl != nil {
		ctrl.SetSink(wrapProvenanceSink(sink, rec, guard))
	}
}

func (c *Controller) Workspace() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.workspace
}

func (c *Controller) SessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessID
}

// PermissionMode returns the current agent permission mode (for approval hub).
func (c *Controller) PermissionMode() permission.Mode {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctrl == nil {
		return permission.ModeDefault
	}
	return c.ctrl.PermissionMode()
}

// makeApprover builds an SSE approval handler that blocks until /api/lab/approve.
// May return edited args JSON when the user modifies the approval card.
func (a *API) makeApprover(emit func(kind string, payload map[string]any)) permission.Asker {
	return a.makeOwnedApprover(runstate.LocalOwner, emit)
}

func (a *API) makeOwnedApprover(owner runstate.Owner, emit func(kind string, payload map[string]any)) permission.Asker {
	return func(ctx context.Context, toolName string, args json.RawMessage) (bool, json.RawMessage, error) {
		if a.approvals == nil {
			return false, nil, fmt.Errorf("approval hub not configured")
		}
		return a.approvals.decideOwned(ctx, owner, toolName, args, func(kind string, payload map[string]any) {
			if kind == "error" {
				if t, ok := payload["text"].(string); ok {
					emit("error", map[string]any{"text": t})
					return
				}
			}
			emit(kind, payload)
		})
	}
}
