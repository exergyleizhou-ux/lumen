package main

import "testing"

func TestParseCheckArgs(t *testing.T) {
	cases := []struct {
		name         string
		rest         []string
		wantDir      string
		wantDataPath string
	}{
		{"no args defaults to cwd", nil, ".", ""},
		{"positional dir only", []string{"mydir"}, "mydir", ""},
		{"--data with space", []string{"--data", "sample.csv"}, ".", "sample.csv"},
		{"--data= form", []string{"--data=sample.csv"}, ".", "sample.csv"},
		{"dir then --data", []string{"mydir", "--data", "s.csv"}, "mydir", "s.csv"},
		{"--data before positional dir", []string{"--data", "s.csv", "mydir"}, "mydir", "s.csv"},
		{"dangling --data has no value", []string{"--data"}, ".", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, dataPath := parseCheckArgs(c.rest)
			if dir != c.wantDir {
				t.Errorf("dir = %q, want %q", dir, c.wantDir)
			}
			if dataPath != c.wantDataPath {
				t.Errorf("dataPath = %q, want %q", dataPath, c.wantDataPath)
			}
		})
	}
}
