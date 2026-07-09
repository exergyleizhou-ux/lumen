package lab

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"lumen/internal/config"
	"lumen/internal/control"
	"lumen/internal/event"
	"lumen/internal/permission"
	"lumen/internal/science/lab/project"
	"lumen/internal/science/lab/provenance"
	labruntime "lumen/internal/science/lab/runtime"
	"lumen/internal/science/lab/tools"
	"lumen/internal/science/lab/workspace"
	"lumen/internal/skill"
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
	guard      *workspace.Guard
	provenance *provenance.Recorder
}

// NewController builds a lab agent controller.
func NewController(sciDir string, fleet *labruntime.FleetManager, projects *project.Store) *Controller {
	return &Controller{
		sciDir:   sciDir,
		fleet:    fleet,
		projects: projects,
		ctrl:     control.New(),
	}
}

// Configure prepares the agent for a project workspace.
func (c *Controller) Configure(slug, sessionID string, sink event.Sink, approver func(context.Context, string, json.RawMessage) (bool, error)) error {
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
	g, err := workspace.NewGuard(ws)
	if err != nil {
		return err
	}
	c.guard = g
	_ = os.Setenv("LUMEN_WORKSPACE_ROOT", ws)

	projDir, err := c.projects.ProjectDir(slug)
	if err != nil {
		return err
	}
	rec, err := provenance.NewRecorder(projDir, sessionID, os.Getenv("LUMEN_SCIENCE_MODEL"))
	if err != nil {
		return err
	}
	c.provenance = rec

	sciCfg, err := scienceConfig(c.sciDir)
	if err != nil {
		return err
	}
	if _, _, err := ApplyScienceProfile(sciCfg); err != nil {
		return err
	}

	_ = os.Setenv("LUMEN_TOOLS_PROFILE", defaultToolProfile)
	// Inject conda/python from the cloned research pack into bash PATH.
	labruntime.InjectLabPath(c.sciDir)

	c.ctrl.SetExtraMemoryPrompt(scienceSystemPrompt)
	sink = wrapProvenanceSink(sink, c.provenance, g)
	if err := c.ctrl.Configure(sink, nil, lumenCfgPath); err != nil {
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

	home, _ := os.UserHomeDir()
	skillPaths := []string{
		filepath.Join(c.sciDir, "skills"),
		filepath.Join(projDir, ".lumen", "skills"),
	}
	if packSkills := labruntime.SkillsDir(c.sciDir); packSkills != "" {
		skillPaths = append(skillPaths, packSkills)
	}
	_ = skill.New(skill.Options{
		HomeDir:     home,
		ProjectRoot: ws,
		CustomPaths: skillPaths,
	})
	return nil
}

// Run executes one chat turn. File tools resolve paths via LUMEN_WORKSPACE_ROOT
// (no process-wide chdir — builtins stay isolated to the project workspace).
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
func (a *API) makeApprover(emit func(kind string, payload map[string]any)) func(context.Context, string, json.RawMessage) (bool, error) {
	return func(ctx context.Context, toolName string, args json.RawMessage) (bool, error) {
		if a.approvals == nil {
			return false, fmt.Errorf("approval hub not configured")
		}
		// emit signature for hub uses map; adapt string errors via payload
		return a.approvals.decide(ctx, toolName, args, func(kind string, payload map[string]any) {
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
