package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"lumen/internal/graphwalker"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&GraphBFSTool{})
	tool.RegisterBuiltin(&GraphDijkstraTool{})
	tool.RegisterBuiltin(&GraphTopologicalSortTool{})
	tool.RegisterBuiltin(&GraphSCCTool{})
}

// ── graph_bfs ────────────────────────────────────────────────────────────────

type GraphBFSTool struct{}

func (t *GraphBFSTool) Name() string   { return "graph_bfs" }
func (t *GraphBFSTool) ReadOnly() bool { return true }

func (t *GraphBFSTool) Description() string {
	return "Perform breadth-first search on a graph. Provide an adjacency list as a JSON map[string][]string and a start node. Returns BFS order, distances, and parent pointers."
}

func (t *GraphBFSTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "graph":{"type":"object","description":"Adjacency list: map from node name to list of neighbor node names"},
  "start":{"type":"string","description":"Start node name"}
},
"required":["graph","start"]
}`)
}

func (t *GraphBFSTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Graph map[string][]string `json:"graph"`
		Start string              `json:"start"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Start == "" {
		return "", fmt.Errorf("start node is required")
	}

	g := graphwalker.NewAdjacencyGraph()
	for from, tos := range p.Graph {
		g.AddNode(from)
		for _, to := range tos {
			g.AddEdge(from, to, 1)
		}
	}

	result := graphwalker.BFS(g, p.Start)
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}

// ── graph_dijkstra ──────────────────────────────────────────────────────────

type GraphDijkstraTool struct{}

func (t *GraphDijkstraTool) Name() string   { return "graph_dijkstra" }
func (t *GraphDijkstraTool) ReadOnly() bool { return true }

func (t *GraphDijkstraTool) Description() string {
	return "Compute shortest paths from a start node using Dijkstra's algorithm. Provide a weighted adjacency list as JSON map[string][{\"to\":\"...\",\"weight\":N}] and a start node. Returns shortest distances and parent pointers."
}

func (t *GraphDijkstraTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "graph":{"type":"object","description":"Weighted adjacency list: map from node name to array of {\"to\":\"...\",\"weight\":N} objects"},
  "start":{"type":"string","description":"Start node name"}
},
"required":["graph","start"]
}`)
}

func (t *GraphDijkstraTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Graph map[string][]json.RawMessage `json:"graph"`
		Start string                       `json:"start"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Start == "" {
		return "", fmt.Errorf("start node is required")
	}

	g := graphwalker.NewAdjacencyGraph()
	for from, edges := range p.Graph {
		g.AddNode(from)
		for _, raw := range edges {
			var e struct {
				To     string  `json:"to"`
				Weight float64 `json:"weight"`
			}
			if err := json.Unmarshal(raw, &e); err != nil {
				return "", fmt.Errorf("invalid edge from %q: %w", from, err)
			}
			g.AddEdge(from, e.To, e.Weight)
		}
	}

	result := graphwalker.Dijkstra(g, p.Start)

	// Reconstruct paths for each reachable node
	paths := make(map[string][]string)
	for _, node := range result.Reachable {
		paths[node] = graphwalker.PathTo(result.Parents, node)
	}

	out := map[string]interface{}{
		"distances": result.Distances,
		"parents":   result.Parents,
		"reachable": result.Reachable,
		"paths":     paths,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}

// ── graph_topological_sort ──────────────────────────────────────────────────

type GraphTopologicalSortTool struct{}

func (t *GraphTopologicalSortTool) Name() string   { return "graph_topological_sort" }
func (t *GraphTopologicalSortTool) ReadOnly() bool { return true }

func (t *GraphTopologicalSortTool) Description() string {
	return "Compute a topological ordering of a directed acyclic graph. Provide an adjacency list as JSON map[string][]string. Returns the topological order of nodes, or an error if the graph contains a cycle."
}

func (t *GraphTopologicalSortTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "graph":{"type":"object","description":"Adjacency list: map from node name to list of neighbor node names"}
},
"required":["graph"]
}`)
}

func (t *GraphTopologicalSortTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Graph map[string][]string `json:"graph"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	g := graphwalker.NewAdjacencyGraph()
	for from, tos := range p.Graph {
		g.AddNode(from)
		for _, to := range tos {
			g.AddEdge(from, to, 1)
		}
	}

	order, err := graphwalker.TopologicalSort(g)
	if err != nil {
		return "", err
	}
	b, _ := json.MarshalIndent(map[string]interface{}{"order": order}, "", "  ")
	return string(b), nil
}

// ── graph_scc ───────────────────────────────────────────────────────────────

type GraphSCCTool struct{}

func (t *GraphSCCTool) Name() string   { return "graph_scc" }
func (t *GraphSCCTool) ReadOnly() bool { return true }

func (t *GraphSCCTool) Description() string {
	return "Find strongly connected components in a directed graph using Tarjan's algorithm. Provide an adjacency list as JSON map[string][]string. Returns the SCCs and their count."
}

func (t *GraphSCCTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "graph":{"type":"object","description":"Adjacency list: map from node name to list of neighbor node names"}
},
"required":["graph"]
}`)
}

func (t *GraphSCCTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Graph map[string][]string `json:"graph"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	g := graphwalker.NewAdjacencyGraph()
	for from, tos := range p.Graph {
		g.AddNode(from)
		for _, to := range tos {
			g.AddEdge(from, to, 1)
		}
	}

	result := graphwalker.TarjanSCC(g)
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}
