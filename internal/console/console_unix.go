//go:build !windows

// Package console adapts the terminal or console the player runs in.
package console

import (
	"os"
	"syscall"
)

// TerminationSignals end the player cleanly.
func TerminationSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}

// Prepare has nothing to do on Unix terminals. It returns how to put things back.
func Prepare() (func(), error) { return func() {}, nil }
