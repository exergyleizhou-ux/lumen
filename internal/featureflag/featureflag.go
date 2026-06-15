// Package featureflag provides runtime feature toggles with percentage-based
// rollouts, user targeting, and kill-switch support. Used for canary
// deployments of new agent features and A/B testing prompt strategies.
package featureflag

import ("crypto/sha256";"encoding/binary";"fmt";"sort";"strings";"sync";"time")
type Flag struct{Key string;Enabled bool;Rollout int;Rules []Rule;UpdatedAt time.Time}
type Rule struct{Field string;Op string;Value string}
type Store struct{mu sync.RWMutex;flags map[string]*Flag}
func NewStore()*Store{return &Store{flags:map[string]*Flag{}}}
func(s*Store)Set(key string,enabled bool,rollout int)*Flag{s.mu.Lock();defer s.mu.Unlock();f:=&Flag{Key:key,Enabled:enabled,Rollout:rollout,UpdatedAt:time.Now()};s.flags[key]=f;return f}
func(s*Store)AddRule(key,field,op,value string){s.mu.Lock();defer s.mu.Unlock();if f,ok:=s.flags[key];ok{f.Rules=append(f.Rules,Rule{Field:field,Op:op,Value:value})}}
func(s*Store)IsEnabled(key string,ctx map[string]string)bool{s.mu.RLock();f,ok:=s.flags[key];s.mu.RUnlock();if!ok||!f.Enabled{return false}
  if f.Rollout<100{hash:=sha256.Sum256([]byte(key));bucket:=int(binary.BigEndian.Uint64(hash[:8])%100);if bucket>=f.Rollout{return false}}
  for _,rule:=range f.Rules{if ctx!=nil{v,ok:=ctx[rule.Field];if!ok{return false}
    switch rule.Op{case "eq":if v!=rule.Value{return false}
    case "neq":if v==rule.Value{return false}
    case "contains":if!strings.Contains(v,rule.Value){return false}}}}
  return true
}
func(s*Store)List()[]*Flag{s.mu.RLock();defer s.mu.RUnlock();out:=make([]*Flag,0,len(s.flags));for _,f:=range s.flags{out=append(out,f)};sort.Slice(out,func(i,j int)bool{return out[i].Key<out[j].Key});return out}
func(s*Store)Delete(key string){s.mu.Lock();defer s.mu.Unlock();delete(s.flags,key)}
func(s*Store)FormatFlags()string{flags:=s.List();if len(flags)==0{return "No feature flags.\n"};var sb strings.Builder;fmt.Fprintf(&sb,"Feature Flags (%d):\n\n",len(flags));for _,f:=range flags{icon:="○";if f.Enabled{icon="●"};fmt.Fprintf(&sb,"%s %-25s rollout:%-3d%% rules:%d\n",icon,f.Key,f.Rollout,len(f.Rules))};return sb.String()}
