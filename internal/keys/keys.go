// Package keys manages cryptographic keys: generation, rotation,
// storage (in-memory), and access control for agent authentication.
package keys

import ("crypto/ed25519";"crypto/rand";"crypto/sha256";"encoding/hex";"fmt";"sort";"strings";"sync";"time")

type Key struct{ID string;Label string;Purpose string;PublicKey ed25519.PublicKey;CreatedAt time.Time;RotatedAt time.Time;ExpiresAt time.Time}
type Manager struct{mu sync.Mutex;keys map[string]*Key;rotationInterval time.Duration}
func NewManager()*Manager{return &Manager{keys:map[string]*Key{},rotationInterval:90*24*time.Hour}}
func(m*Manager)Generate(label,purpose string)(*Key,ed25519.PrivateKey,error){
  pub,priv,err:=ed25519.GenerateKey(rand.Reader);if err!=nil{return nil,nil,err}
  h:=sha256.Sum256(pub);id:=hex.EncodeToString(h[:12])
  k:=&Key{ID:id,Label:label,Purpose:purpose,PublicKey:pub,CreatedAt:time.Now(),ExpiresAt:time.Now().Add(m.rotationInterval)}
  m.mu.Lock();m.keys[id]=k;m.mu.Unlock();return k,priv,nil
}
func(m*Manager)Get(id string)(*Key,bool){m.mu.Lock();defer m.mu.Unlock();k,ok:=m.keys[id];return k,ok}
func(m*Manager)Rotate(id string)(*Key,ed25519.PrivateKey,error){
  m.mu.Lock();old,ok:=m.keys[id];m.mu.Unlock()
  if!ok{return nil,nil,fmt.Errorf("key %q not found",id)}
  newKey,priv,err:=m.Generate(old.Label,old.Purpose)
  if err!=nil{return nil,nil,err}
  m.mu.Lock();old.RotatedAt=time.Now();m.mu.Unlock()
  return newKey,priv,nil
}
func(m*Manager)Expired()[]*Key{
  m.mu.Lock();defer m.mu.Unlock();now:=time.Now();var out []*Key
  for _,k:=range m.keys{if now.After(k.ExpiresAt){out=append(out,k)}};sort.Slice(out,func(i,j int)bool{return out[i].CreatedAt.Before(out[j].CreatedAt)});return out
}
func(m*Manager)List()[]*Key{m.mu.Lock();defer m.mu.Unlock();var out []*Key;for _,k:=range m.keys{out=append(out,k)};sort.Slice(out,func(i,j int)bool{return out[i].CreatedAt.Before(out[j].CreatedAt)});return out}
func(m*Manager)FormatKeys()string{m.mu.Lock();defer m.mu.Unlock();var sb strings.Builder
  fmt.Fprintf(&sb,"Key Manager (%d keys):\n%s\n\n",len(m.keys),strings.Repeat("─",60))
  for _,k:=range m.List(){expired:="";if time.Now().After(k.ExpiresAt){expired=" [EXPIRED]"}
    fmt.Fprintf(&sb,"  %s [%s] %s%s created=%s\n",k.ID[:12],k.Purpose,k.Label,expired,k.CreatedAt.Format("2006-01-02"))}
  return sb.String()}
