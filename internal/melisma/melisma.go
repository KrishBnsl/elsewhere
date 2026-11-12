// Package melisma finds the Melisma engine (`dhunn`) and gives it somewhere to log.
package melisma

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"elsewhere/internal/update"
)

// Command prefers the engine installed beside Elsewhere after checking the override.
func Command() (string, error) {
	if path := os.Getenv("MELISMA_ENGINE"); path != "" {
		if path == "off" {
			return "", errors.New("turned off")
		}
		return path, nil
	}
	if current, err := update.ExecutablePath(); err == nil {
		// LookPath also supplies the .exe suffix on Windows.
		if bundled, err := exec.LookPath(filepath.Join(filepath.Dir(current), "dhunn")); err == nil {
			return bundled, nil
		}
	}
	path, err := exec.LookPath("dhunn")
	if err != nil {
		return "", errors.New("dhunn isn't installed")
	}
	return path, nil
}

// LogFile is where the engine's own messages go, never over the player's screen.
func LogFile(name string) *os.File {
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil
	}
	dir = filepath.Join(dir, "Elsewhere")
	if os.MkdirAll(dir, 0700) != nil {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return nil
	}
	return f
}
