//go:build darwin || (linux && cgo)

package radio

/*
#cgo linux LDFLAGS: -ldl
#include "decoder.h"
*/
import "C"

import (
	"context"
	"encoding/binary"
	"errors"
	"math/rand/v2"
	"unsafe"
)

const radioRate = 32000

// DecoderAvailable reports whether Game Music Emu can be loaded.
func DecoderAvailable() bool {
	return C.radio_decoder_available() != 0
}

func renderRadioTrack(ctx context.Context, data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty chiptune file")
	}
	// Game Music Emu copies input in gme_open_data.
	duration := C.int(90000)
	decoder := C.radio_decoder_open(unsafe.Pointer(&data[0]), C.size_t(len(data)), radioRate, C.uint(rand.Uint32()), &duration)
	if decoder == nil {
		return nil, errors.New("unsupported chiptune file")
	}
	defer C.radio_decoder_close(decoder)

	milliseconds := int(duration)
	if milliseconds <= 0 {
		milliseconds = 90000
	}
	frames := min(milliseconds, 150000) * radioRate / 1000
	wav := make([]byte, 44+frames*4)
	copy(wav, "RIFF")
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 2)
	binary.LittleEndian.PutUint32(wav[24:], radioRate)
	binary.LittleEndian.PutUint32(wav[28:], radioRate*4)
	binary.LittleEndian.PutUint16(wav[32:], 4)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")

	const fade = radioRate * 2
	var samples [8192]C.short
	frame := 0
	for frame < frames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		count := min(len(samples)/2, frames-frame)
		if C.radio_decoder_play(decoder, C.int(count*2), &samples[0]) == 0 {
			return nil, errors.New("could not render chiptune file")
		}
		for i, sample := range samples[:count*2] {
			remaining := frames - frame - i/2
			if remaining < fade {
				sample = C.short(int(sample) * remaining / fade)
			}
			binary.LittleEndian.PutUint16(wav[44+frame*4+i*2:], uint16(sample))
		}
		frame += count
		if C.radio_decoder_ended(decoder) != 0 {
			break
		}
	}
	if frame < radioRate*5 {
		return nil, errors.New("chiptune is too short for radio")
	}
	wav = wav[:44+frame*4]
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	binary.LittleEndian.PutUint32(wav[40:], uint32(frame*4))
	return wav, nil
}

// ChiptuneSupported: this build can load Game Music Emu when it's installed.
const ChiptuneSupported = true
