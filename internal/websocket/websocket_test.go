package websocket
import ("encoding/json";"net/http";"net/http/httptest";"testing";"time")
func TestHubConnect(t *testing.T) {
	h := NewHub(); conn := NewMockConn(); h.Connect(conn)
	if h.ConnectionCount() != 1 { t.Error("connect count") }
}
func TestChannelPubSub(t *testing.T) {
	h := NewHub(); conn := NewMockConn(); h.Connect(conn); h.Join(conn, "events")
	count := h.Emit("events", &Event{Type: "test", Timestamp: time.Now()})
	if count != 1 { t.Error("emit count") }
}
func TestBroadcast(t *testing.T) {
	h := NewHub(); c1 := NewMockConn(); c2 := NewMockConn()
	h.Connect(c1); h.Connect(c2)
	count := h.Broadcast(&Event{Type: "broadcast", Timestamp: time.Now()})
	if count != 2 { t.Error("broadcast count") }
}
func TestDisconnect(t *testing.T) {
	h := NewHub(); conn := NewMockConn(); h.Connect(conn); h.Disconnect(conn)
	if h.ConnectionCount() != 0 { t.Error("disconnect count") }
}
func TestEventLog(t *testing.T) {
	el := NewEventLog(10)
	el.Record(&Event{Type: "a", Timestamp: time.Now()})
	time.Sleep(1 * time.Millisecond)
	now := time.Now()
	time.Sleep(1 * time.Millisecond)
	el.Record(&Event{Type: "b", Timestamp: time.Now()})
	replay := el.Replay(now)
	if len(replay) != 1 { t.Error("replay count") }
}
func TestFormatStats(t *testing.T) {
	h := NewHub(); conn := NewMockConn(); h.Connect(conn)
	s := FormatStats(h.Stats())
	if s == "" { t.Error("format") }
}
func TestWSHandler(t *testing.T) {
	h := NewHub(); wh := NewWSHandler(h)
	req, _ := http.NewRequest("GET", "/ws", nil)
	rec := httptest.NewRecorder(); wh.ServeHTTP(rec, req)
	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["status"] != "connected" { t.Error("ws handler") }
}
