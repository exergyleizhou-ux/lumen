package cronparser
import ("testing";"time")
func TestParseStar(t *testing.T){e,err:=Parse("* * * * *");if err!=nil{t.Fatal(err)};if e.Raw!="* * * * *"{t.Error("raw")}}
func TestMatches(t *testing.T){e,_:=Parse("*/5 * * * *");if!e.Minute.matches(0)&&!e.Minute.matches(5){t.Error("step match")}}
func TestNext(t *testing.T){e,_:=Parse("0 12 * * *");next:=e.Next(time.Now());if next.Hour()!=12||next.Minute()!=0{t.Error("next at noon")}}
func TestDescribe(t *testing.T){e,_:=Parse("0 9 * * 1-5");d:=e.Describe();if d==""{t.Error("describe")}}
func TestFormatSchedule(t *testing.T){s:=FormatSchedule("0 0 * * *",3);if s==""{t.Error("format")}}
