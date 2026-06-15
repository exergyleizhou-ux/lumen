package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"lumen/internal/orchestrator"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&RunWorkflowTool{})
	tool.RegisterBuiltin(&CreateWorkflowTool{})
	tool.RegisterBuiltin(&ListWorkflowsTool{})
}

// ── Shared executor ─────────────────────────────────────────────────────────

var (
	orchestratorExecutor *orchestrator.Executor
	orchestratorOnce     sync.Once
	workflowPlans        = map[string]*orchestrator.Plan{}
	workflowMu           sync.Mutex
)

func getExecutor() *orchestrator.Executor {
	orchestratorOnce.Do(func() {
		pool := orchestrator.NewAgentPool()
		cfg := orchestrator.DefaultConfig()
		orchestratorExecutor = orchestrator.NewExecutor(cfg, pool)
	})
	return orchestratorExecutor
}

// ── run_workflow ────────────────────────────────────────────────────────────

type RunWorkflowTool struct{}

func (t *RunWorkflowTool) Name() string   { return "run_workflow" }
func (t *RunWorkflowTool) ReadOnly() bool { return false }

func (t *RunWorkflowTool) Description() string {
	return "Execute a registered workflow by name. Provide the workflow name as registered via create_workflow."
}

func (t *RunWorkflowTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "workflow_name":{"type":"string","description":"Name of the workflow to execute"}
},
"required":["workflow_name"]
}`)
}

func (t *RunWorkflowTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		WorkflowName string `json:"workflow_name"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.WorkflowName == "" {
		return "", fmt.Errorf("workflow_name is required")
	}

	workflowMu.Lock()
	plan, ok := workflowPlans[p.WorkflowName]
	workflowMu.Unlock()
	if !ok {
		return "", fmt.Errorf("workflow %q not found", p.WorkflowName)
	}

	exec := getExecutor()
	if err := exec.Execute(ctx, plan); err != nil {
		return "", fmt.Errorf("workflow execution failed: %w", err)
	}

	return exec.FormatPlan(plan), nil
}

// ── create_workflow ─────────────────────────────────────────────────────────

type CreateWorkflowTool struct{}

func (t *CreateWorkflowTool) Name() string   { return "create_workflow" }
func (t *CreateWorkflowTool) ReadOnly() bool { return false }

func (t *CreateWorkflowTool) Description() string {
	return "Register a new workflow from a JSON definition. Provide a workflow name and an array of task objects, each with id, name, agent, prompt, and optional depends_on."
}

func (t *CreateWorkflowTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "name":{"type":"string","description":"Workflow name"},
  "tasks":{"type":"array","items":{"type":"object"},"description":"Array of task objects with id, name, agent, prompt, and optional depends_on (array of task IDs)"}
},
"required":["name","tasks"]
}`)
}

func (t *CreateWorkflowTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Name  string            `json:"name"`
		Tasks []json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Name == "" {
		return "", fmt.Errorf("name is required")
	}
	if len(p.Tasks) == 0 {
		return "", fmt.Errorf("at least one task is required")
	}

	var tasks []*orchestrator.Task
	for i, raw := range p.Tasks {
		var t struct {
			ID        string   `json:"id"`
			Name      string   `json:"name"`
			Agent     string   `json:"agent"`
			Prompt    string   `json:"prompt"`
			DependsOn []string `json:"depends_on"`
		}
		if err := json.Unmarshal(raw, &t); err != nil {
			return "", fmt.Errorf("invalid task at index %d: %w", i, err)
		}
		if t.ID == "" {
			t.ID = fmt.Sprintf("task-%d", i)
		}
		tasks = append(tasks, &orchestrator.Task{
			ID:        t.ID,
			Name:      t.Name,
			Agent:     t.Agent,
			Prompt:    t.Prompt,
			DependsOn: t.DependsOn,
			MaxRetries: 2,
		})
	}

	exec := getExecutor()
	plan := exec.NewPlan(p.Name, tasks)

	workflowMu.Lock()
	workflowPlans[p.Name] = plan
	workflowMu.Unlock()

	out := map[string]interface{}{
		"name":    p.Name,
		"plan_id": plan.ID,
		"tasks":   len(tasks),
		"status":  string(plan.Status),
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}

// ── list_workflows ──────────────────────────────────────────────────────────

type ListWorkflowsTool struct{}

func (t *ListWorkflowsTool) Name() string   { return "list_workflows" }
func (t *ListWorkflowsTool) ReadOnly() bool { return true }

func (t *ListWorkflowsTool) Description() string {
	return "List all registered workflow names."
}

func (t *ListWorkflowsTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *ListWorkflowsTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	workflowMu.Lock()
	defer workflowMu.Unlock()

	var names []string
	for name := range workflowPlans {
		names = append(names, name)
	}

	out := map[string]interface{}{
		"count":     len(names),
		"workflows": names,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}
