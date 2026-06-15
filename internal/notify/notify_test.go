package notify
import ("context";"testing")
func TestBellChannel(t *testing.T){bc:=NewBellChannel();if bc.Name()!="bell"{t.Error("name")};err:=bc.Send(context.Background(),NewMessage("test","body"));if err!=nil{t.Error("send")}}
func TestMessage(t *testing.T){m:=NewMessage("title","body");if m.Title!="title"{t.Error("title")};m.WithPriority(PriorityHigh);if m.Priority!=PriorityHigh{t.Error("priority")}}
func TestDesktopChannel(t *testing.T){dc:=NewDesktopChannel("Lumen");if dc.Name()!="desktop"{t.Error("name")}}
