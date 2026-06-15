package maestro
import ("context";"fmt";"strings";"testing";"time")
func TestOrchestratorRun(t *testing.T){ctx:=context.Background();o:=NewOrchestrator();wb:=NewWorkflow("wf1","Simple Workflow");wb.Task("t1","Task1",nil,func(ctx context.Context)error{return nil});o.RegisterWorkflow(wb.Build());result,err:=o.Run(ctx,"wf1");if err!=nil{t.Fatal(err)};if result.Status!="success"{t.Errorf("want success, got %s",result.Status)}}
func TestOrchestratorFail(t *testing.T){ctx:=context.Background();o:=NewOrchestrator();wb:=NewWorkflow("wf2","Failing");wb.Task("t1","FailTask",nil,func(ctx context.Context)error{return fmt.Errorf("boom")});o.RegisterWorkflow(wb.Build());result,_:=o.Run(ctx,"wf2");if result.Status!="failed"{t.Errorf("want failed, got %s",result.Status)}}
func TestRetryPolicyDefault(t *testing.T){rp:=DefaultRetryPolicy();if rp.MaxRetries!=3{t.Error("default retry")}}
