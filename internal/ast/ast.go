// Package ast provides abstract syntax tree utilities for code analysis,
// transformation, and generation. Supports a simplified Go-like AST that
// agents can use to analyze and modify code structures.
package ast

import ("fmt";"sort";"strings";"sync")

type NodeType string
const (NodeFile NodeType="File";NodeFunc NodeType="FuncDecl";NodeStruct NodeType="StructDecl";NodeCall NodeType="CallExpr";NodeIdent NodeType="Ident";NodeImport NodeType="Import")

type Node struct{Type NodeType;Name string;Children []*Node;Attrs map[string]string;Line int;Col int}
func NewNode(typ NodeType,name string)*Node{return &Node{Type:typ,Name:name,Attrs:map[string]string{}}}
func(n*Node)AddChild(child *Node){n.Children=append(n.Children,child)}
func(n*Node)SetAttr(k,v string){n.Attrs[k]=v}
func(n*Node)FindAllType(typ NodeType)[]*Node{var out []*Node;if n.Type==typ{out=append(out,n)};for _,c:=range n.Children{out=append(out,c.FindAllType(typ)...)};return out}
func(n*Node)Walk(fn func(*Node,int)bool){if !fn(n,0){return};for _,c:=range n.Children{c.Walk(func(node*Node,depth int)bool{return fn(node,depth+1)})}}
func(n*Node)FormatNode()string{var sb strings.Builder;n.writeTo(&sb,0);return sb.String()}
func(n*Node)writeTo(sb*strings.Builder,depth int){
  indent:=strings.Repeat("  ",depth)
  fmt.Fprintf(sb,"%s%s: %s",indent,n.Type,n.Name)
  if len(n.Attrs)>0{fmt.Fprintf(sb," %v",n.Attrs)}
  if n.Line>0{fmt.Fprintf(sb," [L%d]",n.Line)}
  sb.WriteByte('\n')
  for _,c:=range n.Children{c.writeTo(sb,depth+1)}
}

type Tree struct{mu sync.Mutex;root *Node;index map[string][]*Node}
func NewTree()*Tree{return &Tree{root:NewNode(NodeFile,""),index:map[string][]*Node{}}}
func(t*Tree)SetRoot(n *Node){t.mu.Lock();defer t.mu.Unlock();t.root=n;t.reindex()}
func(t*Tree)Root()*Node{t.mu.Lock();defer t.mu.Unlock();return t.root}
func(t*Tree)FindByName(name string)[]*Node{t.mu.Lock();defer t.mu.Unlock();return t.index[name]}
func(t*Tree)FindByType(typ NodeType)[]*Node{t.mu.Lock();defer t.mu.Unlock();return t.root.FindAllType(typ)}
func(t*Tree)reindex(){t.index=map[string][]*Node{};if t.root==nil{return};t.root.Walk(func(n*Node,depth int)bool{t.index[n.Name]=append(t.index[n.Name],n);return true})}
func(t*Tree)FormatTree()string{t.mu.Lock();defer t.mu.Unlock();var sb strings.Builder;sb.WriteString("AST Tree:\n");sb.WriteString(strings.Repeat("─",40));sb.WriteString("\n\n")
  symbols:=make([]string,0,len(t.index));for k:=range t.index{symbols=append(symbols,k)};sort.Strings(symbols)
  for _,s:=range symbols{nodes:=t.index[s];fmt.Fprintf(&sb,"  %s: %d occurrence(s)\n",s,len(nodes))}
  return sb.String()}
func(t*Tree)CountNodes()int{t.mu.Lock();defer t.mu.Unlock();count:=0;t.root.Walk(func(n*Node,depth int)bool{count++;return true});return count}
