package verify

import (
	"testing"
)

func TestVerifier(t *testing.T) {
	v := NewVerifier()
	for _, c := range BuiltinChecks() {
		v.AddCheck(c)
	}
	results := v.Verify("hello")
	if len(results) != 5 {
		t.Error("should run 5 checks")
	}
}
func TestHtmlInjection(t *testing.T) {
	v := NewVerifier()
	v.AddCheck(BuiltinChecks()[1]) // html injection check
	results := v.Verify("<script>alert(1)</script>")
	if results[0].Passed {
		t.Error("should fail")
	}
}
func TestIntegrity(t *testing.T) {
	iv := NewIntegrityVerifier()
	iv.Register("test.txt", "abc123")
	r := iv.Check("test.txt", []byte("wrong data"))
	if r.Match {
		t.Error("should not match")
	}
}
func TestRegexValidator(t *testing.T) {
	rv := NewRegexValidator()
	rv.AddRule("email", `^[a-z]+@[a-z]+\.com$`)
	r := rv.Validate("test@example.com")
	if !r["email"] {
		t.Error("should match")
	}
}
