package i18n

import (
	"os"
	"testing"
)

func TestDefaultLangEn(t *testing.T) {
	os.Unsetenv("LANG")
	os.Unsetenv("LC_ALL")
	lang := DefaultLang()
	if lang != EN {
		t.Errorf("default should be EN, got %s", lang)
	}
}

func TestDefaultLangZh(t *testing.T) {
	os.Setenv("LANG", "zh_CN.UTF-8")
	defer os.Unsetenv("LANG")
	lang := DefaultLang()
	if lang != ZHCN {
		t.Errorf("zh_CN should resolve to ZHCN, got %s", lang)
	}
}

func TestTr(t *testing.T) {
	tr := Tr(EN)
	if tr("agent.running") != "Running" {
		t.Errorf("EN: got %q", tr("agent.running"))
	}

	trZH := Tr(ZHCN)
	if trZH("agent.running") != "运行中" {
		t.Errorf("ZH: got %q", trZH("agent.running"))
	}
}

func TestTFallback(t *testing.T) {
	SetLang(EN)
	result := T("nonexistent.key")
	if result != "nonexistent.key" {
		t.Errorf("missing key should return key itself, got %q", result)
	}
}

func TestTWithArgs(t *testing.T) {
	SetLang(EN)
	result := T("tool.approval_needed", "bash")
	if result != "⚠ Approve bash?" {
		t.Errorf("got %q", result)
	}
}

func TestGetLang(t *testing.T) {
	SetLang(ZHCN)
	if GetLang() != ZHCN {
		t.Error("GetLang should reflect SetLang")
	}
	SetLang(EN)
}

func TestTrUnknownLang(t *testing.T) {
	// Unknown lang should fallback to English
	tr := Tr("fr")
	if tr("agent.running") != "Running" {
		t.Error("unknown lang should fallback to EN")
	}
}
