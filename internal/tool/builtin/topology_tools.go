package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"lumen/internal/tool"
	"lumen/internal/topology"
)

func init() {
	tool.RegisterBuiltin(&TopologyBuildGraphTool{})
	tool.RegisterBuiltin(&CycleDetectionTool{})
	tool.RegisterBuiltin(&SPOFDetectionTool{})
	tool.RegisterBuiltin(&CriticalPathTool{})
}

type TopologyBuildGraphTool struct{}

func (t *TopologyBuildGraphTool) Name() string   { return "topology_build_graph" }
func (t *TopologyBuildGraphTool) ReadOnly() bool { return false }
func (t *TopologyBuildGraphTool) Description() string {
	return "Build a service topology graph from nodes and edges definitions."
}
func (t *TopologyBuildGraphTool) Schema() json.RawMessage {
	schema := `{
  "type": "object",
  "properties": {
    "nodes": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "id":   { "type": "string" },
          "name": { "type": "string" },
          "type": { "type": "string" }
        },
        "required": ["id"]
      }
    },
    "edges": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "from":   { "type": "string" },
          "to":     { "type": "string" },
          "weight": { "type": "number" }
        },
        "required": ["from", "to"]
      }
    }
  },
  "required": ["nodes", "edges"]
}`
	return json.RawMessage(schema)
}
func (t *TopologyBuildGraphTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Nodes []struct{ ID, Name, Type string }
		Edges []struct {
			From, To string
			Weight   float64
		}
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	g := topology.NewGraph()
	for _, n := range p.Nodes {
		g.AddNode(n.ID, n.Name, n.Type)
	}
	for _, e := range p.Edges {
		if e.Weight == 0 {
			e.Weight = 1
		}
		g.AddEdge(e.From, e.To, e.Weight)
	}
	return g.FormatGraph(), nil
}

type CycleDetectionTool struct{}

func (t *CycleDetectionTool) Name() string   { return "detect_cycles" }
func (t *CycleDetectionTool) ReadOnly() bool { return true }
func (t *CycleDetectionTool) Description() string {
	return "Detect cycles in a dependency graph. Provide edges."
}
func (t *CycleDetectionTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"edges":{"type":"array","items":{"type":"object","properties":{"from":{"type":"string"},"to":{"type":"string"}},"required":["from","to"]}}},"required":["edges"]}`)
}
func (t *CycleDetectionTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Edges []struct{ From, To string } }
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	g := topology.NewGraph()
	for _, e := range p.Edges {
		g.AddNode(e.From, e.From, "")
		g.AddNode(e.To, e.To, "")
		g.AddEdge(e.From, e.To, 1)
	}
	if g.HasCycle() {
		return "🔴 Cycle detected in the dependency graph!", nil
	}
	return "✅ No cycles detected.", nil
}

type SPOFDetectionTool struct{}

func (t *SPOFDetectionTool) Name() string   { return "detect_spof" }
func (t *SPOFDetectionTool) ReadOnly() bool { return true }
func (t *SPOFDetectionTool) Description() string {
	return "Detect single points of failure in a service graph."
}
func (t *SPOFDetectionTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"edges":{"type":"array","items":{"type":"object","properties":{"from":{"type":"string"},"to":{"type":"string"}},"required":["from","to"]}}},"required":["edges"]}`)
}
func (t *SPOFDetectionTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Edges []struct{ From, To string } }
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	g := topology.NewGraph()
	for _, e := range p.Edges {
		g.AddNode(e.From, e.From, "")
		g.AddNode(e.To, e.To, "")
		g.AddEdge(e.From, e.To, 1)
	}
	spof := g.SPOF()
	if len(spof) == 0 {
		return "No single points of failure found.", nil
	}
	return fmt.Sprintf("🔴 SPOFs detected: %v", spof), nil
}

type CriticalPathTool struct{}

func (t *CriticalPathTool) Name() string   { return "critical_path" }
func (t *CriticalPathTool) ReadOnly() bool { return true }
func (t *CriticalPathTool) Description() string {
	return "Find the critical path (longest weighted path) in a DAG."
}
func (t *CriticalPathTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"edges":{"type":"array","items":{"type":"object","properties":{"from":{"type":"string"},"to":{"type":"string"},"weight":{"type":"number"}},"required":["from","to"]}}},"required":["edges"]}`)
}
func (t *CriticalPathTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Edges []struct {
			From, To string
			Weight   float64
		}
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	g := topology.NewGraph()
	for _, e := range p.Edges {
		if e.Weight == 0 {
			e.Weight = 1
		}
		g.AddNode(e.From, e.From, "")
		g.AddNode(e.To, e.To, "")
		g.AddEdge(e.From, e.To, e.Weight)
	}
	path, weight := g.CriticalPath()
	return fmt.Sprintf("Critical path (weight=%.1f): %s", weight, strings.Join(path, " → ")), nil
}
