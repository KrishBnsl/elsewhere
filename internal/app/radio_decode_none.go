//go:build !darwin && !(linux && cgo)

package app

import (
	"context"
	"errors"
)

// Chiptune Radio needs Game Music Emu, loaded through cgo: not available in this build.
func radioDecoderAvailable() bool { return false }

func renderRadioTrack(context.Context, []byte) ([]byte, error) {
	return nil, errors.New("Chiptune Radio isn't available on this system")
}

const chiptuneSupported = false

const gmeInstallHint = "not available on this system"
