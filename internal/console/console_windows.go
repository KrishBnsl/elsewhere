//go:build windows

// Package console adapts the terminal or console the player runs in.
package console

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// TerminationSignals end the player cleanly.
func TerminationSignals() []os.Signal { return []os.Signal{os.Interrupt, syscall.SIGTERM} }

// Prepare turns on escape sequences (colours, cursor moves, images) and UTF-8 output
// in the Windows console, and returns how to put things back.
func Prepare() (func(), error) {
	out := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(out, &mode); err != nil {
		return nil, err
	}
	if err := windows.SetConsoleMode(out, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.DISABLE_NEWLINE_AUTO_RETURN); err != nil {
		return nil, err
	}
	codePage, _ := windows.GetConsoleOutputCP()
	windows.SetConsoleOutputCP(65001) // UTF-8
	return func() {
		windows.SetConsoleMode(out, mode)
		if codePage != 0 {
			windows.SetConsoleOutputCP(codePage)
		}
	}, nil
}
