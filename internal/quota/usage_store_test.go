package quota

import (
	"context"
	"errors"
	"testing"

	"lumen/internal/usage"
)

type usageStoreStub struct {
	err   error
	calls int
}

func (s *usageStoreStub) CreateUsage(usage.Record) error {
	s.calls++
	return s.err
}

type quotaStoreStub struct {
	MemoryStore
	recordErr error
	calls     int
}

func (s *quotaStoreStub) RecordUsage(context.Context, usage.Record) error {
	s.calls++
	return s.recordErr
}

func TestUsageStoreDoesNotDebitWhenCanonicalInsertFails(t *testing.T) {
	insertErr := errors.New("postgres unavailable")
	canonical := &usageStoreStub{err: insertErr}
	ledger := &quotaStoreStub{}
	store := UsageStore{Usage: canonical, Quota: ledger}

	if err := store.CreateUsage(usage.Record{RunID: "run", EventID: "event"}); !errors.Is(err, insertErr) {
		t.Fatalf("err=%v", err)
	}
	if ledger.calls != 0 {
		t.Fatalf("ledger calls=%d; failed insert must not debit", ledger.calls)
	}
}

func TestUsageStoreRetriesDebitAfterCommittedInsert(t *testing.T) {
	canonical := usage.NewMemoryStore()
	ledger := &quotaStoreStub{recordErr: errors.New("control plane unavailable")}
	store := UsageStore{Usage: canonical, Quota: ledger}
	r := usage.Record{RunID: "run", EventID: "event", UserID: "user", WorkspaceID: "workspace"}

	if err := store.CreateUsage(r); err == nil {
		t.Fatal("expected first debit failure")
	}
	if got := len(canonical.Records()); got != 1 {
		t.Fatalf("canonical records=%d", got)
	}
	ledger.recordErr = nil
	if err := store.CreateUsage(r); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := len(canonical.Records()); got != 1 {
		t.Fatalf("retry duplicated canonical record: %d", got)
	}
	if ledger.calls != 2 {
		t.Fatalf("ledger calls=%d", ledger.calls)
	}
}

func TestUsageStoreDuplicateStillSettlesIdempotentDebit(t *testing.T) {
	canonical := usage.NewMemoryStore()
	r := usage.Record{RunID: "run", EventID: "event", UserID: "user", WorkspaceID: "workspace"}
	if err := canonical.CreateUsage(r); err != nil {
		t.Fatal(err)
	}
	ledger := &quotaStoreStub{}
	if err := (UsageStore{Usage: canonical, Quota: ledger}).CreateUsage(r); err != nil {
		t.Fatal(err)
	}
	if ledger.calls != 1 || len(canonical.Records()) != 1 {
		t.Fatalf("ledger=%d records=%d", ledger.calls, len(canonical.Records()))
	}
}
