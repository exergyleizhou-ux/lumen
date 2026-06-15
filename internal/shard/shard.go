// Package shard provides consistent hashing-based data sharding with
// virtual nodes, replica management, and rebalancing for distributed
// agent storage and caching.
package shard

import ("crypto/md5";"encoding/binary";"fmt";"sort";"strings";"sync")

type Node struct{ID string;Address string;Weight int;Healthy bool}
type Ring struct{mu sync.RWMutex;nodes map[string]*Node;virtualNodes int;ring []ringEntry}
type ringEntry struct{hash uint64;nodeID string}
func NewRing(virtualNodes int)*Ring{if virtualNodes<1{virtualNodes=128};return &Ring{nodes:map[string]*Node{},virtualNodes:virtualNodes}}
func(r*Ring)AddNode(node *Node){r.mu.Lock();defer r.mu.Unlock();r.nodes[node.ID]=node;r.rebuild()}
func(r*Ring)RemoveNode(id string){r.mu.Lock();defer r.mu.Unlock();delete(r.nodes,id);r.rebuild()}
func(r*Ring)MarkHealthy(id string,bool healthy){r.mu.Lock();defer r.mu.Unlock();if n,ok:=r.nodes[id];ok{n.Healthy=healthy};r.rebuild()}
func(r*Ring)GetNode(key string)*Node{r.mu.RLock();defer r.mu.RUnlock();if len(r.ring)==0{return nil};h:=hashKey(key);idx:=sort.Search(len(r.ring),func(i int)bool{return r.ring[i].hash>=h});if idx>=len(r.ring){idx=0};return r.nodes[r.ring[idx].nodeID]}
func(r*Ring)GetReplicas(key string,count int)[]*Node{r.mu.RLock();defer r.mu.RUnlock();if len(r.ring)==0||count<1{return nil}
  h:=hashKey(key);idx:=sort.Search(len(r.ring),func(i int)bool{return r.ring[i].hash>=h})
  var result []*Node;seen:=map[string]bool{}
  for i:=0;i<len(r.ring)&&len(result)<count;i++{entryIdx:=(idx+i)%len(r.ring);nodeID:=r.ring[entryIdx].nodeID;if!seen[nodeID]{seen[nodeID]=true;result=append(result,r.nodes[nodeID])}}
  return result
}
func(r*Ring)rebuild(){
  r.ring=nil
  for id,n:=range r.nodes{if!n.Healthy{continue};for v:=0;v<r.virtualNodes*n.Weight;v++{h:=hashKey(fmt.Sprintf("%s-v%d",id,v));r.ring=append(r.ring,ringEntry{hash:h,nodeID:id})}}
  sort.Slice(r.ring,func(i,j int)bool{return r.ring[i].hash<r.ring[j].hash})
}
func hashKey(key string)uint64{h:=md5.Sum([]byte(key));return binary.BigEndian.Uint64(h[:8])}
func(r*Ring)NodeCount()int{r.mu.RLock();defer r.mu.RUnlock();return len(r.nodes)}
func(r*Ring)RingSize()int{r.mu.RLock();defer r.mu.RUnlock();return len(r.ring)}

type KeyDistribution struct{mu sync.Mutex;counts map[string]int}
func NewKeyDistribution()*KeyDistribution{return &KeyDistribution{counts:map[string]int{}}}
func(kd*KeyDistribution)Record(nodeID string){kd.mu.Lock();defer kd.mu.Unlock();kd.counts[nodeID]++}
func(kd*KeyDistribution)Stats()map[string]int{kd.mu.Lock();defer kd.mu.Unlock();out:=make(map[string]int);for k,v:=range kd.counts{out[k]=v};return out}
func(kd*KeyDistribution)Imbalance()float64{kd.mu.Lock();defer kd.mu.Unlock();if len(kd.counts)==0{return 0};var sum,min,max int;min=1<<30
  for _,c:=range kd.counts{sum+=c;if c<min{min=c};if c>max{max=c}}
  if min==0||sum==0{return 0};avg:=float64(sum)/float64(len(kd.counts));return(float64(max)-float64(min))/avg
}

func(r*Ring)FormatRing()string{r.mu.RLock();defer r.mu.RUnlock();var sb strings.Builder;fmt.Fprintf(&sb,"Consistent Hash Ring: %d nodes, %d vnodes\n%s\n\n",len(r.nodes),len(r.ring),strings.Repeat("─",50))
  ids:=make([]string,0,len(r.nodes));for id:=range r.nodes{ids=append(ids,id)};sort.Strings(ids)
  for _,id:=range ids{n:=r.nodes[id];icon:="✅";if!n.Healthy{icon="🔴"};vnodes:=0;for _,e:=range r.ring{if e.nodeID==id{vnodes++}};fmt.Fprintf(&sb,"  %s %-20s %-30s weight=%d vnodes=%d\n",icon,id,n.Address,n.Weight,vnodes)}
  return sb.String()}
