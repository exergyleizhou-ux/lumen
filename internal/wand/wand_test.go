package wand
import ("testing")
func TestWandDiagnose(t *testing.T){w:=NewWand();for _,d:=range BuiltinDiagnostics(){w.RegisterDiagnostic(d)};issues,_:=w.Diagnose();t.Logf("found %d issues",len(issues))}
func TestFix(t *testing.T){w:=NewWand();results:=w.Fix([]Issue{{ID:"fix-001",Severity:"low",FixFn:func()error{return nil}}});if len(results)!=1{t.Error("fix count")}}
func TestSuggester(t *testing.T){sg:=NewSuggester();sg.AddPattern("timeout","Increase timeout");suggestions:=sg.Suggest("connection timeout error");if len(suggestions)!=1{t.Error("suggest")}}
