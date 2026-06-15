// Package graphwalker provides graph traversal algorithms: BFS, DFS,
// Dijkstra shortest path, A* search, topological sort, and strongly
// connected components (Tarjan's). Works on any graph implementing
// the Graph interface.
package graphwalker

import (
	"container/heap"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

// Graph is the graph interface for all algorithms.
type Graph interface {
	Nodes() []string
	Neighbors(node string) []Edge
}

// Edge is a weighted directed edge.
type Edge struct {
	To     string  `json:"to"`
	Weight float64 `json:"weight"`
}

// AdjacencyGraph is a simple adjacency-list graph.
type AdjacencyGraph struct {
	mu  sync.RWMutex
	adj map[string][]Edge
}

// NewAdjacencyGraph creates an adjacency-list graph.
func NewAdjacencyGraph() *AdjacencyGraph {
	return &AdjacencyGraph{adj: map[string][]Edge{}}
}

// AddNode adds a node.
func (g *AdjacencyGraph) AddNode(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.adj[id]; !ok {
		g.adj[id] = nil
	}
}

// AddEdge adds a directed edge.
func (g *AdjacencyGraph) AddEdge(from, to string, weight float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.adj[from] = append(g.adj[from], Edge{To: to, Weight: weight})
	if _, ok := g.adj[to]; !ok {
		g.adj[to] = nil
	}
}

func (g *AdjacencyGraph) Nodes() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []string
	for n := range g.adj {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (g *AdjacencyGraph) Neighbors(node string) []Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.adj[node]
}

// ── BFS ──────────────────────────────────────────────────

// BFSResult is the output of BFS traversal.
type BFSResult struct {
	Order     []string          `json:"order"`
	Distances map[string]int    `json:"distances"`
	Parents   map[string]string `json:"parents"`
}

// BFS performs breadth-first search from a start node.
func BFS(g Graph, start string) *BFSResult {
	result := &BFSResult{
		Distances: map[string]int{},
		Parents:   map[string]string{},
	}

	visited := map[string]bool{start: true}
	queue := []string{start}
	result.Distances[start] = 0

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		result.Order = append(result.Order, node)

		for _, edge := range g.Neighbors(node) {
			if !visited[edge.To] {
				visited[edge.To] = true
				result.Distances[edge.To] = result.Distances[node] + 1
				result.Parents[edge.To] = node
				queue = append(queue, edge.To)
			}
		}
	}
	return result
}

// ── DFS ──────────────────────────────────────────────────

// DFSResult is the output of DFS traversal.
type DFSResult struct {
	Order     []string `json:"order"`
	PreOrder  []string `json:"pre_order"`
	PostOrder []string `json:"post_order"`
}

// DFS performs depth-first search from a start node.
func DFS(g Graph, start string) *DFSResult {
	result := &DFSResult{}
	visited := map[string]bool{}
	var dfs func(node string)
	dfs = func(node string) {
		visited[node] = true
		result.PreOrder = append(result.PreOrder, node)
		result.Order = append(result.Order, node)
		for _, edge := range g.Neighbors(node) {
			if !visited[edge.To] {
				dfs(edge.To)
			}
		}
		result.PostOrder = append(result.PostOrder, node)
	}
	dfs(start)
	return result
}

// ── Dijkstra ──────────────────────────────────────────────

// ShortestPathResult is the output of shortest-path algorithms.
type ShortestPathResult struct {
	Distances map[string]float64 `json:"distances"`
	Parents   map[string]string  `json:"parents"`
	Reachable []string           `json:"reachable"`
}

// Dijkstra computes shortest paths from a start node.
func Dijkstra(g Graph, start string) *ShortestPathResult {
	result := &ShortestPathResult{
		Distances: map[string]float64{},
		Parents:   map[string]string{},
	}

	for _, n := range g.Nodes() {
		result.Distances[n] = math.Inf(1)
	}
	result.Distances[start] = 0

	pq := &minHeap{{node: start, dist: 0}}
	heap.Init(pq)
	visited := map[string]bool{}

	for pq.Len() > 0 {
		item := heap.Pop(pq).(*heapItem)
		if visited[item.node] {
			continue
		}
		visited[item.node] = true
		result.Reachable = append(result.Reachable, item.node)

		for _, edge := range g.Neighbors(item.node) {
			if visited[edge.To] {
				continue
			}
			newDist := result.Distances[item.node] + edge.Weight
			if newDist < result.Distances[edge.To] {
				result.Distances[edge.To] = newDist
				result.Parents[edge.To] = item.node
				heap.Push(pq, &heapItem{node: edge.To, dist: newDist})
			}
		}
	}
	return result
}

type heapItem struct {
	node string
	dist float64
}
type minHeap []*heapItem

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return h[i].dist < h[j].dist }
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x any)        { *h = append(*h, x.(*heapItem)) }
func (h *minHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// PathTo reconstructs the path from start to target.
func PathTo(parents map[string]string, target string) []string {
	var path []string
	for current := target; current != ""; current = parents[current] {
		path = append([]string{current}, path...)
	}
	return path
}

// ── A* Search ─────────────────────────────────────────────

// Heuristic is a function estimating cost from a node to goal.
type Heuristic func(node, goal string) float64

// AStar finds the shortest path using A* search.
func AStar(g Graph, start, goal string, h Heuristic) ([]string, float64, bool) {
	gScore := map[string]float64{start: 0}
	fScore := map[string]float64{start: h(start, goal)}
	parents := map[string]string{}
	visited := map[string]bool{}

	pq := &minHeap{{node: start, dist: fScore[start]}}
	heap.Init(pq)

	for pq.Len() > 0 {
		item := heap.Pop(pq).(*heapItem)
		if item.node == goal {
			return PathTo(parents, goal), gScore[goal], true
		}
		if visited[item.node] {
			continue
		}
		visited[item.node] = true

		for _, edge := range g.Neighbors(item.node) {
			if visited[edge.To] {
				continue
			}
			tentative := gScore[item.node] + edge.Weight
			if current, ok := gScore[edge.To]; !ok || tentative < current {
				parents[edge.To] = item.node
				gScore[edge.To] = tentative
				fScore[edge.To] = tentative + h(edge.To, goal)
				heap.Push(pq, &heapItem{node: edge.To, dist: fScore[edge.To]})
			}
		}
	}
	return nil, 0, false
}

// ManhattanHeuristic returns a Manhattan distance heuristic for grid graphs.
func ManhattanHeuristic(node, goal string) float64 { return 0 } // Stub — depends on node naming convention

// ── Topological Sort ─────────────────────────────────────

// TopologicalSort orders nodes in a DAG.
func TopologicalSort(g Graph) ([]string, error) {
	inDegree := map[string]int{}
	for _, n := range g.Nodes() {
		inDegree[n] = 0
	}
	for _, n := range g.Nodes() {
		for _, e := range g.Neighbors(n) {
			inDegree[e.To]++
		}
	}

	var queue []string
	for n, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, n)
		}
	}
	sort.Strings(queue)

	var order []string
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		order = append(order, n)
		for _, e := range g.Neighbors(n) {
			inDegree[e.To]--
			if inDegree[e.To] == 0 {
				queue = append(queue, e.To)
			}
		}
		sort.Strings(queue)
	}
	if len(order) != len(inDegree) {
		return nil, fmt.Errorf("graph contains a cycle")
	}
	return order, nil
}

// ── SCC (Tarjan's Algorithm) ──────────────────────────────

// SCCResult contains strongly connected components.
type SCCResult struct {
	Components [][]string `json:"components"`
	Count      int        `json:"count"`
}

// TarjanSCC finds strongly connected components using Tarjan's algorithm.
func TarjanSCC(g Graph) *SCCResult {
	index := 0
	stack := []string{}
	onStack := map[string]bool{}
	indices := map[string]int{}
	lowLinks := map[string]int{}
	var components [][]string

	var strongconnect func(node string)
	strongconnect = func(node string) {
		indices[node] = index
		lowLinks[node] = index
		index++
		stack = append(stack, node)
		onStack[node] = true

		for _, edge := range g.Neighbors(node) {
			if _, ok := indices[edge.To]; !ok {
				strongconnect(edge.To)
				if lowLinks[edge.To] < lowLinks[node] {
					lowLinks[node] = lowLinks[edge.To]
				}
			} else if onStack[edge.To] {
				if indices[edge.To] < lowLinks[node] {
					lowLinks[node] = indices[edge.To]
				}
			}
		}

		if lowLinks[node] == indices[node] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == node {
					break
				}
			}
			components = append(components, comp)
		}
	}

	for _, n := range g.Nodes() {
		if _, ok := indices[n]; !ok {
			strongconnect(n)
		}
	}

	return &SCCResult{Components: components, Count: len(components)}
}

// ── Formatting ────────────────────────────────────────────

// FormatGraph formats the adjacency graph.
func FormatGraph(g Graph) string {
	var sb strings.Builder
	nodes := g.Nodes()
	fmt.Fprintf(&sb, "Graph: %d nodes\n%s\n\n", len(nodes), strings.Repeat("─", 40))
	for _, n := range nodes {
		neighbors := g.Neighbors(n)
		fmt.Fprintf(&sb, "  %s", n)
		if len(neighbors) > 0 {
			var parts []string
			for _, e := range neighbors {
				parts = append(parts, fmt.Sprintf("%s(%.1f)", e.To, e.Weight))
			}
			fmt.Fprintf(&sb, " → %s", strings.Join(parts, ", "))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// FormatPath formats a path.
func FormatPath(path []string) string {
	if len(path) == 0 {
		return "no path"
	}
	return strings.Join(path, " → ")
}

// FormatSCC formats strongly connected components.
func FormatSCC(result *SCCResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "SCC: %d components\n%s\n\n", result.Count, strings.Repeat("─", 40))
	for i, comp := range result.Components {
		fmt.Fprintf(&sb, "  Component %d: %s\n", i+1, strings.Join(comp, ", "))
	}
	return sb.String()
}
