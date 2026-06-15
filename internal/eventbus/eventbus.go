package eventbus
import ("fmt";"strings";"sync";"time")
type Handler func(Event)
type Event struct{ID,Type,Source string;Data map[string]any;Timestamp time.Time}
type Bus struct{mu sync.RWMutex;handlers map[string][]Handler;history []Event;maxH int}
func NewBus(maxH int)*Bus{return &Bus{handlers:map[string][]Handler{},maxH:maxH}}
func(b*Bus)Publish(e Event){e.Timestamp=time.Now();b.mu.Lock();b.history=append(b.history,e);if len(b.history)>b.maxH{b.history=b.history[len(b.history)-b.maxH:]};hs:=make([]Handler,len(b.handlers[e.Type]));copy(hs,b.handlers[e.Type]);b.mu.Unlock();for _,h:=range hs{go h(e)}}
func(b*Bus)Subscribe(t string,h Handler){b.mu.Lock();defer b.mu.Unlock();b.handlers[t]=append(b.handlers[t],h)}
func(b*Bus)History(t string,limit int)[]Event{b.mu.RLock();defer b.mu.RUnlock();var o []Event;for i:=len(b.history)-1;i>=0&&len(o)<limit;i--{if t==""||b.history[i].Type==t{o=append(o,b.history[i])}};return o}
type Emitter struct{bus *Bus;source string}
func NewEmitter(bus *Bus,source string)*Emitter{return &Emitter{bus:bus,source:source}}
func(e*Emitter)Emit(et string,data map[string]any){e.bus.Publish(Event{ID:fmt.Sprintf("evt-%d",time.Now().UnixNano()),Type:et,Source:e.source,Data:data})}
func FormatEvents(es []Event)string{var sb strings.Builder;fmt.Fprintf(&sb,"%d events:\n",len(es));for _,e:=range es{fmt.Fprintf(&sb,"  [%s] %s\n",e.Timestamp.Format("15:04:05"),e.Type)};return sb.String()}
