//go:build !windows

package fileutil

import (
	"os"
	"syscall"
)

func init() {
	processUmask = func() os.FileMode {
		old := syscall.Umask(0)
		syscall.Umask(old)
		return os.FileMode(old)
	}()
}
