// Package topology builds and analyzes service topology graphs from agent
// call traces and dependency declarations. It detects cycles, computes
// critical paths, and identifies single points of failure.
package topology

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Node is a service in the topology.
type Node struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Edge is a directed dependency between two nodes.
type Edge struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	Weight    float64 `json:"weight"`
	CallCount int64   `json:"call_count"`
}

// Graph is a directed service topology graph.
type Graph struct {
	mu    sync.RWMutex
	nodes map[string]*Node
	edges []*Edge
}

// NewGraph creates a topology graph.
func NewGraph() *Graph {
	return &Graph{nodes: map[string]*Node{}}
}

// AddNode inserts or updates a node.
func (g *Graph) AddNode(id, name, typ string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nodes[id] = &Node{ID: id, Name: name, Type: typ}
}

// AddEdge adds a directed dependency.
func (g *Graph) AddEdge(from, to string, weight float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.edges = append(g.edges, &Edge{From: from, To: to, Weight: weight})
}

// Node returns a node by ID.
func (g *Graph) Node(id string) *Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.nodes[id]
}

// Nodes returns all nodes.
func (g *Graph) Nodes() []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]*Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Edges returns all edges.
func (g *Graph) Edges() []*Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]*Edge, len(g.edges))
	copy(out, g.edges)
	return out
}

// Outgoing returns edges going from the given node.
func (g *Graph) Outgoing(nodeID string) []*Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []*Edge
	for _, e := range g.edges {
		if e.From == nodeID {
			out = append(out, e)
		}
	}
	return out
}

// Incoming returns edges going to the given node.
func (g *Graph) Incoming(nodeID string) []*Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.incomingLocked(nodeID)
}

// incomingLocked assumes the caller holds the lock.
func (g *Graph) incomingLocked(nodeID string) []*Edge {
	var out []*Edge
	for _, e := range g.edges {
		if e.To == nodeID {
			out = append(out, e)
		}
	}
	return out
}

// HasCycle detects whether the graph contains a cycle.
func (g *Graph) HasCycle() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	state := map[string]int{} // 0=unvisited, 1=visiting, 2=visited
	var dfs func(id string) bool
	dfs = func(id string) bool {
		if state[id] == 1 {
			return true
		}
		if state[id] == 2 {
			return false
		}
		state[id] = 1
		for _, e := range g.edges {
			if e.From == id && dfs(e.To) {
				return true
			}
		}
		state[id] = 2
		return false
	}
	for id := range g.nodes {
		if dfs(id) {
			return true
		}
	}
	return false
}

// CriticalPath finds the longest weighted path through the DAG.
func (g *Graph) CriticalPath() ([]string, float64) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Topological sort (assumes DAG)
	inDeg := map[string]int{}
	for id := range g.nodes {
		inDeg[id] = 0
	}
	for _, e := range g.edges {
		inDeg[e.To]++
	}

	var queue []string
	for id, d := range inDeg {
		if d == 0 {
			queue = append(queue, id)
		}
	}

	order := []string{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)
		for _, e := range g.edges {
			if e.From == id {
				inDeg[e.To]--
				if inDeg[e.To] == 0 {
					queue = append(queue, e.To)
				}
			}
		}
	}

	dist := map[string]float64{}
	prev := map[string]string{}
	for id := range g.nodes {
		dist[id] = -1
	}
	for _, id := range order {
		dist[id] = 0
	}

	for _, id := range order {
		for _, e := range g.edges {
			if e.From == id {
				nd := dist[id] + e.Weight
				if nd > dist[e.To] {
					dist[e.To] = nd
					prev[e.To] = id
				}
			}
		}
	}

	var end string
	maxDist := -1.0
	for id, d := range dist {
		if d > maxDist {
			maxDist = d
			end = id
		}
	}
	if maxDist < 0 {
		return nil, 0
	}

	path := []string{end}
	for prev[end] != "" {
		end = prev[end]
		path = append([]string{end}, path...)
	}
	return path, maxDist
}

// SPOF finds single points of failure (nodes with exactly one path to them).
func (g *Graph) SPOF() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	result := map[string]bool{}
	for _, e := range g.edges {
		result[e.To] = true
	}
	for _, e := range g.edges {
		if len(g.incomingLocked(e.To)) > 1 {
			delete(result, e.To)
		}
	}
	var out []string
	for id := range result {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Diameter returns the node with the highest degree.
func (g *Graph) HubNodes(n int) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	deg := map[string]int{}
	for _, e := range g.edges {
		deg[e.From]++
		deg[e.To]++
	}
	type kv struct {
		id  string
		deg int
	}
	var pairs []kv
	for id, d := range deg {
		pairs = append(pairs, kv{id, d})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].deg > pairs[j].deg })
	var out []string
	for i := 0; i < n && i < len(pairs); i++ {
		out = append(out, pairs[i].id)
	}
	return out
}

// ToJSON marshals the graph to JSON.
func (g *Graph) ToJSON() ([]byte, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	type out struct {
		Nodes []*Node `json:"nodes"`
		Edges []*Edge `json:"edges"`
	}
	o := out{Edges: g.edges}
	for _, n := range g.nodes {
		o.Nodes = append(o.Nodes, n)
	}
	return json.MarshalIndent(o, "", "  ")
}

// FormatGraph returns a text representation of the graph.
func (g *Graph) FormatGraph() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Topology: %d nodes, %d edges\n%s\n\n", len(g.nodes), len(g.edges), strings.Repeat("─", 40))
	for _, n := range g.Nodes() {
		fmt.Fprintf(&sb, "  [%s] %s (%s)\n", n.ID, n.Name, n.Type)
		for _, e := range g.Outgoing(n.ID) {
			fmt.Fprintf(&sb, "    → %s (w:%.1f)\n", e.To, e.Weight)
		}
	}
	if g.HasCycle() {
		sb.WriteString("\n⚠️  Cycle detected!\n")
	}
	spof := g.SPOF()
	if len(spof) > 0 {
		fmt.Fprintf(&sb, "\n🔴 SPOFs: %v\n", spof)
	}
	cp, w := g.CriticalPath()
	if len(cp) > 0 {
		fmt.Fprintf(&sb, "\nCritical Path (%.1f): %s\n", w, strings.Join(cp, " → "))
	}
	return sb.String()
}
