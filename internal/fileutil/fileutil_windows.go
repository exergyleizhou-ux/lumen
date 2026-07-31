//go:build windows

package fileutil

import "os"

var processUmask = func() os.FileMode {
	return 0 // Windows 不支持 umask
}()