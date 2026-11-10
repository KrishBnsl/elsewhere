package main

/*
#cgo CFLAGS: -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework AVFoundation -framework Foundation -framework CoreAudio -framework Accelerate -framework ImageIO -framework CoreGraphics
#include <stdlib.h>
#include "audio.h"
*/
import "C"

import (
	"errors"
	"strings"
	"unsafe"
)

type playback struct {
	elapsed, duration float64
	playing           bool
}

func readNativeMetadata(path string) tags {
	filename := C.CString(path)
	defer C.free(unsafe.Pointer(filename))
	native := C.audio_metadata(filename)
	take := func(value *C.char) string {
		defer C.free(unsafe.Pointer(value))
		return strings.TrimSpace(C.GoString(value))
	}
	return tags{take(native.title), take(native.artist), take(native.album), take(native.albumArtist), native.artwork != 0}
}

const coverSize = C.coverSize
const bandCount = 24

type cover struct {
	pixels [coverSize][coverSize][4]byte
	png    []byte
}

func readCover(path string, embedded bool) *cover {
	filename := C.CString(path)
	defer C.free(unsafe.Pointer(filename))
	var art cover
	var png *C.uchar
	var size C.size_t
	if C.audio_artwork(filename, flag(embedded), (*C.uchar)(unsafe.Pointer(&art.pixels)), &png, &size) == 0 {
		return nil
	}
	art.png = takeCoverPNG(png, size)
	return &art
}

func readCoverBytes(data []byte) *cover {
	if len(data) == 0 {
		return nil
	}
	var art cover
	var png *C.uchar
	var size C.size_t
	if C.audio_artwork_bytes((*C.uchar)(unsafe.Pointer(&data[0])), C.size_t(len(data)),
		(*C.uchar)(unsafe.Pointer(&art.pixels)), &png, &size) == 0 {
		return nil
	}
	art.png = takeCoverPNG(png, size)
	return &art
}

func takeCoverPNG(png *C.uchar, size C.size_t) []byte {
	if png == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(png))
	return C.GoBytes(unsafe.Pointer(png), C.int(size))
}

func flag(value bool) C.int {
	if value {
		return 1
	}
	return 0
}

func startAudio(path string, volume int) error {
	filename := C.CString(path)
	defer C.free(unsafe.Pointer(filename))
	if message := C.audio_start(filename, C.float(volume)/100); message != nil {
		defer C.free(unsafe.Pointer(message))
		return errors.New(C.GoString(message))
	}
	return nil
}

func startLiveAudio(address string, volume int) bool {
	url := C.CString(address)
	defer C.free(unsafe.Pointer(url))
	return C.audio_start_live(url, C.float(volume)/100) != 0
}

func liveAudioStatus() int { return int(C.audio_live_status()) }

func audioState() playback {
	state := C.audio_state()
	return playback{float64(state.elapsed), float64(state.duration), state.playing != 0}
}

func resumeAudio() bool    { return C.audio_resume() != 0 }
func pauseAudio()          { C.audio_pause() }
func stopAudio()           { C.audio_stop() }
func seekAudio(at float64) { C.audio_seek(C.double(at)) }
func setVolume(value int)  { C.audio_volume(C.float(value) / 100) }
func outputVolume() int    { return int(C.audio_output_volume()) }

func readSpectrum(at float64) ([bandCount]float32, bool) {
	var levels [bandCount]float32
	ok := C.audio_spectrum(C.double(at), (*C.float)(unsafe.Pointer(&levels))) != 0
	return levels, ok
}
