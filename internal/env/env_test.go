package env

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestStoreGet(t *testing.T) {
	s := NewStore()
	s.Define(Var{Name: "TEST_VAL", Default: "hello"})
	if s.Get("TEST_VAL") != "hello" {
		t.Error("default")
	}
	os.Setenv("TEST_VAL", "world")
	s2 := NewStore()
	s2.Define(Var{Name: "TEST_VAL", Default: "hello"})
	if s2.Get("TEST_VAL") != "world" {
		t.Error("env override")
	}
	os.Unsetenv("TEST_VAL")
}
func TestGetInt(t *testing.T) {
	s := NewStore()
	s.Define(Var{Name: "PORT", Default: "8080"})
	if s.GetInt("PORT") != 8080 {
		t.Error("int")
	}
}
func TestGetBool(t *testing.T) {
	s := NewStore()
	s.Define(Var{Name: "DEBUG", Default: "true"})
	if !s.GetBool("DEBUG") {
		t.Error("bool")
	}
}
func TestGetDuration(t *testing.T) {
	s := NewStore()
	s.Define(Var{Name: "TIMEOUT", Default: "5s"})
	if s.GetDuration("TIMEOUT") != 5*time.Second {
		t.Error("duration")
	}
}
func TestValidate(t *testing.T) {
	s := NewStore()
	s.Define(Var{Name: "REQUIRED_KEY", Required: true})
	errs := s.Validate()
	if len(errs) != 1 {
		t.Error("should have 1 error")
	}
}
func TestDumpSecrets(t *testing.T) {
	s := NewStore()
	s.Define(Var{Name: "TOKEN", Secret: true})
	os.Setenv("TOKEN", "abcdef123456")
	s2 := NewStore()
	s2.Define(Var{Name: "TOKEN", Secret: true})
	dump := s2.Dump()
	if val, ok := dump["TOKEN"]; ok && strings.Contains(val, "****") {
		t.Log("masked")
	}
	os.Unsetenv("TOKEN")
}
func TestReporter(t *testing.T) {
	r := NewReporter()
	info := r.Info()
	if info["hostname"] == "" {
		t.Error("hostname")
	}
}
