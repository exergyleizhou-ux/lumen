package metrics
import ("fmt";"sort";"strings";"sync";"sync/atomic";"time")
type Counter struct{ val atomic.Int64 }
func NewCounter()*Counter{return &Counter{}}
func(c*Counter)Inc(){c.val.Add(1)}
func(c*Counter)Add(n int64){c.val.Add(n)}
func(c*Counter)Value()int64{return c.val.Load()}
func(c*Counter)Reset(){c.val.Store(0)}
type Gauge struct{ val atomic.Int64 }
func NewGauge()*Gauge{return &Gauge{}}
func(g*Gauge)Set(n int64){g.val.Store(n)}
func(g*Gauge)Value()int64{return g.val.Load()}
type Histogram struct{mu sync.Mutex;buckets []int64;bucketBounds []float64;count int64;sum float64}
func NewHistogram(bounds []float64)*Histogram{return &Histogram{bucketBounds:bounds,buckets:make([]int64,len(bounds)+1)}}
func(h*Histogram)Observe(v float64){h.mu.Lock();defer h.mu.Unlock();h.count++;h.sum+=v;for i,b:=range h.bucketBounds{if v<=b{h.buckets[i]++;return}};h.buckets[len(h.bucketBounds)]++}
func(h*Histogram)Snapshot()(count int64,sum float64,buckets []int64){h.mu.Lock();defer h.mu.Unlock();bs:=make([]int64,len(h.buckets));copy(bs,h.buckets);return h.count,h.sum,bs}
type Timer struct{start time.Time;hist *Histogram}
func StartTimer(h *Histogram)*Timer{return &Timer{start:time.Now(),hist:h}}
func(t*Timer)Stop(){t.hist.Observe(float64(time.Since(t.start).Milliseconds()))}
type Registry struct{mu sync.Mutex;counters map[string]*Counter;gauges map[string]*Gauge;histograms map[string]*Histogram}
func NewRegistry()*Registry{return &Registry{counters:map[string]*Counter{},gauges:map[string]*Gauge{},histograms:map[string]*Histogram{}}}
func(r*Registry)Counter(name string)*Counter{r.mu.Lock();defer r.mu.Unlock();if c,ok:=r.counters[name];ok{return c};c:=NewCounter();r.counters[name]=c;return c}
func(r*Registry)Gauge(name string)*Gauge{r.mu.Lock();defer r.mu.Unlock();if g,ok:=r.gauges[name];ok{return g};g:=NewGauge();r.gauges[name]=g;return g}
func(r*Registry)Histogram(name string,bounds []float64)*Histogram{r.mu.Lock();defer r.mu.Unlock();if h,ok:=r.histograms[name];ok{return h};h=NewHistogram(bounds);r.histograms[name]=h;return h}
func(r*Registry)Snapshot()map[string]any{r.mu.Lock();defer r.mu.Unlock();m:=map[string]any{};for n,c:=range r.counters{m["counter."+n]=c.Value()};for n,g:=range r.gauges{m["gauge."+n]=g.Value()};for n,h:=range r.histograms{c,sum,_:=h.Snapshot();m["hist."+n+".count"]=c;m["hist."+n+".sum"]=sum};return m}
func(r*Registry)FormatStats()string{r.mu.Lock();defer r.mu.Unlock();var sb strings.Builder;sb.WriteString(fmt.Sprintf("Metrics (%d counters, %d gauges, %d histograms):\n\n",len(r.counters),len(r.gauges),len(r.histograms)))
type pair struct{k string;v int64};var items []pair;for n,c:=range r.counters{items=append(items,pair{n,c.Value()})};sort.Slice(items,func(i,j int)bool{return items[i].v>items[j].v});sb.WriteString("Counters:\n");for _,it:=range items{fmt.Fprintf(&sb,"  %-30s %d\n",it.k,it.v)};return sb.String()}
