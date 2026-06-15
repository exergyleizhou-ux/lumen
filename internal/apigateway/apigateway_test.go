package apigateway
import ("net/http";"net/http/httptest";"strings";"testing")
func TestRouterSimple(t *testing.T){r:=NewRouter();r.HandleFunc("GET","/health","health",func(w http.ResponseWriter,r *http.Request){w.Write([]byte("ok"))});req:=httptest.NewRequest("GET","/health",nil);rec:=httptest.NewRecorder();r.ServeHTTP(rec,req);if rec.Code!=200{t.Error("code")}}
func TestRouter404(t *testing.T){r:=NewRouter();req:=httptest.NewRequest("GET","/missing",nil);rec:=httptest.NewRecorder();r.ServeHTTP(rec,req);if rec.Code!=404{t.Error("404")}}
func TestCORSMiddleware(t *testing.T){mw:=CORSMiddleware([]string{"*"});handler:=mw(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.WriteHeader(200)}));req:=httptest.NewRequest("OPTIONS","/",nil);rec:=httptest.NewRecorder();handler.ServeHTTP(rec,req);if rec.Code!=200{t.Error("cors")}}
func TestRecoveryMiddleware(t *testing.T){mw:=RecoveryMiddleware();handler:=mw(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){panic("test")}));req:=httptest.NewRequest("GET","/",nil);rec:=httptest.NewRecorder();handler.ServeHTTP(rec,req);if rec.Code!=500{t.Error("recovery")}}
func TestRateLimiter(t *testing.T){rl:=NewRateLimiter(10,3);if!rl.Allow("k"){t.Error("allow")}}
func TestAPIMetrics(t *testing.T){m:=NewAPIMetrics();m.Record("/test",0);if s:=m.FormatStats();!strings.Contains(s,"/test"){t.Error("format")}}
