package e2e
import ("net/http";"net/http/httptest";"testing";"lumen/internal/apigateway";"lumen/internal/audit";"lumen/internal/configlive";"lumen/internal/orchestrator")
func TestAudit(t *testing.T){trail:=audit.NewTrail(100);e:=trail.Record("agent","au_flow","r","ok",nil);if e.Action!="au_flow"{t.Error("audit")};ok,_:=trail.Verify();if!ok{t.Error("chain")}}
func TestOrch(t *testing.T){pool:=orchestrator.NewAgentPool();ex:=orchestrator.NewExecutor(orchestrator.DefaultConfig(),pool);if ex==nil{t.Error("executor")}}
func TestConfig(t *testing.T){s:=configlive.NewStore();s.Set("k","v","d");v,ok:=s.Get("k");if!ok||v!="v"{t.Error("get")}}
func TestGateway(t *testing.T){m:=apigateway.AuthMiddleware(func(t string)bool{return t=="tok"});h:=m(http.HandlerFunc(func(w http.ResponseWriter,r*http.Request){w.WriteHeader(200)}));req:=httptest.NewRequest("GET","/",nil);req.Header.Set("Authorization","Bearer tok");r:=httptest.NewRecorder();h.ServeHTTP(r,req);if r.Code!=200{t.Error("auth")}}
