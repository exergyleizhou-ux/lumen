package hotplug

import (
	"strings"
	"testing"
)

func TestRegistryRegister(t *testing.T) {
	r := NewRegistry()
	r.Register(&Plugin{Name: "test", Type: PluginTool})
	if r.Count() != 1 {
		t.Error("count")
	}
}
func TestRegistryUnregister(t *testing.T) {
	r := NewRegistry()
	r.Register(&Plugin{Name: "tmp", Type: PluginTool})
	r.Unregister("tmp")
	if r.Count() != 0 {
		t.Error("unregister")
	}
}
func TestRegistryListByType(t *testing.T) {
	r := NewRegistry()
	r.Register(&Plugin{Name: "t1", Type: PluginTool})
	r.Register(&Plugin{Name: "s1", Type: PluginSkill})
	if len(r.ListByType(PluginTool)) != 1 {
		t.Error("filter")
	}
}
func TestFormatPlugins(t *testing.T) {
	o := FormatPlugins([]*Plugin{{Name: "p1", Type: PluginTool, Status: "loaded"}})
	if !strings.Contains(o, "p1") {
		t.Error("format")
	}
}
func TestManager(t *testing.T) {
	m := NewManager()
	m.ToolRegistry().Register(&Plugin{Name: "tm", Type: PluginTool})
	if m.ToolRegistry().Count() != 1 {
		t.Error("manager")
	}
}
func TestOnChange(t *testing.T) {
	r := NewRegistry()
	called := false
	r.OnChange("load", func(p *Plugin) { called = true })
	r.Register(&Plugin{Name: "cb", Type: PluginTool})
	if !called {
		t.Error("callback")
	}
}
