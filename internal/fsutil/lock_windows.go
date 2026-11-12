//go:build windows

package fsutil

import (
	"os"

	"golang.org/x/sys/windows"
)

// LockFile takes an exclusive lock on f without waiting.
func LockFile(f *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
}
