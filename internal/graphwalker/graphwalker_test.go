package graphwalker

import (
	"testing"
)

func TestBFS(t *testing.T) {
	g := NewAdjacencyGraph()
	g.AddEdge("a", "b", 1)
	g.AddEdge("a", "c", 1)
	g.AddEdge("b", "d", 1)
	r := BFS(g, "a")
	if len(r.Order) != 4 {
		t.Error("bfs order")
	}
	if r.Distances["d"] != 2 {
		t.Error("bfs dist")
	}
}
func TestDijkstra(t *testing.T) {
	g := NewAdjacencyGraph()
	g.AddEdge("a", "b", 3)
	g.AddEdge("a", "c", 1)
	g.AddEdge("c", "b", 1)
	r := Dijkstra(g, "a")
	if r.Distances["b"] != 2 {
		t.Error("dijkstra")
	}
}
func TestTopologicalSort(t *testing.T) {
	g := NewAdjacencyGraph()
	g.AddEdge("a", "b", 1)
	g.AddEdge("b", "c", 1)
	order, err := TopologicalSort(g)
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 3 {
		t.Error("topo")
	}
}
func TestSCC(t *testing.T) {
	g := NewAdjacencyGraph()
	g.AddEdge("a", "b", 1)
	g.AddEdge("b", "a", 1)
	r := TarjanSCC(g)
	if r.Count != 1 {
		t.Error("scc")
	}
}
func TestFormat(t *testing.T) {
	g := NewAdjacencyGraph()
	g.AddNode("x")
	if FormatGraph(g) == "" {
		t.Error("format")
	}
}
