//go:build windows

package fileutil

import "os"

func init() {
	processUmask = 0
}

// UmaskedPerm returns the given permission with process umask applied.
// On Windows, umask is not applicable — return as-is.
func UmaskedPerm(mode os.FileMode) os.FileMode {
	return mode
}
