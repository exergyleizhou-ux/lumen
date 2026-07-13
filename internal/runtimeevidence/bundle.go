// Package runtimeevidence exports bounded, hashed audit bundles for Code and Lab.
package runtimeevidence

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"lumen/internal/approvalstate"
	"lumen/internal/artifact"
	"lumen/internal/event"
	"lumen/internal/runstate"
	"lumen/internal/usage"
	"sort"
	"strings"
	"time"
)

const DefaultMaxBytes int64 = 100 << 20

type UsageReader interface {
	ListRun(runstate.Owner, string) ([]usage.Record, error)
}
type Service struct {
	Runs      *runstate.Manager
	Approvals approvalstate.Store
	Artifacts artifact.Store
	Usage     UsageReader
	MaxBytes  int64
	Timeout   time.Duration
}

func (s Service) Build(parent context.Context, o runstate.Owner, id string) ([]byte, error) {
	if !o.Valid() {
		return nil, runstate.ErrRunNotFound
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	run, err := bounded(ctx, func() (runstate.Run, error) { return s.Runs.GetOwned(o, id) })
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	events, err := bounded(ctx, func() ([]event.Event, error) { return s.Runs.EventsOwned(o, id, 0) })
	if err != nil {
		return nil, err
	}
	var approvals []approvalstate.Approval
	if s.Approvals != nil {
		approvals, err = bounded(ctx, func() ([]approvalstate.Approval, error) { return s.Approvals.ListRun(o, id) })
		if err != nil {
			return nil, err
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	var arts []artifact.Record
	if s.Artifacts != nil {
		arts, err = bounded(ctx, func() ([]artifact.Record, error) { return s.Artifacts.ListRun(o, id) })
		if err != nil {
			return nil, err
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	var uses []usage.Record
	if s.Usage != nil {
		uses, err = bounded(ctx, func() ([]usage.Record, error) { return s.Usage.ListRun(o, id) })
		if err != nil {
			return nil, err
		}
	}
	files := map[string][]byte{}
	files["run.json"] = pretty(redactJSON(run))
	files["events.jsonl"] = jsonLines(redactEvents(events))
	files["approvals.json"] = pretty(redactJSON(approvals))
	files["verification.json"] = pretty(structuredVerification(events))
	files["provenance.jsonl"] = jsonLines(structuredProvenance(arts))
	files["usage.json"] = pretty(redactJSON(uses))
	manifestArts := make([]artifact.Record, 0, len(arts))
	max := s.MaxBytes
	if max <= 0 {
		max = DefaultMaxBytes
	}
	var total int64
	for _, a := range arts {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		rc, e := s.Artifacts.Open(ctx, o, a)
		if e != nil {
			return nil, e
		}
		b, e := io.ReadAll(io.LimitReader(rc, max-total+1))
		rc.Close()
		if e != nil {
			return nil, e
		}
		total += int64(len(b))
		if total > max {
			return nil, errors.New("evidence bundle exceeds size limit")
		}
		sum := sha256.Sum256(b)
		if a.SHA256 != "" && a.SHA256 != hex.EncodeToString(sum[:]) {
			return nil, fmt.Errorf("artifact checksum mismatch: %s", a.ID)
		}
		name := "artifacts/" + artifact.SafeName(a.Name)
		if _, exists := files[name]; exists {
			name = "artifacts/" + artifact.SafeName(a.ID+"-"+a.Name)
		}
		files[name] = b
		manifestArts = append(manifestArts, a)
	}
	files["manifest.json"] = pretty(redactJSON(map[string]any{"schema_version": 1, "run_id": id, "owner": o, "artifacts": manifestArts, "generated_at": time.Now().UTC()}))
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var sums strings.Builder
	for _, n := range names {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		h := sha256.Sum256(files[n])
		fmt.Fprintf(&sums, "%x  %s\n", h, n)
	}
	files["SHA256SUMS"] = []byte(sums.String())
	names = append(names, "SHA256SUMS")
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, n := range names {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		w, e := zw.Create(n)
		if e != nil {
			return nil, e
		}
		if _, e = w.Write(files[n]); e != nil {
			return nil, e
		}
	}
	if err = zw.Close(); err != nil {
		return nil, err
	}
	if int64(out.Len()) > max {
		return nil, errors.New("evidence bundle exceeds size limit")
	}
	return out.Bytes(), nil
}
func pretty(v any) []byte { b, _ := json.MarshalIndent(v, "", "  "); return append(b, '\n') }
func jsonLines(v any) []byte {
	items, _ := json.Marshal(v)
	var list []any
	if json.Unmarshal(items, &list) != nil {
		return nil
	}
	var b bytes.Buffer
	for _, x := range list {
		line, _ := json.Marshal(x)
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.Bytes()
}
func redactEvents(in []event.Event) []any {
	out := make([]any, 0, len(in))
	for _, e := range in {
		raw, _ := json.Marshal(e)
		var v any
		_ = json.Unmarshal(raw, &v)
		out = append(out, redact(v))
	}
	return out
}
func redact(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			lk := strings.ToLower(k)
			if strings.Contains(lk, "reason") || strings.Contains(lk, "args") || strings.Contains(lk, "command") || strings.Contains(lk, "reasoning") || strings.Contains(lk, "api_key") || strings.Contains(lk, "secret") || credentialTokenKey(lk) {
				x[k] = "[REDACTED]"
			} else {
				x[k] = redact(val)
			}
		}
	case []any:
		for i := range x {
			x[i] = redact(x[i])
		}
	case string:
		var nested any
		if json.Unmarshal([]byte(x), &nested) == nil {
			if b, err := json.Marshal(redact(nested)); err == nil {
				return string(b)
			}
		}
	}
	return v
}
func credentialTokenKey(k string) bool {
	switch k {
	case "token", "access_token", "refresh_token", "auth_token", "bearer_token", "api_token", "authorization":
		return true
	}
	return strings.HasSuffix(k, "_credential")
}
func redactJSON(v any) any {
	raw, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(raw, &out)
	return redact(out)
}

type boundedResult[T any] struct {
	v   T
	err error
}

func bounded[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	ch := make(chan boundedResult[T], 1)
	go func() { v, e := fn(); ch <- boundedResult[T]{v, e} }()
	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case r := <-ch:
		return r.v, r.err
	}
}
func extract(events []event.Event, kind string) []any {
	var out []any
	for _, e := range events {
		if strings.Contains(strings.ToLower(string(e.Kind)), kind) || strings.Contains(strings.ToLower(e.Text), kind) {
			out = append(out, redactEvents([]event.Event{e})[0])
		}
	}
	return out
}
func structuredVerification(events []event.Event) []any {
	var out []any
	for _, e := range events {
		if e.Kind == event.VerifyStarted || e.Kind == event.VerifyResult {
			out = append(out, redactEvents([]event.Event{e})[0])
		}
	}
	return out
}
func structuredProvenance(arts []artifact.Record) []any {
	out := make([]any, 0, len(arts))
	for _, a := range arts {
		out = append(out, redactJSON(map[string]any{"artifact_id": a.ID, "run_id": a.RunID, "step_id": a.StepID, "tool_call_id": a.ToolCallID, "sha256": a.SHA256, "input_refs": a.InputRefs, "provenance": a.Provenance}))
	}
	return out
}
