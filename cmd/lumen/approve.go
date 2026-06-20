package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// newConfirmApprover returns an interactive permission approver: it prints what
// the agent wants to run and reads a y/N line. Anything other than an explicit
// yes (default No) denies — the safe default for a destructive prompt. Reading
// from injectable in/out makes it unit-testable without a TTY.
func newConfirmApprover(in io.Reader, out io.Writer) func(ctx context.Context, toolName string, args json.RawMessage) (bool, error) {
	r := bufio.NewReader(in)
	return func(ctx context.Context, toolName string, args json.RawMessage) (bool, error) {
		fmt.Fprintf(out, "\n⚠️  Allow %s%s? [y/N] ", toolName, approvalDetail(toolName, args))
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			return false, nil // EOF / no input → deny (safe default)
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return true, nil
		default:
			return false, nil
		}
	}
}

// approvalDetail surfaces the most decision-relevant arg (the bash command, a
// file path) so the user isn't approving blind.
func approvalDetail(toolName string, args json.RawMessage) string {
	var p struct {
		Command string `json:"command"`
		Path    string `json:"path"`
	}
	if json.Unmarshal(args, &p) == nil {
		switch {
		case p.Command != "":
			return ": " + truncate(p.Command, 120)
		case p.Path != "":
			return " " + p.Path
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
