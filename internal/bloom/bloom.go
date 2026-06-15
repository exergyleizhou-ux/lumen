// Package bloom provides a space-efficient probabilistic set for
// membership testing with configurable false positive rate. Used for
// deduplication and fast "probably seen" checks in agent pipelines.
package bloom

import ("encoding/binary";"fmt";"hash/fnv";"math";"sync")

type Filter struct{bits []uint64;m uint64;k uint;mu sync.RWMutex}
func New(size uint64,hashCount uint)*Filter{if size<1{size=1024};if hashCount<1{hashCount=4};return &Filter{bits:make([]uint64,(size+63)/64),m:size,k:hashCount}}
func NewOptimal(expectedItems uint,fpr float64)*Filter{
  if fpr<=0{fpr=0.01};if fpr>=1{fpr=0.5}
  m:=uint64(math.Ceil(-float64(expectedItems)*math.Log(fpr)/math.Pow(math.Log(2),2)))
  k:=uint(math.Ceil(math.Log(2)*float64(m)/float64(expectedItems)))
  if k<1{k=1};if m<1{m=1};return New(m,k)
}
func(f*Filter)Add(data []byte){f.mu.Lock();defer f.mu.Unlock();h1,h2:=f.hash(data);for i:=uint(0);i<f.k;i++{idx:=(h1+uint64(i)*h2)%f.m;f.bits[idx/64]|=1<<(idx%64)}}
func(f*Filter)Contains(data []byte)bool{f.mu.RLock();defer f.mu.RUnlock();h1,h2:=f.hash(data);for i:=uint(0);i<f.k;i++{idx:=(h1+uint64(i)*h2)%f.m;if f.bits[idx/64]&(1<<(idx%64))==0{return false}};return true}
func(f*Filter)hash(data []byte)(uint64,uint64){h1:=fnv.New64a();h1.Write(data);s1:=h1.Sum64();h2:=fnv.New64();h2.Write(data);s2:=h2.Sum64();return s1,s2}
func(f*Filter)EstimatedItems()float64{f.mu.RLock();defer f.mu.RUnlock();ones:=0;for _,w:=range f.bits{ones+=popcount64(w)};p:=float64(ones)/float64(f.m);if p>=1{return float64(f.m)};return -float64(f.m)/float64(f.k)*math.Log(1-p)}
func popcount64(x uint64)int{var c int;for x!=0{x&=x-1;c++};return c}
func(f*Filter)Format()string{f.mu.RLock();defer f.mu.RUnlock();ones:=0;for _,w:=range f.bits{ones+=popcount64(w)};pct:=float64(ones)/float64(f.m)*100;return fmt.Sprintf("Bloom Filter: %d bits, %d hashes, %.1f%% filled, ~%.0f items",f.m,f.k,pct,f.EstimatedItems())}
func(f*Filter)Merge(other *Filter){f.mu.Lock();defer f.mu.Unlock();other.mu.RLock();defer other.mu.RUnlock();for i:=range f.bits{if i<len(other.bits){f.bits[i]|=other.bits[i]}}}

