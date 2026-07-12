package quota

import (
	"context"
	"errors"
	"fmt"
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
	w, writable := s.Store.(artifact.Writer)
	d, deletable := s.Store.(artifact.Deleter)
	if !writable || !deletable {
		return errors.New("artifact store must support durable persist and compensation")
	}
	a := Artifact{RunID: r.RunID, IdempotencyKey: r.ID + ":artifact", Owner: r.Owner, Bytes: int64(len(b))}
	if err := s.Quota.ReserveArtifact(ctx, a); err != nil {
		return err
	}
	if err := w.Persist(ctx, r, b); err != nil {
		_ = s.Quota.ReleaseArtifact(context.Background(), a)
		return err
	}
	if err := s.Quota.CommitArtifact(ctx, a); err != nil {
		if deleteErr := d.Delete(context.Background(), r.Owner, r); deleteErr != nil {
			return fmt.Errorf("commit artifact quota: %w; compensation failed: %v", err, deleteErr)
		}
		if releaseErr := s.Quota.ReleaseArtifact(context.Background(), a); releaseErr != nil {
			return fmt.Errorf("commit artifact quota: %w; release reservation: %v", err, releaseErr)
		}
		return err
	}
	return nil
}
