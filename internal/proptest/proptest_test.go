package proptest
import ("testing")
func TestIntRange(t *testing.T){g:=IntRange(1,10);r:=rand.New(rand.NewSource(42));for i:=0;i<20;i++{v:=g.Gen(r);if v<1||v>10{t.Error("range")}}}
func TestOneOf(t *testing.T){g:=OneOf("a","b","c");r:=rand.New(rand.NewSource(42));v:=g.Gen(r);if v!="a"&&v!="b"&&v!="c"{t.Error("oneof")}}
func TestRunnerAllPass(t *testing.T){r:=NewRunner(Config{MaxTests:10});r.Check("always-true",func()bool{return true});if!r.AllPassed(){t.Error("should pass")}}
func TestRunnerFails(t *testing.T){r:=NewRunner(Config{MaxTests:5});r.Check("always-false",func()bool{return false});if r.AllPassed(){t.Error("should fail")}}
func TestShrinker(t *testing.T){s:=NewShrinker(20);failing:="abcdefgh";result:=s.Shrink(failing,func(s string)bool{return len(s)>3});if len(result)>3{t.Error("shrink failed")}}
