package main

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestConfirmApproverYesNo(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"n\n", false},
		{"no\n", false},
		{"\n", false}, // bare enter → default No (safe)
		{"", false},   // EOF → default No
		{"garbage\n", false},
	}
	for _, tc := range cases {
		approve := newConfirmApprover(strings.NewReader(tc.in), io.Discard)
		got, _, err := approve(context.Background(), "bash", json.RawMessage(`{"command":"rm -rf /"}`))
		if err != nil {
			t.Fatalf("input %q: unexpected error %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("input %q: got %v, want %v", tc.in, got, tc.want)
		}
	}
}
