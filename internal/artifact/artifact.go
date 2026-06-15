// Package artifact manages build artifacts: storage, versioning,
// checksumming, tagging, and lifecycle (promote/archive/delete).
package artifact

import ("crypto/sha256";"encoding/hex";"fmt";"sort";"strings";"sync";"time")

type Artifact struct{Name string;Version string;Path string;Checksum string;Size int64;MIMEType string;Tags []string;CreatedAt time.Time;PromotedAt time.Time;ArchivedAt time.Time}
func NewArtifact(name,version,path string,data []byte)*Artifact{
  h:=sha256.Sum256(data)
  return &Artifact{Name:name,Version:version,Path:path,Checksum:hex.EncodeToString(h[:]),Size:int64(len(data)),CreatedAt:time.Now()}
}
func(a*Artifact)AddTag(tag string){a.Tags=append(a.Tags,tag);sort.Strings(a.Tags)}
func(a*Artifact)HasTag(tag string)bool{for _,t:=range a.Tags{if t==tag{return true}};return false}
func(a*Artifact)Promote(){a.PromotedAt=time.Now()}
func(a*Artifact)Archive(){a.ArchivedAt=time.Now()}
func(a*Artifact)IsArchived()bool{return !a.ArchivedAt.IsZero()}

type Registry struct{mu sync.Mutex;artifacts map[string]*Artifact}
func NewRegistry()*Registry{return &Registry{artifacts:map[string]*Artifact{}}}
func(r*Registry)Register(a *Artifact)error{r.mu.Lock();defer r.mu.Unlock();key:=a.Name+":"+a.Version;r.artifacts[key]=a;return nil}
func(r*Registry)Get(name,version string)(*Artifact,bool){r.mu.Lock();defer r.mu.Unlock();a,ok:=r.artifacts[name+":"+version];return a,ok}
func(r*Registry)List()[]*Artifact{r.mu.Lock();defer r.mu.Unlock();var out []*Artifact;for _,a:=range r.artifacts{out=append(out,a)};sort.Slice(out,func(i,j int)bool{return out[i].CreatedAt.Before(out[j].CreatedAt)});return out}
func(r*Registry)FindByTag(tag string)[]*Artifact{r.mu.Lock();defer r.mu.Unlock();var out []*Artifact;for _,a:=range r.artifacts{if a.HasTag(tag){out=append(out,a)}};return out}
func(r*Registry)FormatArtifacts()string{r.mu.Lock();defer r.mu.Unlock();var sb strings.Builder
  fmt.Fprintf(&sb,"Artifact Registry (%d):\n%s\n\n",len(r.artifacts),strings.Repeat("─",60))
  for _,a:=range r.List(){archived:="";if a.IsArchived(){archived=" [ARCHIVED]"};fmt.Fprintf(&sb,"  %-40s v%-10s %s%s\n",a.Name,a.Version,byteCount(a.Size),archived)}
  return sb.String()}
func byteCount(n int64)string{if n<1024{return fmt.Sprintf("%dB",n)};if n<1024*1024{return fmt.Sprintf("%.1fKB",float64(n)/1024)};return fmt.Sprintf("%.1fMB",float64(n)/1024/1024)}
