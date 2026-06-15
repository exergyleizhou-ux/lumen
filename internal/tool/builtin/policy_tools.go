package builtin

import (
	"context"
	"encoding/json"
	

	"lumen/internal/policy"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&PolicyEvalTool{})
	tool.RegisterBuiltin(&PolicyListTool{})
	tool.RegisterBuiltin(&PolicyAuditLogTool{})
}

type PolicyEvalTool struct{}
func (t *PolicyEvalTool) Name() string { return "policy_evaluate" }
func (t *PolicyEvalTool) ReadOnly() bool { return true }
func (t *PolicyEvalTool) Description() string { return "Evaluate a policy rule against input data. Returns allow/deny/warn decision." }
func (t *PolicyEvalTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"policy":{"type":"string"},"input":{"type":"object"}},"required":["policy","input"]}`)
}
func (t *PolicyEvalTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Policy string; Input map[string]any }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }
	engine := policy.NewEngine()
	engine.Register(policy.DefaultSecurityPolicy())
	decision, err := engine.Evaluate("security.default", p.Input)
	if err != nil { return "", err }
	return policy.FormatDecision(decision), nil
}

type PolicyListTool struct{}
func (t *PolicyListTool) Name() string { return "policy_list" }
func (t *PolicyListTool) ReadOnly() bool { return true }
func (t *PolicyListTool) Description() string { return "List all registered policies and their rules." }
func (t *PolicyListTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{}}`) }
func (t *PolicyListTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	p := policy.DefaultSecurityPolicy()
	return policy.FormatPolicy(p), nil
}

type PolicyAuditLogTool struct{}
func (t *PolicyAuditLogTool) Name() string { return "policy_audit_log" }
func (t *PolicyAuditLogTool) ReadOnly() bool { return true }
func (t *PolicyAuditLogTool) Description() string { return "Show the policy decision audit log." }
func (t *PolicyAuditLogTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{}}`) }
func (t *PolicyAuditLogTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	engine := policy.NewEngine()
	log := engine.AuditLog()
	b, _ := json.MarshalIndent(log, "", "  ")
	return string(b), nil
}
