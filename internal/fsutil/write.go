// Package fsutil holds small file helpers shared by the player and its subsystems.
package fsutil

import (
	"os"
	"path/filepath"
)

// WriteFile writes data to filename atomically, creating its folder (0700) if needed.
func WriteFile(filename string, data []byte) error {
	folder := filepath.Dir(filename)
	if err := os.MkdirAll(folder, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(folder, ".elsewhere-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), filename)
}
