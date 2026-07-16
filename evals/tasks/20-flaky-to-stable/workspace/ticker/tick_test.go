package ticker

import (
	"testing"
	"time"
)

func TestTickerFires(t *testing.T) {
	tk := NewTicker(10 * time.Millisecond)
	defer tk.Stop()

	count := 0
	timeout := time.After(200 * time.Millisecond)
loop:
	for count < 3 {
		select {
		case <-tk.C:
			count++
		case <-timeout:
			t.Fatalf("only got %d ticks before timeout", count)
			break loop
		}
	}
	if count < 3 {
		t.Fatalf("expected at least 3 ticks, got %d", count)
	}
}
