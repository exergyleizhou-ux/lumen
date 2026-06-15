package loadgen

import ("context";"fmt";"math";"sort";"strings";"sync";"sync/atomic";"time")

type Config struct{Name string;Concurrency int;TotalRequests int64;RampTime time.Duration;RequestTimeout time.Duration;TargetFn func(int64)error}
func DefaultConfig()Config{return Config{Name:"default",Concurrency:10,TotalRequests:1000,RampTime:100*time.Millisecond,RequestTimeout:30*time.Second}}

type Report struct{Config Config;StartedAt time.Time;Duration time.Duration;Total,Success,Failed int64;Throughput float64;Latencies LatencyStats;Errors map[string]int64}
type LatencyStats struct{Min,Max,Mean,Median,P95,P99 time.Duration;Samples []time.Duration}

func computeLatStats(s []time.Duration)LatencyStats{
  if len(s)==0{return LatencyStats{}}
  sort.Slice(s,func(i,j int)bool{return s[i]<s[j]})
  ls:=LatencyStats{Samples:s,Min:s[0],Max:s[len(s)-1]}
  var sum int64;for _,d:=range s{sum+=int64(d)};ls.Mean=time.Duration(sum/int64(len(s)))
  ls.Median=s[len(s)/2];ls.P95=s[int(math.Ceil(0.95*float64(len(s))))-1];ls.P99=s[int(math.Ceil(0.99*float64(len(s))))-1]
  return ls
}

func NewRunner()*Runner{return &Runner{active:map[string]context.CancelFunc{}}}
type Runner struct{mu sync.Mutex;reports []*Report;active map[string]context.CancelFunc}

func(r*Runner)Run(cfg Config)(*Report,error){
  if cfg.TargetFn==nil{return nil,fmt.Errorf("TargetFn required")}
  if cfg.Concurrency<=0{cfg.Concurrency=1}
  report:=&Report{Config:cfg,StartedAt:time.Now(),Errors:map[string]int64{}}
  var succ,fail atomic.Int64;var lats []time.Duration;var latMu,errMu sync.Mutex;var wg sync.WaitGroup;sem:=make(chan struct{},cfg.Concurrency)
  start:=time.Now()
  for i:=int64(0);i<cfg.TotalRequests;i++{sem<-struct{}{};wg.Add(1);go func(id int64){defer wg.Done();defer func(){<-sem}()
    reqStart:=time.Now();err:=cfg.TargetFn(id);d:=time.Since(reqStart)
    latMu.Lock();lats=append(lats,d);latMu.Unlock()
    if err!=nil{fail.Add(1);errMu.Lock();report.Errors[err.Error()]++;errMu.Unlock()}else{succ.Add(1)}
  }(i)}
  wg.Wait();elapsed:=time.Since(start)
  report.Duration=elapsed;report.Total=cfg.TotalRequests;report.Success=succ.Load();report.Failed=fail.Load()
  report.Throughput=float64(cfg.TotalRequests)/elapsed.Seconds();report.Latencies=computeLatStats(lats)
  r.mu.Lock();r.reports=append(r.reports,report);r.mu.Unlock()
  return report,nil
}

func FormatReport(r *Report)string{
  var sb strings.Builder
  fmt.Fprintf(&sb,"Load Test: %s\n%s\n\n",r.Config.Name,strings.Repeat("═",60))
  fmt.Fprintf(&sb,"  Concurrency: %d\n  Total: %d req\n  Duration: %v\n  Throughput: %.1f req/s\n",r.Config.Concurrency,r.Total,r.Duration,r.Throughput)
  fmt.Fprintf(&sb,"  Success: %d (%.1f%%)  Failed: %d\n\n",r.Success,float64(r.Success)/float64(r.Total)*100,r.Failed)
  fmt.Fprintf(&sb,"  Latency: min=%v mean=%v median=%v p95=%v p99=%v max=%v\n",r.Latencies.Min,r.Latencies.Mean,r.Latencies.Median,r.Latencies.P95,r.Latencies.P99,r.Latencies.Max)
  if len(r.Errors)>0{fmt.Fprintf(&sb,"\n  Errors:\n");for e,c:=range r.Errors{fmt.Fprintf(&sb,"    [%d×] %s\n",c,trunc(e,80))}}
  return sb.String()
}
func trunc(s string,n int)string{if len(s)<=n{return s};return s[:n-3]+"..."}
