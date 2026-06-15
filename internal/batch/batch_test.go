package batch

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestMapProcessor(t *testing.T) {
	r := NewRunner[int, string](DefaultBatchConfig())
	out, err := r.Run(context.Background(), []int{1, 2, 3}, NewMapProcessor(func(i int) string { return fmt.Sprintf("x%d", i) }))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Error("count")
	}
	if !strings.Contains(r.FormatProgress(), "100.0%") {
		t.Error("progress")
	}
}
func TestFilterProcessor(t *testing.T) {
	r := NewRunner[int, int](DefaultBatchConfig())
	out, _ := r.Run(context.Background(), []int{1, 2, 3, 4, 5}, NewFilterProcessor(func(i int) bool { return i > 3 }))
	if len(out) != 2 {
		t.Error("filter")
	}
}
func TestChunking(t *testing.T) {
	cfg := DefaultBatchConfig()
	cfg.ChunkSize = 2
	r := NewRunner[int, int](cfg)
	out, _ := r.Run(context.Background(), []int{1, 2, 3, 4, 5}, NewMapProcessor(func(i int) int { return i * 2 }))
	if len(out) != 5 {
		t.Error("chunking")
	}
}
func TestProgress(t *testing.T) {
	r := NewRunner[int, int](DefaultBatchConfig())
	r.Run(context.Background(), []int{1, 2, 3}, NewMapProcessor(func(i int) int { return i }))
	p := r.Progress()
	if !p.Done {
		t.Error("should be done")
	}
}
