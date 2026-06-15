package ast

import (
	"testing"
)

func TestTree(t *testing.T) {
	root := NewNode(NodeFile, "main.go")
	fn := NewNode(NodeFunc, "main")
	fn.SetAttr("params", "()")
	call := NewNode(NodeCall, "fmt.Println")
	fn.AddChild(call)
	root.AddChild(fn)
	tr := NewTree()
	tr.SetRoot(root)
	if tr.CountNodes() != 3 {
		t.Error("node count")
	}
	funcs := tr.FindByType(NodeFunc)
	if len(funcs) != 1 {
		t.Error("func count")
	}
	nodes := tr.FindByName("main")
	if len(nodes) != 1 {
		t.Error("find by name")
	}
}
func TestWalk(t *testing.T) {
	root := NewNode(NodeFile, "")
	root.AddChild(NewNode(NodeFunc, "a"))
	root.AddChild(NewNode(NodeFunc, "b"))
	count := 0
	root.Walk(func(n *Node, d int) bool { count++; return true })
	if count != 3 {
		t.Error("walk count")
	}
}
func TestFormat(t *testing.T) {
	tr := NewTree()
	tr.SetRoot(NewNode(NodeFile, "test.go"))
	if tr.FormatTree() == "" {
		t.Error("format")
	}
}
