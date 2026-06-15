package toolpipeline
import ("strings";"testing")
func TestPipelineSimple(t *testing.T){p:=NewPipeline("test");p.AddStep(&Step{Name:"s1",Tool:"read",Args:map[string]any{"path":"x"},Retries:1});exec:=func(n string,a map[string]any)(string,error){return "ok",nil};p.Run(exec);if!p.AllDone(){t.Error("done")}}
func TestPipelineDeps(t *testing.T){p:=NewPipeline("dep");p.AddStep(&Step{Name:"s1",Tool:"read",Args:map[string]any{},Retries:1});p.AddStep(&Step{Name:"s2",Tool:"write",Args:map[string]any{},DependsOn:[]string{"s1"},Retries:1});p.Run(func(n string,a map[string]any)(string,error){return "ok",nil});if!p.AllDone(){t.Error("deps")}}
func TestBuilder(t *testing.T){b:=NewBuilder("build");p:=b.Then("read","read_file",nil).Then("write","write_file",nil).Build();if p.StepCount()!=2{t.Error("count")}}
func TestFormat(t *testing.T){p:=NewPipeline("fmt");p.AddStep(&Step{Name:"s1",Tool:"bash",Args:map[string]any{}});if!strings.Contains(p.Format(),"s1"){t.Error("fmt")}}
