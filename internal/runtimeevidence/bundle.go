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
	run, err := s.Runs.GetOwned(o, id)
	if err != nil {
		return nil, err
	}
	events, err := s.Runs.EventsOwned(o, id, 0)
	if err != nil {
		return nil, err
	}
	var approvals []approvalstate.Approval
	if s.Approvals != nil {
		approvals, err = s.Approvals.ListRun(o, id)
		if err != nil {
			return nil, err
		}
	}
	var arts []artifact.Record
	if s.Artifacts != nil {
		arts, err = s.Artifacts.ListRun(o, id)
		if err != nil {
			return nil, err
		}
	}
	var uses []usage.Record
	if s.Usage != nil {
		uses, err = s.Usage.ListRun(o, id)
		if err != nil {
			return nil, err
		}
	}
	files := map[string][]byte{}
	files["run.json"] = pretty(run)
	files["events.jsonl"] = jsonLines(redactEvents(events))
	files["approvals.json"] = pretty(approvals)
	files["verification.json"] = pretty(extract(events, "verification"))
	files["provenance.jsonl"] = jsonLines(extract(events, "provenance"))
	files["usage.json"] = pretty(uses)
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
	files["manifest.json"] = pretty(map[string]any{"schema_version": 1, "run_id": id, "owner": o, "artifacts": manifestArts, "generated_at": time.Now().UTC()})
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var sums strings.Builder
	for _, n := range names {
		h := sha256.Sum256(files[n])
		fmt.Fprintf(&sums, "%x  %s\n", h, n)
	}
	files["SHA256SUMS"] = []byte(sums.String())
	names = append(names, "SHA256SUMS")
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, n := range names {
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
			if strings.Contains(lk, "reasoning") || strings.Contains(lk, "api_key") || strings.Contains(lk, "secret") || strings.Contains(lk, "token") {
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
func extract(events []event.Event, kind string) []any {
	var out []any
	for _, e := range events {
		if strings.Contains(strings.ToLower(string(e.Kind)), kind) || strings.Contains(strings.ToLower(e.Text), kind) {
			out = append(out, redactEvents([]event.Event{e})[0])
		}
	}
	return out
}
