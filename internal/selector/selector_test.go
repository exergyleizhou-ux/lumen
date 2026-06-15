package selector
import ("testing")
func TestParseMatch(t *testing.T){s,err:=Parse("env=prod");if err!=nil{t.Fatal(err)};if!s.Matches(map[string]string{"env":"prod"}){t.Error("match")}}
func TestNotEqual(t *testing.T){s,_:=Parse("env!=staging");if!s.Matches(map[string]string{"env":"prod"}){t.Error("ne match")}}
func TestExists(t *testing.T){s,_:=Parse("required-key");if!s.Matches(map[string]string{"required-key":"any"}){t.Error("exists")}}
func TestFilter(t *testing.T){f:=NewFilter();f.Add("a",map[string]string{"env":"prod"});f.Add("b",map[string]string{"env":"staging"});s,_:=Parse("env=prod");ids:=f.Match(s);if len(ids)!=1||ids[0]!="a"{t.Error("filter")}}
