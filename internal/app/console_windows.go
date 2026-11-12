//go:build windows

package app

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// terminationSignals end the player cleanly.
func terminationSignals() []os.Signal { return []os.Signal{os.Interrupt, syscall.SIGTERM} }

// prepareConsole turns on escape sequences (colours, cursor moves, images) and UTF-8 output
// in the Windows console, and returns how to put things back.
func prepareConsole() (func(), error) {
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

// lockFile takes an exclusive lock on f without waiting.
func lockFile(f *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
}
