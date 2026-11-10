//go:build darwin && !dhunn_audio

package main

/*
#cgo CFLAGS: -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework AVFoundation -framework Foundation -framework CoreAudio -framework Accelerate -framework ImageIO -framework CoreGraphics -framework MediaPlayer -framework AppKit
#include <stdlib.h>
#include "audio.h"
*/
import "C"

import (
	"encoding/json"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"unsafe"
)

// AppKit and its event loop must stay on the original OS thread.
func init() { runtime.LockOSThread() }

func readNativeMetadata(path string) tags {
	filename := C.CString(path)
	defer C.free(unsafe.Pointer(filename))
	native := C.audio_metadata(filename)
	take := func(value *C.char) string {
		defer C.free(unsafe.Pointer(value))
		return strings.TrimSpace(C.GoString(value))
	}
	return tags{title: take(native.title), artist: take(native.artist), album: take(native.album),
		albumArtist: take(native.albumArtist), artwork: native.artwork != 0, disc: int(native.disc), number: int(native.number),
		trackGain: tagGain(take(native.trackGain)), albumGain: tagGain(take(native.albumGain))}
}

// audio.h's coverSize must match the one in audio.go.
const _ = uint(coverSize-C.coverSize) + uint(C.coverSize-coverSize)

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

func outputDevices() ([]outputDevice, error) {
	raw := C.audio_devices()
	if raw == nil {
		return nil, errors.New("Cannot list system outputs")
	}
	defer C.free(unsafe.Pointer(raw))
	var devices []outputDevice
	err := json.Unmarshal([]byte(C.GoString(raw)), &devices)
	return devices, err
}

func chooseOutput(id string) error {
	n, err := strconv.ParseUint(id, 10, 32)
	if err != nil || C.audio_select_device(C.uint(n)) == 0 {
		return errors.New("Cannot switch system output")
	}
	return nil
}

func startMediaControls()   { C.audio_media_start() }
func stopMediaControls()    { C.audio_media_stop() }
func pollMediaControl() int { return int(C.audio_media_poll()) }
func updateMediaControls(t track, s playback) {
	title, artist, album := C.CString(t.title), C.CString(t.artist), C.CString(t.album)
	defer C.free(unsafe.Pointer(title))
	defer C.free(unsafe.Pointer(artist))
	defer C.free(unsafe.Pointer(album))
	C.audio_media_update(title, artist, album, C.double(s.elapsed), C.double(s.duration), flag(s.playing))
}

const switchOutputRestarts = false
const crossfadeSupported = true

func prepareNextAudio(path string, volume int, overlap float64) bool {
	name := C.CString(path)
	defer C.free(unsafe.Pointer(name))
	return C.audio_prepare_next(name, C.float(volume)/100, C.double(overlap)) != 0
}
func cancelNextAudio()     { C.audio_cancel_next() }
func adoptNextAudio() bool { return C.audio_adopt_next() != 0 }
