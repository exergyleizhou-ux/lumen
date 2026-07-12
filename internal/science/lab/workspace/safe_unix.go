//go:build !windows

package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func (g *Guard) OpenFile(rel string, flags int, perm os.FileMode) (*os.File, error) {
	parts, err := safeParts(rel)
	if err != nil {
		return nil, err
	}
	dir, err := unix.Open(g.root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range parts[:len(parts)-1] {
		next, e := unix.Openat(dir, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if e == unix.ENOENT && flags&os.O_CREATE != 0 {
			if e = unix.Mkdirat(dir, part, 0o700); e == nil || e == unix.EEXIST {
				next, e = unix.Openat(dir, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			}
		}
		unix.Close(dir)
		if e != nil {
			return nil, fmt.Errorf("open parent %s: %w", part, e)
		}
		dir = next
	}
	fd, e := unix.Openat(dir, parts[len(parts)-1], flags|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(perm.Perm()))
	unix.Close(dir)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(fd), parts[len(parts)-1]), nil
}

func (g *Guard) WriteFile(rel string, data []byte, perm os.FileMode) error {
	f, e := g.OpenFile(rel, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if e != nil {
		return e
	}
	_, e = f.Write(data)
	ce := f.Close()
	if e != nil {
		return e
	}
	return ce
}

func safeParts(rel string) ([]string, error) {
	if filepath.IsAbs(rel) {
		return nil, fmt.Errorf("absolute paths not allowed")
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("path escapes workspace")
	}
	return strings.Split(clean, string(filepath.Separator)), nil
}
