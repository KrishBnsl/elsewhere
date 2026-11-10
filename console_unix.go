//go:build !windows

package main

import (
	"os"
	"syscall"
)

// terminationSignals end the player cleanly.
func terminationSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}

// prepareConsole has nothing to do on Unix terminals.
func prepareConsole() (func(), error) { return func() {}, nil }

// lockFile takes an exclusive lock on f without waiting.
func lockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
