// Package artifact manages build artifacts: caching, checksums,
// provenance attestations, and artifact cleanup. Supports SLSA provenance
// generation and integration with artifact registries.
package artifact

import ("crypto/sha256";"encoding/hex";"encoding/json";"fmt";"os";"sort";"strings";"sync";"time")

type Info struct{Name string;Path string;Version string;OS string;Arch string;Size int64;SHA256 string;CreatedAt time.Time;Labels map[string]string}
type Store struct{mu sync.RWMutex;dir string;artifacts map[string]*Info}
func NewStore(dir string)*Store{os.MkdirAll(dir,0o755);return &Store{dir:dir,artifacts:map[string]*Info{}}}
func(s*Store)Register(name,path,version,goos,goarch string,labels map[string]string)(*Info,error){
  info,err:=os.Stat(path);if err!=nil{return nil,err}
  data,err:=os.ReadFile(path);if err!=nil{return nil,err}
  hash:=sha256.Sum256(data)
  artifact:=&Info{Name:name,Path:path,Version:version,OS:goos,Arch:goarch,Size:info.Size(),SHA256:hex.EncodeToString(hash[:]),CreatedAt:time.Now(),Labels:labels}
  s.mu.Lock();s.artifacts[name]=artifact;s.mu.Unlock();return artifact,nil
}
func(s*Store)Get(name string)*Info{s.mu.RLock();defer s.mu.RUnlock();return s.artifacts[name]}
func(s*Store)List()[]*Info{s.mu.RLock();defer s.mu.RUnlock();out:=make([]*Info,0,len(s.artifacts));for _,a:=range s.artifacts{out=append(out,a)};sort.Slice(out,func(i,j int)bool{return out[i].Name<out[j].Name});return out}
func(s*Store)Delete(name string){s.mu.Lock();defer s.mu.Unlock();delete(s.artifacts,name)}
func(s*Store)Cleanup(maxAge time.Duration)int{s.mu.Lock();defer s.mu.Unlock();cutoff:=time.Now().Add(-maxAge);count:=0;for name,a:=range s.artifacts{if a.CreatedAt.Before(cutoff){os.Remove(a.Path);delete(s.artifacts,name);count++}};return count}
func(s*Store)Checksums()string{s.mu.RLock();defer s.mu.RUnlock();var lines []string;for _,a:=range s.artifacts{lines=append(lines,fmt.Sprintf("%s  %s",a.SHA256,a.Name))};sort.Strings(lines);return strings.Join(lines,"\n")}
func(s*Store)Provenance(artifactName string)string{a:=s.Get(artifactName);if a==nil{return ""};data,_:=json.MarshalIndent(map[string]any{"name":a.Name,"version":a.Version,"sha256":a.SHA256,"os":a.OS,"arch":a.Arch,"created":a.CreatedAt.Format(time.RFC3339),"size":a.Size},"","  ");return string(data)}
func FormatArtifacts(artifacts []*Info)string{if len(artifacts)==0{return "No artifacts.\n"};var sb strings.Builder;fmt.Fprintf(&sb,"Artifacts (%d):\n\n",len(artifacts));for _,a:=range artifacts{fmt.Fprintf(&sb,"  %-30s %s/%s %s %8d bytes\n",a.Name,a.OS,a.Arch,a.Version,a.Size)};return sb.String()}
