//go:build !darwin && !(linux && cgo)

package radio

import (
	"context"
	"errors"
)

// Chiptune Radio needs Game Music Emu, loaded through cgo: not available in this build.
func DecoderAvailable() bool { return false }

func renderRadioTrack(context.Context, []byte) ([]byte, error) {
	return nil, errors.New("Chiptune Radio isn't available on this system")
}

// ChiptuneSupported is false when this build cannot load Game Music Emu.
const ChiptuneSupported = false

// InstallHint says how to get Game Music Emu.
const InstallHint = "not available on this system"
