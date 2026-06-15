package signal
import ("testing";"time")
func TestManagerHooks(t *testing.T) {
	m := NewManager(5 * time.Second)
	called := false
	m.OnSignal("test", 1, func() error { called = true; return nil })
	m.Shutdown()
	if !called { t.Error("hook should be called") }
	if len(m.ListHooks()) == 0 { t.Error("hooks") }
}
func TestPinger(t *testing.T) {
	p := NewPinger()
	if !p.Alive() { t.Error("should be alive") }
	p.Ping()
	if p.LastPing().IsZero() { t.Error("ping time") }
}
func TestFormatHooks(t *testing.T) {
	m := NewManager(5 * time.Second)
	m.OnSignal("http", 1, func() error { return nil })
	m.OnSignal("db", 2, func() error { return nil })
	s := m.FormatHooks()
	if s == "" { t.Error("format") }
}
