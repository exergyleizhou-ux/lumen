package evacuate
import ("testing";"time")
func TestDrainer(t *testing.T){d:=NewDrainer(100*time.Millisecond,time.Second);d.AddConn("c1");d.AddConn("c2");if d.ConnCount()!=2{t.Error("count")};d.Drain();time.Sleep(300*time.Millisecond);if d.State()!=StateDrained{t.Error("should drain")}}
func TestRedirector(t *testing.T){r:=NewRedirector([]string{"a","b","c"});tgt:=r.Next();if tgt==""{t.Error("no target")};r.MarkUnhealthy(tgt);next:=r.Next();if next==tgt{t.Error("should skip unhealthy")}}
func TestStateString(t *testing.T){if StateActive.String()!="active"{t.Error("state string")}}
