package heapdump
import ("testing";"time")
func TestCollectorSnap(t *testing.T){c:=NewCollector(10);s:=c.Snap();if s==nil||s.Alloc==0{t.Error("snapshot")};if c.Latest()!=s{t.Error("latest")}}
func TestDelta(t *testing.T){c:=NewCollector(10);b:=c.Snap();a:=c.Snap();d:=Delta(b,a);if d==nil{t.Error("delta")}}
func TestFormatSnapshot(t *testing.T){s:=&Snapshot{Timestamp:time.Now(),Alloc:1024,Goroutines:10,NumGC:5};f:=FormatSnapshot(s);if f==""{t.Error("format")}}
