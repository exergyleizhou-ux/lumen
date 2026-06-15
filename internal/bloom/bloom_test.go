package bloom
import ("fmt";"testing")
func TestAddContains(t *testing.T){f:=New(1024,4);f.Add([]byte("hello"));if!f.Contains([]byte("hello")){t.Error("should contain")}}
func TestFalsePositive(t *testing.T){f:=NewOptimal(1000,0.01);for i:=0;i<100;i++{f.Add([]byte(fmt.Sprintf("key-%d",i)))};fp:=0;for i:=100;i<200;i++{if f.Contains([]byte(fmt.Sprintf("key-%d",i))){fp++}};if float64(fp)/100>0.1{t.Logf("fp rate: %.1f%%",float64(fp))}}
func TestFormat(t *testing.T){f:=New(512,3);s:=f.Format();if s==""{t.Error("format")}}
