package extender
import ("testing")
func TestRegistryEnable(t *testing.T) {
	r := NewRegistry()
	r.Register(&Extension{Name: "ext1", Version: "1.0"})
	if err := r.Enable("ext1"); err != nil { t.Fatal(err) }
	if !r.Enabled()[0].Enabled { t.Error("should be enabled") }
}
func TestDisable(t *testing.T) {
	r := NewRegistry()
	r.Register(&Extension{Name: "ext2", Version: "1.0", InitFn: func() error { return nil }, CloseFn: func() error { return nil }})
	r.Enable("ext2")
	r.Disable("ext2")
	if len(r.Enabled()) != 0 { t.Error("should be disabled") }
}
func TestTools(t *testing.T) {
	r := NewRegistry()
	r.Register(&Extension{Name: "tools-ext", Version: "1.0", Tools: []string{"tool.a", "tool.b"}})
	r.Enable("tools-ext")
	tools := r.Tools()
	if len(tools) != 2 { t.Error("tools") }
}
func TestReload(t *testing.T) {
	r := NewRegistry()
	r.Register(&Extension{Name: "reloadable", Version: "1.0"})
	r.Enable("reloadable")
	if err := r.Reload("reloadable"); err != nil { t.Error("reload") }
}
