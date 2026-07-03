package config

import (
	"testing"
)

func TestValidatePorts(t *testing.T) {
	cases := []struct {
		name    string
		f       File
		wantErr bool
	}{
		{"defaults ok", Default(), false},
		{"reserved real port", File{Provider: "deepseek", ProxyPort: 8765, SandboxPort: 8990, Mode: "proxy"}, true},
		{"same ports", File{Provider: "deepseek", ProxyPort: 18991, SandboxPort: 18991, Mode: "proxy"}, true},
		{"bad provider", File{Provider: "bogus", ProxyPort: 18991, SandboxPort: 8990, Mode: "proxy"}, true},
		{"bad mode", File{Provider: "deepseek", ProxyPort: 18991, SandboxPort: 8990, Mode: "hybrid"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(c.f)
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() err=%v wantErr=%v", err, c.wantErr)
			}
		})
	}
}

func TestUpdateRejectsInvalidPorts(t *testing.T) {
	dir := t.TempDir()
	if _, err := Update(dir, func(c *File) {
		c.ProxyPort = 8765
	}); err == nil {
		t.Fatal("expected error for reserved port")
	}
}