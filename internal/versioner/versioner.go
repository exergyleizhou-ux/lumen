// Package versioner provides semantic version parsing, comparison,
// constraint matching, and version bumping for agent releases.
package versioner

import ("fmt";"sort";"strconv";"strings")

type Version struct{Major,Minor,Patch int;PreRelease string;Build string}
func Parse(v string)(*Version,error){
  parts:=strings.SplitN(v,"-",2);core:=parts[0];pre:=""
  if len(parts)>1{pre=parts[1]}
  build:=""
  if idx:=strings.Index(pre,"+");idx>=0{build=pre[idx+1:];pre=pre[:idx]}
  nums:=strings.Split(core,".")
  if len(nums)!=3{return nil,fmt.Errorf("invalid semver: %s",v)}
  m,_:=strconv.Atoi(nums[0]);mi,_:=strconv.Atoi(nums[1]);p,_:=strconv.Atoi(nums[2])
  return &Version{Major:m,Minor:mi,Patch:p,PreRelease:pre,Build:build},nil
}
func(v*Version)String()string{s:=fmt.Sprintf("%d.%d.%d",v.Major,v.Minor,v.Patch);if v.PreRelease!=""{s+="-"+v.PreRelease};if v.Build!=""{s+="+"+v.Build};return s}
func(v*Version)Compare(other*Version)int{
  if v.Major!=other.Major{if v.Major>other.Major{return 1};return -1}
  if v.Minor!=other.Minor{if v.Minor>other.Minor{return 1};return -1}
  if v.Patch!=other.Patch{if v.Patch>other.Patch{return 1};return -1}
  if v.PreRelease==""&&other.PreRelease!=""{return 1}
  if v.PreRelease!=""&&other.PreRelease==""{return -1}
  if v.PreRelease<other.PreRelease{return -1}else if v.PreRelease>other.PreRelease{return 1}
  return 0
}
func(v*Version)BumpMajor()*Version{return &Version{Major:v.Major+1,Minor:0,Patch:0}}
func(v*Version)BumpMinor()*Version{return &Version{Major:v.Major,Minor:v.Minor+1,Patch:0}}
func(v*Version)BumpPatch()*Version{return &Version{Major:v.Major,Minor:v.Minor,Patch:v.Patch+1}}
func(v*Version)IsPrerelease()bool{return v.PreRelease!=""}

type Constraint struct{Op string;Version *Version}
func ParseConstraint(s string)(*Constraint,error){
  if strings.HasPrefix(s,">="){ver,_:=Parse(s[2:]);return &Constraint{Op:">=",Version:ver},nil}
  if strings.HasPrefix(s,"<="){ver,_:=Parse(s[2:]);return &Constraint{Op:"<=",Version:ver},nil}
  if strings.HasPrefix(s,">"){ver,_:=Parse(s[1:]);return &Constraint{Op:">",Version:ver},nil}
  if strings.HasPrefix(s,"<"){ver,_:=Parse(s[1:]);return &Constraint{Op:"<",Version:ver},nil}
  if strings.HasPrefix(s,"^"){ver,_:=Parse(s[1:]);return &Constraint{Op:"^",Version:ver},nil}
  if strings.HasPrefix(s,"~"){ver,_:=Parse(s[1:]);return &Constraint{Op:"~",Version:ver},nil}
  ver,_:=Parse(s);return &Constraint{Op:"=",Version:ver},nil
}
func(c*Constraint)Satisfies(v *Version)bool{
  switch c.Op{
  case "=":return v.Compare(c.Version)==0
  case ">":return v.Compare(c.Version)>0
  case "<":return v.Compare(c.Version)<0
  case ">=":return v.Compare(c.Version)>=0
  case "<=":return v.Compare(c.Version)<=0
  case "^":return v.Major==c.Version.Major&&v.Compare(c.Version)>=0
  case "~":return v.Major==c.Version.Major&&v.Minor==c.Version.Minor&&v.Compare(c.Version)>=0
  default:return false
  }
}

type Repository struct{versions map[string]*Version}
func NewRepository()*Repository{return &Repository{versions:map[string]*Version{}}}
func(r*Repository)Register(v *Version){r.versions[v.String()]=v}
func(r*Repository)Latest()*Version{var best *Version;for _,v:=range r.versions{if best==nil||v.Compare(best)>0{best=v}};return best}
func(r*Repository)Sorted()[]*Version{var out []*Version;for _,v:=range r.versions{out=append(out,v)};sort.Slice(out,func(i,j int)bool{return out[i].Compare(out[j])<0});return out}
func(r*Repository)String()string{var sb strings.Builder;fmt.Fprintf(&sb,"Version Repository:\n");for _,v:=range r.Sorted(){fmt.Fprintf(&sb,"  %s\n",v.String())};return sb.String()}
