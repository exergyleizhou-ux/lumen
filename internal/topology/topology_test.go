package topology

import ("strings";"testing")

func TestGraphBasic(t *testing.T) {
	g := NewGraph(); g.AddNode("a", "ServiceA", "http"); g.AddNode("b", "ServiceB", "http")
	g.AddEdge("a", "b", 1.0)
	if len(g.Nodes()) != 2 { t.Error("node count") }
	if len(g.Edges()) != 1 { t.Error("edge count") }
}
func TestHasCycle(t *testing.T) {
	g := NewGraph(); g.AddNode("a", "A", ""); g.AddNode("b", "B", ""); g.AddNode("c", "C", "")
	g.AddEdge("a", "b", 1); g.AddEdge("b", "c", 1); g.AddEdge("c", "a", 1)
	if !g.HasCycle() { t.Error("should detect cycle") }
}
func TestNoCycle(t *testing.T) {
	g := NewGraph(); g.AddNode("a", "A", ""); g.AddNode("b", "B", "")
	g.AddEdge("a", "b", 1)
	if g.HasCycle() { t.Error("should not detect cycle") }
}
func TestSPOF(t *testing.T) {
	g := NewGraph(); g.AddNode("a", "A", ""); g.AddNode("b", "B", ""); g.AddNode("c", "C", "")
	g.AddEdge("a", "c", 1); g.AddEdge("b", "c", 1) // c has 2 incoming => not SPOF
	spof := g.SPOF()
	for _, s := range spof { if s == "c" { t.Error("c should not be SPOF") } }
}
func TestFormatGraph(t *testing.T) {
	g := NewGraph(); g.AddNode("n", "Node", "t")
	s := g.FormatGraph()
	if !strings.Contains(s, "Node") { t.Error("format") }
}
func TestCriticalPath(t *testing.T) {
	g := NewGraph(); g.AddNode("a", "A", ""); g.AddNode("b", "B", "")
	g.AddEdge("a", "b", 5.0)
	path, w := g.CriticalPath()
	if len(path) != 2 || w != 5.0 { t.Error("critical path") }
}
