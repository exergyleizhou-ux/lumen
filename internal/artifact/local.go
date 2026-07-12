package artifact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type LocalBackend struct{ root string }

func NewLocalBackend(root string) (*LocalBackend, error) {
	if root == "" {
		return nil, errors.New("artifact object directory required")
	}
	if err := os.MkdirAll(filepath.Join(root, "objects"), 0700); err != nil {
		return nil, err
	}
	return &LocalBackend{root: root}, nil
}
func (l *LocalBackend) path(key string) (string, error) {
	if key == "" || strings.HasPrefix(key, "/") {
		return "", errors.New("invalid object key")
	}
	for _, p := range strings.Split(key, "/") {
		if p == "" || p == ".." || p == "." {
			return "", errors.New("invalid object key")
		}
	}
	return filepath.Join(l.root, "objects", filepath.FromSlash(key)), nil
}
func (l *LocalBackend) Put(ctx context.Context, key string, r io.Reader, _ int64, _ string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".upload-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	_, err = io.Copy(tmp, &contextReader{ctx: ctx, r: r})
	cerr := tmp.Close()
	if err != nil {
		return err
	}
	if cerr != nil {
		return cerr
	}
	if err = os.Rename(name, p); err != nil {
		return fmt.Errorf("commit object: %w", err)
	}
	return nil
}
func (l *LocalBackend) Get(_ context.Context, key string) (io.ReadCloser, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.r.Read(p)
	}
}
