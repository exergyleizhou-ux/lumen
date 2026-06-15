// Package dispatcher provides a task dispatcher with worker pools,
// priority queues, and load balancing for distributing agent work
// across multiple execution contexts.
package dispatcher

import ("container/heap";"context";"fmt";"sort";"strings";"sync";"time")

type Priority int
const (PriorityLow Priority=0;PriorityNormal Priority=5;PriorityHigh Priority=10;PriorityCritical Priority=20)

type Task struct{ID string;Name string;Priority Priority;Payload any;CreatedAt time.Time;Deadline time.Time;Retries int;MaxRetries int}
type TaskResult struct{TaskID string;Success bool;Output any;Error string;Duration time.Duration;CompletedAt time.Time}

type Worker interface{ID()string;Process(ctx context.Context,task *Task)*TaskResult}
type taskHeap []*Task
func(h taskHeap)Len()int{return len(h)}
func(h taskHeap)Less(i,j int)bool{return h[i].Priority>h[j].Priority}
func(h taskHeap)Swap(i,j int){h[i],h[j]=h[j],h[i]}
func(h*taskHeap)Push(x any){*h=append(*h,x.(*Task))}
func(h*taskHeap)Pop()any{old:=*h;n:=len(old);item:=old[n-1];*h=old[:n-1];return item}

type Dispatcher struct{mu sync.Mutex;queue taskHeap;workers map[string]Worker;busyWorkers map[string]bool;results []TaskResult;maxResults int;nextID int64;stopCh chan struct{}}
func NewDispatcher()*Dispatcher{return &Dispatcher{workers:map[string]Worker{},busyWorkers:map[string]bool{},maxResults:1000,stopCh:make(chan struct{})}}
func(d*Dispatcher)RegisterWorker(w Worker){d.mu.Lock();defer d.mu.Unlock();d.workers[w.ID()]=w}
func(d*Dispatcher)Enqueue(task *Task){d.mu.Lock();defer d.mu.Unlock();heap.Push(&d.queue,task)}
func(d*Dispatcher)Start(ctx context.Context){
  go func(){ticker:=time.NewTicker(100*time.Millisecond);defer ticker.Stop()
    for{select{case<-d.stopCh:return;case<-ctx.Done():return;case<-ticker.C:}d.dispatchRound(ctx)}
  }()
}
func(d*Dispatcher)Stop(){close(d.stopCh)}
func(d*Dispatcher)dispatchRound(ctx context.Context){
  d.mu.Lock()
  if d.queue.Len()==0{d.mu.Unlock();return}
  // Find available worker
  var avail Worker
  for _,w:=range d.workers{if!d.busyWorkers[w.ID()]{avail=w;break}}
  if avail==nil{d.mu.Unlock();return}
  task:=heap.Pop(&d.queue).(*Task)
  d.busyWorkers[avail.ID()]=true
  d.mu.Unlock()

  go func(w Worker,t *Task){
    defer func(){d.mu.Lock();delete(d.busyWorkers,w.ID());d.mu.Unlock()}()
    result:=w.Process(ctx,t)
    d.mu.Lock();d.results=append(d.results,*result);if len(d.results)>d.maxResults{d.results=d.results[1:]};d.mu.Unlock()
  }(avail,task)
}
func(d*Dispatcher)QueueLen()int{d.mu.Lock();defer d.mu.Unlock();return d.queue.Len()}
func(d*Dispatcher)Results()[]TaskResult{d.mu.Lock();defer d.mu.Unlock();out:=make([]TaskResult,len(d.results));copy(out,d.results);return out}
func(d*Dispatcher)FormatStatus()string{d.mu.Lock();defer d.mu.Unlock();var sb strings.Builder
  fmt.Fprintf(&sb,"Dispatcher: %d workers, %d queued, %d results\n%s\n\n",len(d.workers),d.queue.Len(),len(d.results),strings.Repeat("─",60))
  busy:=0;for _,b:=range d.busyWorkers{if b{busy++}};fmt.Fprintf(&sb,"  Busy workers: %d\n",busy)
  if d.queue.Len()>0{fmt.Fprintf(&sb,"\n  Pending tasks:\n");for i:=0;i<min(d.queue.Len(),5);i++{t:=d.queue[i];fmt.Fprintf(&sb,"    [%d] %s (pri=%d)\n",i,t.Name,t.Priority)}}
  return sb.String()}
func min(a,b int)int{if a<b{return a};return b}
