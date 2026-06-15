package e2e
import ("net/http";"net/http/httptest";"testing";"time";"lumen/internal/apigateway";"lumen/internal/audit";"lumen/internal/configlive";"lumen/internal/orchestrator")
func TestFullPipeline(t *testing.T){
  store:=configlive.NewStore();store.Set("agent.name","lumen","test");_=store
  trail:=audit.NewTrail(100);e:=trail.Record("agent","au_flow","resource","success",nil);if e.Action!="au_flow"{t.Error("audit")}
  ok,_:=trail.Verify();if!ok{t.Error("chain broken")}
}
func TestOrchestrator(t *testing.T){
  pool:=orchestrator.NewAgentPool();ex:=orchestrator.NewExecutor(orchestrator.DefaultConfig(),pool);_=ex
}
func TestConfigLive(t *testing.T){
  store:=configlive.NewStore();changed:=make(chan string,1)
  store.Watch("f",func(k string,o,n any){changed<-k});store.Set("f.a",false,"d");store.Set("f.a",true,"o")
  _=<-changed
}
func TestAPIGateway(t *testing.T){
  tokens:=map[string]bool{"tok":true};mw:=apigateway.AuthMiddleware(func(t string)bool{return tokens[t]})
  h:=mw(http.HandlerFunc(func(w http.ResponseWriter,r*http.Request){w.WriteHeader(200)}))
  req:=httptest.NewRequest("GET","/",nil);req.Header.Set("Authorization","Bearer tok");rec:=httptest.NewRecorder();h.ServeHTTP(rec,req)
  if rec.Code!=200{t.Error("auth")}
}
