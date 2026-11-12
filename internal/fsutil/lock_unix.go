//go:build !windows

package fsutil

import (
	"os"
	"syscall"
)

// LockFile takes an exclusive lock on f without waiting.
func LockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
