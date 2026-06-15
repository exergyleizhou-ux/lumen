// Package graphdb provides a simple in-memory property graph with labeled
// nodes and directed edges. Supports graph traversal, shortest path, and
// cycle detection. Used for dependency graphs and call-graph analysis.
package graphdb

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Node struct{ID string;Label string;Properties map[string]any}
type Edge struct{ID string;From string;To string;Label string;Properties map[string]any}
type Graph struct{mu sync.RWMutex;nodes map[string]*Node;edges map[string]*Edge;fromIndex map[string][]string;toIndex map[string][]string}
func NewGraph()*Graph{return &Graph{nodes:map[string]*Node{},edges:map[string]*Edge{},fromIndex:map[string][]string{},toIndex:map[string][]string{}}}
func(g*Graph)AddNode(id,label string,props map[string]any)*Node{
  g.mu.Lock();defer g.mu.Unlock();n:=&Node{ID:id,Label:label,Properties:props};g.nodes[id]=n;return n
}
func(g*Graph)AddEdge(id,from,to,label string,props map[string]any)*Edge{
  g.mu.Lock();defer g.mu.Unlock();e:=&Edge{ID:id,From:from,To:to,Label:label,Properties:props};g.edges[id]=e;g.fromIndex[from]=append(g.fromIndex[from],id);g.toIndex[to]=append(g.toIndex[to],id);return e
}
func(g*Graph)GetNode(id string)*Node{g.mu.RLock();defer g.mu.RUnlock();return g.nodes[id]}
func(g*Graph)GetEdge(id string)*Edge{g.mu.RLock();defer g.mu.RUnlock();return g.edges[id]}
func(g*Graph)NodeCount()int{g.mu.RLock();defer g.mu.RUnlock();return len(g.nodes)}
func(g*Graph)EdgeCount()int{g.mu.RLock();defer g.mu.RUnlock();return len(g.edges)}
func(g*Graph)Outgoing(nodeID string)[]*Edge{
  g.mu.RLock();defer g.mu.RUnlock();var out []*Edge
  for _,eid:=range g.fromIndex[nodeID]{if e,ok:=g.edges[eid];ok{out=append(out,e)}}
  return out
}
func(g*Graph)Incoming(nodeID string)[]*Edge{
  g.mu.RLock();defer g.mu.RUnlock();var out []*Edge
  for _,eid:=range g.toIndex[nodeID]{if e,ok:=g.edges[eid];ok{out=append(out,e)}}
  return out
}
func(g*Graph)Neighbors(nodeID string)[]*Node{
  g.mu.RLock();defer g.mu.RUnlock();seen:=map[string]bool{};var out []*Node
  for _,eid:=range g.fromIndex[nodeID]{if e,ok:=g.edges[eid];ok{if n,ok2:=g.nodes[e.To];ok2&&!seen[n.ID]{seen[n.ID]=true;out=append(out,n)}}}
  for _,eid:=range g.toIndex[nodeID]{if e,ok:=g.edges[eid];ok{if n,ok2:=g.nodes[e.From];ok2&&!seen[n.ID]{seen[n.ID]=true;out=append(out,n)}}}
  return out
}
func(g*Graph)ShortestPath(from,to string)[]string{
  g.mu.RLock();defer g.mu.RUnlock()
  if _,ok:=g.nodes[from];!ok{return nil}
  if _,ok:=g.nodes[to];!ok{return nil}
  visited:=map[string]bool{from:true};prev:=map[string]string{};queue:=[]string{from}
  for len(queue)>0{current:=queue[0];queue=queue[1:];if current==to{break}
    for _,eid:=range g.fromIndex[current]{if e,ok:=g.edges[eid];ok{if !visited[e.To]{visited[e.To]=true;prev[e.To]=current;queue=append(queue,e.To)}}}
  }
  if !visited[to]{return nil}
  var path []string;for at:=to;at!=from;at=prev[at]{path=append([]string{at},path...)}
  path=append([]string{from},path...);return path
}
func(g*Graph)HasCycle()bool{g.mu.RLock();defer g.mu.RUnlock();white:=map[string]bool{};gray:=map[string]bool{};black:=map[string]bool{};for id:=range g.nodes{white[id]=true}
  var visit func(string)bool;visit=func(n string)bool{
    delete(white,n);gray[n]=true
    for _,eid:=range g.fromIndex[n]{if e,ok:=g.edges[eid];ok{if gray[e.To]{return true};if white[e.To]&&visit(e.To){return true}}}
    delete(gray,n);black[n]=true;return false
  }
  for len(white)>0{for n:=range white{if visit(n){return true};break}};return false
}
func(g*Graph)SubGraph(nodeIDs []string)*Graph{
  g.mu.RLock();defer g.mu.RUnlock();sg:=NewGraph()
  for _,id:=range nodeIDs{if n,ok:=g.nodes[id];ok{sg.nodes[id]=n}}
  for _,e:=range g.edges{if _,ok1:=sg.nodes[e.From];ok1{if _,ok2:=sg.nodes[e.To];ok2{sg.edges[e.ID]=e;sg.fromIndex[e.From]=append(sg.fromIndex[e.From],e.ID);sg.toIndex[e.To]=append(sg.toIndex[e.To],e.ID)}}}
  return sg
}
func(g*Graph)FormatStats()string{g.mu.RLock();defer g.mu.RUnlock()
  var sb strings.Builder
  fmt.Fprintf(&sb,"Graph: %d nodes, %d edges\n\n",len(g.nodes),len(g.edges))
  type nc struct{id string;deg int}
  var degrees []nc
  for id:=range g.nodes{deg:=len(g.fromIndex[id])+len(g.toIndex[id]);degrees=append(degrees,nc{id,deg})}
  sort.Slice(degrees,func(i,j int)bool{return degrees[i].deg>degrees[j].deg})
  sb.WriteString("Top nodes by degree:\n")
  for i,d:=range degrees{if i>=10{break};n:=g.nodes[d.id];fmt.Fprintf(&sb,"  %s (%s): %d\n",d.id,n.Label,d.deg)}
  return sb.String()
}
