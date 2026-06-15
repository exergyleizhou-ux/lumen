package deadletter

import (
	"fmt"
	"testing"
	"time"
)

func TestQueue_EnqueueAndGet(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxMessages = 100
	q := New(cfg)
	defer q.Close()

	msg := &Message{
		ID:      "msg-1",
		Subject: "orders",
		Payload: []byte(`{"order": 123}`),
		Headers: map[string]string{"x-trace": "abc"},
	}
	if err := q.Enqueue(msg); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	got, ok := q.Get("msg-1")
	if !ok {
		t.Fatal("expected to find message")
	}
	if got.Subject != "orders" {
		t.Fatalf("expected subject 'orders', got %q", got.Subject)
	}
	if got.Partition == "" {
		t.Fatal("expected partition to be set")
	}
}

func TestQueue_Replay(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxMessages = 100
	q := New(cfg)
	defer q.Close()

	// Enqueue 3 messages.
	for i := 0; i < 3; i++ {
		q.Enqueue(&Message{
			ID:      fmt.Sprintf("r%d", i),
			Subject: "emails",
			Payload: []byte("email content"),
		})
	}

	replayed := 0
	n, err := q.Replay("emails", func(msg *Message) error {
		replayed++
		return nil
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 replayed, got %d", n)
	}
	if replayed != 3 {
		t.Fatalf("expected 3 callbacks, got %d", replayed)
	}

	// After successful replay, messages should be removed.
	if q.Size() != 0 {
		t.Fatalf("expected 0 messages after replay, got %d", q.Size())
	}
}

func TestQueue_Expiry(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxMessages = 100
	q := New(cfg)
	defer q.Close()

	msg := &Message{
		ID:        "expiring",
		Subject:   "test",
		Payload:   []byte("data"),
		ExpiresAt: time.Now().Add(50 * time.Millisecond),
	}
	q.Enqueue(msg)

	// Should be available now.
	if _, ok := q.Get("expiring"); !ok {
		t.Fatal("expected to find message before expiry")
	}

	// Wait for expiry.
	time.Sleep(100 * time.Millisecond)

	// Should be gone.
	if _, ok := q.Get("expiring"); ok {
		t.Fatal("expected message to be expired")
	}
}

func TestQueue_SubjectsAndPartitions(t *testing.T) {
	cfg := DefaultConfig()
	q := New(cfg)
	defer q.Close()

	q.Enqueue(&Message{ID: "a", Subject: "foo", Payload: []byte("1")})
	q.Enqueue(&Message{ID: "b", Subject: "bar", Payload: []byte("2")})
	q.Enqueue(&Message{ID: "c", Subject: "foo", Payload: []byte("3")})

	subjects := q.Subjects()
	if len(subjects) != 2 {
		t.Fatalf("expected 2 subjects, got %d", len(subjects))
	}

	foos := q.ListBySubject("foo")
	if len(foos) != 2 {
		t.Fatalf("expected 2 foo messages, got %d", len(foos))
	}

	bars := q.ListBySubject("bar")
	if len(bars) != 1 {
		t.Fatalf("expected 1 bar message, got %d", len(bars))
	}
}

func TestQueue_FormatQueue(t *testing.T) {
	cfg := DefaultConfig()
	q := New(cfg)
	defer q.Close()

	q.Enqueue(&Message{ID: "m1", Subject: "test", Payload: []byte("x")})

	s := q.FormatQueue()
	if s == "" {
		t.Fatal("expected non-empty format")
	}
	if len(s) < 10 {
		t.Fatalf("format too short: %s", s)
	}
}

func TestQueue_Purge(t *testing.T) {
	cfg := DefaultConfig()
	q := New(cfg)
	defer q.Close()

	for i := 0; i < 5; i++ {
		q.Enqueue(&Message{ID: fmt.Sprintf("p%d", i), Subject: "x", Payload: []byte("y")})
	}
	if q.Size() != 5 {
		t.Fatal("expected 5 messages")
	}

	n := q.Purge()
	if n != 5 {
		t.Fatalf("expected purge to return 5, got %d", n)
	}
	if q.Size() != 0 {
		t.Fatal("expected 0 messages after purge")
	}
}
