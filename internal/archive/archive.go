// Package archive provides compression and archival for agent outputs:
// gzip/bzip2 streaming compression, tar wrapper, zip archive building,
// and archive metadata extraction.
package archive

import ("archive/tar";"archive/zip";"bytes";"compress/gzip";"fmt";"io";"os";"sort";"strings";"sync";"time")

type Entry struct{Name string;Size int64;Mode os.FileMode;ModTime time.Time;IsDir bool;Data []byte}
type Archiver struct{mu sync.Mutex}
func NewArchiver()*Archiver{return &Archiver{}}

func(a*Archiver)CreateTarGz(entries []Entry)([]byte,error){
  var buf bytes.Buffer;gw:=gzip.NewWriter(&buf);tw:=tar.NewWriter(gw)
  for _,e:=range entries{
    hdr:=&tar.Header{Name:e.Name,Size:e.Size,Mode:int64(e.Mode),ModTime:e.ModTime}
    if e.IsDir{hdr.Typeflag=tar.TypeDir;tw.WriteHeader(hdr)}else{hdr.Typeflag=tar.TypeReg;tw.WriteHeader(hdr);tw.Write(e.Data)}
  }
  tw.Close();gw.Close();return buf.Bytes(),nil
}

func(a*Archiver)CreateZip(entries []Entry)([]byte,error){
  var buf bytes.Buffer;zw:=zip.NewWriter(&buf)
  for _,e:=range entries{
    if e.IsDir{zw.Create(e.Name+"/")}else{w,err:=zw.Create(e.Name);if err!=nil{return nil,err};w.Write(e.Data)}
  }
  zw.Close();return buf.Bytes(),nil
}

func(a*Archiver)ExtractTarGz(data []byte,destDir string)([]Entry,error){
  gr,err:=gzip.NewReader(bytes.NewReader(data));if err!=nil{return nil,err};defer gr.Close()
  tr:=tar.NewReader(gr);var entries []Entry
  for{hdr,err:=tr.Next();if err==io.EOF{break}else if err!=nil{return nil,err}
    var content []byte
    if hdr.Typeflag==tar.TypeReg{content,_=io.ReadAll(tr)}
    entries=append(entries,Entry{Name:hdr.Name,Size:hdr.Size,Mode:os.FileMode(hdr.Mode),ModTime:hdr.ModTime,IsDir:hdr.Typeflag==tar.TypeDir,Data:content})
  }
  return entries,nil
}

func(a*Archiver)ListArchive(data []byte)string{
  var sb strings.Builder
  if len(data)>2&&data[0]==0x1f&&data[1]==0x8b{
    // gzip
    gr,_:=gzip.NewReader(bytes.NewReader(data));defer gr.Close();tr:=tar.NewReader(gr);fmt.Fprintf(&sb,"Archive (tar.gz):\n")
    for{hdr,err:=tr.Next();if err!=nil{break};icon:="📄";if hdr.Typeflag==tar.TypeDir{icon="📁"};fmt.Fprintf(&sb,"  %s %-40s %s\n",icon,hdr.Name,byteCount(hdr.Size))}
  }else if len(data)>2&&data[0]=='P'&&data[1]=='K'{
    fmt.Fprintf(&sb,"Archive (zip):\n")
    zr,_:=zip.NewReader(bytes.NewReader(data),int64(len(data)));for _,f:=range zr.File{icon:="📄";if f.FileInfo().IsDir(){icon="📁"};fmt.Fprintf(&sb,"  %s %-40s %s\n",icon,f.Name,byteCount(f.FileInfo().Size()))}
  }
  return sb.String()
}
func byteCount(n int64)string{if n<1024{return fmt.Sprintf("%dB",n)};if n<1024*1024{return fmt.Sprintf("%.1fKB",float64(n)/1024)};return fmt.Sprintf("%.1fMB",float64(n)/1024/1024)}

type Snapshot struct{mu sync.Mutex;entries []Entry;baseDir string}
func NewSnapshot(baseDir string)*Snapshot{return &Snapshot{baseDir:baseDir}}
func(s*Snapshot)AddFile(path string,data []byte){s.mu.Lock();defer s.mu.Unlock();s.entries=append(s.entries,Entry{Name:path,Size:int64(len(data)),Data:data,ModTime:time.Now()})}
func(s*Snapshot)AddDir(path string){s.mu.Lock();defer s.mu.Unlock();s.entries=append(s.entries,Entry{Name:path,IsDir:true,ModTime:time.Now()})}
func(s*Snapshot)ToArchive()([]byte,error){a:=NewArchiver();return a.CreateTarGz(s.entries)}
func(s*Snapshot)Count()int{s.mu.Lock();defer s.mu.Unlock();return len(s.entries)}
func(s*Snapshot)Format()string{s.mu.Lock();defer s.mu.Unlock();var sb strings.Builder;fmt.Fprintf(&sb,"Snapshot (%d entries):\n%s\n\n",len(s.entries),strings.Repeat("─",40));sort.Slice(s.entries,func(i,j int)bool{return s.entries[i].Name<s.entries[j].Name})
  for _,e:=range s.entries{icon:="📄";if e.IsDir{icon="📁"};fmt.Fprintf(&sb,"  %s %-40s %s\n",icon,e.Name,byteCount(e.Size))};return sb.String()}
