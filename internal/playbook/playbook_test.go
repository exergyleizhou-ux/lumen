package playbook

import ("fmt";"testing";"time")

func TestExecutorRun(t *testing.T) {
	e := NewExecutor(func(action string, with map[string]any) (string, error) { return "ok", nil })
	pb := &Playbook{Name: "test", Version: "1.0", Steps: []Step{{Name: "step1", Action: "tool.echo", With: map[string]any{"msg": "hello"}}, {Name: "step2", Action: "tool.upper", With: map[string]any{"text": "world"}}}}
	result, err := e.Run(pb)
	if err != nil { t.Fatal(err) }
	if result.State != "success" { t.Errorf("want success, got %s", result.State) }
	if len(result.Steps) != 2 { t.Error("step count") }
}
func TestExecutorFail(t *testing.T) {
	e := NewExecutor(func(action string, with map[string]any) (string, error) { if action == "fail.me" { return "", fmt.Errorf("intentional") }; return "ok", nil })
	pb := &Playbook{Name: "test-fail", Version: "1.0", Steps: []Step{{Name: "s1", Action: "fail.me"}, {Name: "s2", Action: "never.runs"}}}
	result, _ := e.Run(pb)
	if result.State != "failed" { t.Errorf("want failed, got %s", result.State) }
}
func TestLibrary(t *testing.T) {
	l := NewLibrary()
	l.Register(&Playbook{Name: "pb1", Version: "1.0", Steps: []Step{{Name: "s1", Action: "a"}}})
	pb, ok := l.Get("pb1")
	if !ok || pb.Name != "pb1" { t.Error("get") }
	if len(l.Names()) != 1 { t.Error("names") }
}
func TestValidate(t *testing.T) {
	l := NewLibrary()
	errs := l.Validate(&Playbook{Name: "", Steps: nil})
	if len(errs) != 2 { t.Error("should have 2 errors") }
}
func TestFormatRunStatus(t *testing.T) {
	r := &RunStatus{Playbook: "pb", Version: "1.0", State: "success", Duration: time.Second, Steps: []StepStatus{{StepName: "s1", State: "success"}}}
	s := FormatRunStatus(r)
	if s == "" { t.Error("format") }
}
