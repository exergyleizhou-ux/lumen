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
