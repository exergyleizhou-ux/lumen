package builtin

import (
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.0.0.5", "::1", // loopback
		"169.254.169.254", "169.254.1.1", "fe80::1", // link-local (incl. cloud metadata)
		"10.0.0.1", "172.16.0.1", "172.31.255.255", "192.168.1.1", "fc00::1", "fd00::1", // private
		"0.0.0.0", "::", // unspecified
		"100.64.0.1", // carrier-grade NAT
	}
	for _, s := range blocked {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("bad test IP %q", s)
		}
		if !isBlockedIP(ip) {
			t.Errorf("isBlockedIP(%s) = false, want true (should be blocked)", s)
		}
	}
	allowed := []string{"1.1.1.1", "8.8.8.8", "93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946"}
	for _, s := range allowed {
		ip := net.ParseIP(s)
		if isBlockedIP(ip) {
			t.Errorf("isBlockedIP(%s) = true, want false (public address)", s)
		}
	}
}

func TestSSRFDialControl(t *testing.T) {
	t.Setenv(EnvWebFetchAllowLocal, "")
	if err := ssrfDialControl("tcp", "127.0.0.1:80", nil); err == nil {
		t.Error("dial to loopback must be blocked by default")
	}
	if err := ssrfDialControl("tcp", "169.254.169.254:80", nil); err == nil {
		t.Error("dial to cloud-metadata IP must be blocked")
	}
	if err := ssrfDialControl("tcp", "1.1.1.1:443", nil); err != nil {
		t.Errorf("dial to a public IP must be allowed, got %v", err)
	}
	// Opt-out for local development.
	t.Setenv(EnvWebFetchAllowLocal, "1")
	if err := ssrfDialControl("tcp", "127.0.0.1:80", nil); err != nil {
		t.Errorf("with allow-local set, loopback should be permitted, got %v", err)
	}
}

func TestCheckFetchURL(t *testing.T) {
	t.Setenv(EnvWebFetchAllowLocal, "")
	bad := []string{
		"file:///etc/passwd",
		"ftp://example.com/x",
		"gopher://x",
		"http://127.0.0.1/admin",   // loopback literal
		"http://169.254.169.254/",  // metadata literal
		"https://[::1]/",           // ipv6 loopback literal
		"http://10.0.0.5/internal", // private literal
		"not a url",
	}
	for _, u := range bad {
		if err := checkFetchURL(u); err == nil {
			t.Errorf("checkFetchURL(%q) = nil, want error", u)
		}
	}
	good := []string{
		"http://example.com/path",
		"https://api.github.com/repos",
		"https://1.1.1.1/",
	}
	for _, u := range good {
		if err := checkFetchURL(u); err != nil {
			t.Errorf("checkFetchURL(%q) = %v, want nil", u, err)
		}
	}
}
