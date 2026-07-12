//go:build windows

package workspace

import "os"

func (g *Guard) OpenFile(rel string, flags int, perm os.FileMode) (*os.File, error) {
	p, e := g.Resolve(rel)
	if e != nil {
		return nil, e
	}
	return os.OpenFile(p, flags, perm)
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
	p, e := g.Resolve(rel)
	if e != nil {
		return nil, e
	}
	return os.ReadFile(p)
}
func (g *Guard) MkdirAll(rel string, p os.FileMode) error {
	x, e := g.Resolve(rel)
	if e != nil {
		return e
	}
	return os.MkdirAll(x, p)
}
func (g *Guard) ReadDir(rel string) ([]os.DirEntry, error) {
	x, e := g.Resolve(rel)
	if e != nil {
		return nil, e
	}
	return os.ReadDir(x)
}
func (g *Guard) Stat(rel string) (os.FileInfo, error) {
	x, e := g.Resolve(rel)
	if e != nil {
		return nil, e
	}
	return os.Stat(x)
}
func (g *Guard) Rename(a, b string) error {
	x, e := g.Resolve(a)
	if e != nil {
		return e
	}
	y, e := g.Resolve(b)
	if e != nil {
		return e
	}
	return os.Rename(x, y)
}

func (g *Guard) Replace(a, b string) error {
	x, e := g.Resolve(a)
	if e != nil {
		return e
	}
	y, e := g.Resolve(b)
	if e != nil {
		return e
	}
	_ = os.Remove(y)
	return os.Rename(x, y)
}
func (g *Guard) Copy(a, b string) error {
	data, e := g.ReadFile(a)
	if e != nil {
		return e
	}
	return g.WriteFile(b, data, 0600)
}
func (g *Guard) RemoveAll(rel string) error {
	x, e := g.Resolve(rel)
	if e != nil {
		return e
	}
	return os.RemoveAll(x)
}
