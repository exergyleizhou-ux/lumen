package tracepoint
import ("testing";"time")
func TestFire(t *testing.T){r:=NewRegistry();r.Register("load.config","startup");r.Fire("load.config");if r.Hits("load.config")!=1{t.Error("hit count")}}
func TestDisable(t *testing.T){r:=NewRegistry();r.Register("debug.log","debug");r.Disable("debug.log");r.Fire("debug.log");if r.Hits("debug.log")!=0{t.Error("should not fire")}}
func TestCondition(t *testing.T){r:=NewRegistry();r.Register("cond","test");count:=0;r.SetCondition("cond",func()bool{count++;return count>2});r.Fire("cond");r.Fire("cond");r.Fire("cond");if r.Hits("cond")!=1{t.Error("condition should limit")}}
func TestSession(t *testing.T){s:=NewSession();s.Begin("root");s.Begin("child");s.End();s.End();if s.TotalDuration()==0{t.Error("duration")};f:=s.FormatTree();if f==""{t.Error("format")}}
func TestFlameChart(t *testing.T){s:=NewSession();s.Begin("op");time.Sleep(time.Millisecond);s.End();f:=s.FormatFlameChart();if f==""{t.Error("flame")}}
