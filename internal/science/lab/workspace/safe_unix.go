//go:build !windows

package workspace

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func (g *Guard) openDir(rel string, create bool) (int, error) {
	fd, err := unix.Open(g.root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	if rel == "" || rel == "." {
		return fd, nil
	}
	parts, err := safeParts(rel)
	if err != nil {
		unix.Close(fd)
		return -1, err
	}
	for _, part := range parts {
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if e == unix.ENOENT && create {
			if e = unix.Mkdirat(fd, part, 0o700); e == nil || e == unix.EEXIST {
				next, e = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			}
		}
		unix.Close(fd)
		if e != nil {
			return -1, e
		}
		fd = next
	}
	return fd, nil
}

func (g *Guard) parent(rel string, create bool) (int, string, error) {
	parts, err := safeParts(rel)
	if err != nil {
		return -1, "", err
	}
	dir := "."
	if len(parts) > 1 {
		dir = filepath.Join(parts[:len(parts)-1]...)
	}
	fd, err := g.openDir(dir, create)
	return fd, parts[len(parts)-1], err
}

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

func (g *Guard) ReadFile(rel string) ([]byte, error) {
	f, e := g.OpenFile(rel, os.O_RDONLY, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return io.ReadAll(f)
}
func (g *Guard) MkdirAll(rel string, _ os.FileMode) error {
	fd, e := g.openDir(rel, true)
	if e == nil {
		e = unix.Close(fd)
	}
	return e
}
func (g *Guard) ReadDir(rel string) ([]os.DirEntry, error) {
	fd, e := g.openDir(rel, false)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), "workspace")
	defer f.Close()
	return f.ReadDir(-1)
}
func (g *Guard) Stat(rel string) (os.FileInfo, error) {
	if rel == "" || rel == "." {
		fd, err := g.openDir(".", false)
		if err != nil {
			return nil, err
		}
		f := os.NewFile(uintptr(fd), "workspace")
		defer f.Close()
		return f.Stat()
	}
	f, e := g.OpenFile(rel, os.O_RDONLY, 0)
	if e != nil {
		fd, n, pe := g.parent(rel, false)
		if pe != nil {
			return nil, e
		}
		defer unix.Close(fd)
		var st unix.Stat_t
		if pe = unix.Fstatat(fd, n, &st, unix.AT_SYMLINK_NOFOLLOW); pe != nil {
			return nil, pe
		}
		if st.Mode&unix.S_IFMT == unix.S_IFLNK {
			return nil, fmt.Errorf("symlink not allowed")
		}
		d, e2 := unix.Openat(fd, n, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if e2 != nil {
			return nil, e
		}
		f = os.NewFile(uintptr(d), n)
	}
	defer f.Close()
	return f.Stat()
}
func (g *Guard) Rename(oldRel, newRel string) error {
	of, on, e := g.parent(oldRel, false)
	if e != nil {
		return e
	}
	defer unix.Close(of)
	nf, nn, e := g.parent(newRel, true)
	if e != nil {
		return e
	}
	defer unix.Close(nf)
	var st unix.Stat_t
	if e = unix.Fstatat(of, on, &st, unix.AT_SYMLINK_NOFOLLOW); e != nil {
		return e
	}
	if st.Mode&unix.S_IFMT == unix.S_IFLNK {
		return fmt.Errorf("symlink not allowed")
	}
	if e = unix.Fstatat(nf, nn, &st, unix.AT_SYMLINK_NOFOLLOW); e == nil {
		return fs.ErrExist
	}
	return unix.Renameat(of, on, nf, nn)
}
func (g *Guard) Copy(src, dst string) error {
	st, e := g.Stat(src)
	if e != nil {
		return e
	}
	if st.IsDir() {
		if e = g.MkdirAll(dst, 0700); e != nil {
			return e
		}
		es, e := g.ReadDir(src)
		if e != nil {
			return e
		}
		for _, x := range es {
			if x.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("symlink not allowed")
			}
			if e = g.Copy(filepath.Join(src, x.Name()), filepath.Join(dst, x.Name())); e != nil {
				return e
			}
		}
		return nil
	}
	b, e := g.ReadFile(src)
	if e != nil {
		return e
	}
	return g.WriteFile(dst, b, st.Mode().Perm())
}
func (g *Guard) RemoveAll(rel string) error {
	fd, n, e := g.parent(rel, false)
	if e != nil {
		return e
	}
	defer unix.Close(fd)
	return removeAt(fd, n)
}
func removeAt(parent int, name string) error {
	var st unix.Stat_t
	if e := unix.Fstatat(parent, name, &st, unix.AT_SYMLINK_NOFOLLOW); e != nil {
		if e == unix.ENOENT {
			return nil
		}
		return e
	}
	if st.Mode&unix.S_IFMT == unix.S_IFLNK {
		return fmt.Errorf("symlink not allowed")
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return unix.Unlinkat(parent, name, 0)
	}
	fd, e := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return e
	}
	f := os.NewFile(uintptr(fd), name)
	entries, e := f.ReadDir(-1)
	if e != nil {
		f.Close()
		return e
	}
	for _, x := range entries {
		if e = removeAt(fd, x.Name()); e != nil {
			f.Close()
			return e
		}
	}
	f.Close()
	return unix.Unlinkat(parent, name, unix.AT_REMOVEDIR)
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
