package proptest
import ("math/rand";"testing")
func TestIntRange(t *testing.T){g:=IntRange(1,10);v:=g.Gen(rand.New(rand.NewSource(42)));if v<1||v>10{t.Error("range")}}
func TestRunnerAllPass(t *testing.T){r:=NewRunner(Config{MaxTests:10});r.Check("always-true",func()bool{return true});if!r.AllPassed(){t.Error("should pass")}}
func TestRunnerFails(t *testing.T){r:=NewRunner(Config{MaxTests:5});r.Check("always-false",func()bool{return false});if r.AllPassed(){t.Error("should fail")}}
