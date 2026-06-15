// Package repl provides an interactive REPL for testing agent tool calls,
// prompt composition, and workflow debugging. Supports history, tab
// completion, and session save/restore.
package repl

import ("bufio";"fmt";"io";"os";"sort";"strings";"sync";"time")

// Session is a REPL session.
type Session struct{mu sync.Mutex;history []Command;input *bufio.Reader;output io.Writer;running bool;handlers map[string]func([]string)string}
type Command struct{Line string;Timestamp time.Time;Result string}
func NewSession()*Session{return &Session{input:bufio.NewReader(os.Stdin),output:os.Stdout,handlers:map[string]func([]string)string{}}}
func(s*Session)Register(name string,fn func([]string)string){s.mu.Lock();defer s.mu.Unlock();s.handlers[name]=fn}
func(s*Session)Handle(cmd string)string{
  parts:=strings.Fields(cmd)
  if len(parts)==0{return ""}
  name:=parts[0];args:=parts[1:]
  s.mu.Lock();fn,ok:=s.handlers[name];s.mu.Unlock()
  var result string
  if ok{result=fn(args)}else{result=fmt.Sprintf("unknown command: %s (try: help)",name)}
  s.mu.Lock();s.history=append(s.history,Command{Line:cmd,Timestamp:time.Now(),Result:result});s.mu.Unlock()
  return result
}
func(s*Session)History()[]Command{s.mu.Lock();defer s.mu.Unlock();out:=make([]Command,len(s.history));copy(out,s.history);return out}
func(s*Session)Help()string{
  s.mu.Lock();defer s.mu.Unlock()
  var sb strings.Builder
  sb.WriteString("Available commands:\n")
  keys:=make([]string,0,len(s.handlers))
  for k:=range s.handlers{keys=append(keys,k)}
  sort.Strings(keys)
  for _,k:=range keys{sb.WriteString(fmt.Sprintf("  %s\n",k))}
  return sb.String()
}
func(s*Session)Run(){
  s.running=true
  fmt.Fprintf(s.output,"REPL started. Type 'help' for commands, 'exit' to quit.\n")
  for s.running{
    fmt.Fprintf(s.output,"> ")
    line,err:=s.input.ReadString('\n')
    if err!=nil{break}
    line=strings.TrimSpace(line)
    if line=="exit"||line=="quit"{break}
    if line=="help"{fmt.Fprintln(s.output,s.Help());continue}
    result:=s.Handle(line)
    fmt.Fprintln(s.output,result)
  }
}
func(s*Session)Stop(){s.running=false}

func(s*Session)FormatHistory()string{
  hist:=s.History()
  if len(hist)==0{return "No history.\n"}
  var sb strings.Builder
  fmt.Fprintf(&sb,"Command History (%d):\n%s\n",len(hist),strings.Repeat("─",40))
  for i,h:=range hist{if i>=50{fmt.Fprintf(&sb,"  ... %d more\n",len(hist)-50);break}
    fmt.Fprintf(&sb,"  %s %s\n",h.Timestamp.Format("15:04:05"),h.Line)
    if h.Result!=""{fmt.Fprintf(&sb,"    → %s\n",h.Result)}
  }
  return sb.String()
}
