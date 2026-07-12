package quota

import (
	"context"
	"errors"
	"io"

	"lumen/internal/artifact"
	"lumen/internal/runstate"
)

// ArtifactStore reserves durable storage quota before object I/O and releases
// the reservation if persistence fails. Successful retries are idempotent.
type ArtifactStore struct {
	Store artifact.Store
	Quota Store
}

func (s ArtifactStore) Create(r artifact.Record) error {
	return errors.New("quota artifact store requires byte-aware Persist")
}
func (s ArtifactStore) ListRun(o runstate.Owner, id string) ([]artifact.Record, error) {
	return s.Store.ListRun(o, id)
}
func (s ArtifactStore) Open(ctx context.Context, o runstate.Owner, r artifact.Record) (io.ReadCloser, error) {
	return s.Store.Open(ctx, o, r)
}
func (s ArtifactStore) Persist(ctx context.Context, r artifact.Record, b []byte) error {
	a := Artifact{RunID: r.RunID, IdempotencyKey: r.ID + ":artifact", Owner: r.Owner, Bytes: int64(len(b))}
	if err := s.Quota.ReserveArtifact(ctx, a); err != nil {
		return err
	}
	w, ok := s.Store.(artifact.Writer)
	if !ok {
		_ = s.Quota.ReleaseArtifact(context.Background(), a)
		return errors.New("artifact store is not writable")
	}
	if err := w.Persist(ctx, r, b); err != nil {
		_ = s.Quota.ReleaseArtifact(context.Background(), a)
		return err
	}
	return nil
}
