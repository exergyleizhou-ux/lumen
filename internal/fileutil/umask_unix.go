//go:build !windows

package fileutil

import "syscall"

var processUmask = func() os.FileMode {
	old := syscall.Umask(0)
	syscall.Umask(old)
	return os.FileMode(old)
}()
