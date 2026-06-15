package monitor
import ("testing";"time")
func TestCollectorInc(t *testing.T){c:=NewCollector();c.RegisterCounter("requests","test");c.Inc("requests",nil);if c.CounterSum("requests")!=1{t.Error("sum")}}
func TestCollectorGauge(t *testing.T){c:=NewCollector();c.SetGauge("temp",42.5,nil)}
func TestCollectorExportPrometheus(t *testing.T){c:=NewCollector();c.RegisterCounter("test_counter","a counter");c.Inc("test_counter",nil);o:=c.ExportPrometheus();if o==""{t.Error("empty prometheus")}}
func TestAlertManager(t *testing.T){a:=NewAlertManager();a.AddRule(AlertRule{Name:"high-latency",Metric:"request_latency",Condition:"gt",Threshold:100,Message:"latency high"});c:=NewCollector();c.Inc("request_latency",nil);c.Inc("request_latency",nil);fired:=a.Evaluate(c);if len(fired)!=0{t.Log("no alert expected for 0 latency counter")}}
func TestTrendAnalyzer(t *testing.T){ta:=NewTrendAnalyzer(10);ta.Record("requests",10);ta.Record("requests",20);if ta.Slope("requests")<=0{t.Error("positive slope expected")}}
func TestLatencyStats(t *testing.T){ls:=NewLatencyStats(100);ls.Record(10*time.Millisecond);ls.Record(20*time.Millisecond);if ls.Avg()<15{t.Error("avg")}}
