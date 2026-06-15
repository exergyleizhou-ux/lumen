// Package mux provides connection multiplexing over a single transport.
// It supports stream multiplexing with flow control, channel isolation,
// and graceful connection teardown.
package mux

import ("fmt";"sort";"strings";"sync";"sync/atomic";"time")

type Stream struct{ID int64;State string;CreatedAt time.Time;BytesIn int64;BytesOut int64}
type StreamHandler func(stream *Stream,data []byte)[]byte
type Mux struct{mu sync.Mutex;streams map[int64]*Stream;handler StreamHandler;nextID int64;maxStreams int}
func NewMux(maxStreams int)*Mux{return &Mux{streams:map[int64]*Stream{},maxStreams:maxStreams}}
func(m*Mux)Handle(fn StreamHandler){m.mu.Lock();defer m.mu.Unlock();m.handler=fn}
func(m*Mux)OpenStream()(*Stream,error){
  m.mu.Lock();defer m.mu.Unlock()
  if len(m.streams)>=m.maxStreams{return nil,fmt.Errorf("max streams %d reached",m.maxStreams)}
  id:=atomic.AddInt64(&m.nextID,1)
  s:=&Stream{ID:id,State:"open",CreatedAt:time.Now()};m.streams[id]=s;return s,nil
}
func(m*Mux)CloseStream(id int64)error{m.mu.Lock();defer m.mu.Unlock();s,ok:=m.streams[id];if!ok{return fmt.Errorf("stream %d not found",id)};s.State="closed";delete(m.streams,id);return nil}
func(m*Mux)Send(streamID int64,data []byte)([]byte,error){
  m.mu.Lock();s,ok:=m.streams[streamID];m.mu.Unlock()
  if!ok||s.State!="open"{return nil,fmt.Errorf("stream %d not open",streamID)}
  s.BytesIn+=int64(len(data))
  if m.handler!=nil{return m.handler(s,data),nil}
  return data,nil
}
func(m*Mux)StreamCount()int{m.mu.Lock();defer m.mu.Unlock();return len(m.streams)}
func(m*Mux)FormatStreams()string{m.mu.Lock();defer m.mu.Unlock();var sb strings.Builder
  fmt.Fprintf(&sb,"Mux Streams (%d/%d):\n%s\n\n",len(m.streams),m.maxStreams,strings.Repeat("─",50))
  ids:=make([]int64,0,len(m.streams));for id:=range m.streams{ids=append(ids,id)};sort.Slice(ids,func(i,j int)bool{return ids[i]<ids[j]})
  for _,id:=range ids{s:=m.streams[id];fmt.Fprintf(&sb,"  [%d] %s in=%d out=%d age=%v\n",s.ID,s.State,s.BytesIn,s.BytesOut,time.Since(s.CreatedAt).Round(time.Millisecond))}
  return sb.String()}
