package config

import "testing"

func TestTemplateByIDDeepseek(t *testing.T) {
	tpl, ok := TemplateByID("deepseek")
	if !ok || tpl.Adapter != "deepseek" {
		t.Fatalf("%+v", tpl)
	}
}

func TestTemplateByIDCustom(t *testing.T) {
	tpl, ok := TemplateByID("custom")
	if !ok || !tpl.BaseURLEditable {
		t.Fatal()
	}
}

func TestListTemplatesCount(t *testing.T) {
	if len(ListTemplates()) < 7 {
		t.Fatalf("want >=7 templates")
	}
}

func TestTemplateIDForLegacySlot(t *testing.T) {
	cases := map[string]string{
		"deepseek": "deepseek", "relay-glm": "glm", "unknown": "custom",
	}
	for in, want := range cases {
		if got := TemplateIDForLegacySlot(in); got != want {
			t.Fatalf("%s: %s", in, got)
		}
	}
}
