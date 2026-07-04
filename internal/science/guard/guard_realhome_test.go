package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRealScienceDirUsesScienceRealHome(t *testing.T) {
	t.Setenv("SCIENCE_REAL_HOME", "/tmp/real-user-home")
	got, err := RealScienceDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/tmp/real-user-home", ".claude-science")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	t.Setenv("SCIENCE_REAL_HOME", "")
	if _, err := RealScienceDir(); err != nil {
		t.Fatal(err)
	}
	_ = os.UserHomeDir()
}