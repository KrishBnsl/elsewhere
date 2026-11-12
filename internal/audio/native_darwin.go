//go:build darwin && !dhunn_audio

package audio

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

func readNativeMetadata(path string) Tags {
	filename := C.CString(path)
	defer C.free(unsafe.Pointer(filename))
	native := C.audio_metadata(filename)
	take := func(value *C.char) string {
		defer C.free(unsafe.Pointer(value))
		return strings.TrimSpace(C.GoString(value))
	}
	return Tags{Title: take(native.title), Artist: take(native.artist), Album: take(native.album),
		AlbumArtist: take(native.albumArtist), Artwork: native.artwork != 0, Disc: int(native.disc), Number: int(native.number),
		TrackGain: tagGain(take(native.trackGain)), AlbumGain: tagGain(take(native.albumGain))}
}

// audio.h's coverSize must match the one in audio.go.
const _ = uint(CoverSize-C.coverSize) + uint(C.coverSize-CoverSize)

func ReadCover(path string, embedded bool) *Cover {
	filename := C.CString(path)
	defer C.free(unsafe.Pointer(filename))
	var art Cover
	var png *C.uchar
	var size C.size_t
	if C.audio_artwork(filename, flag(embedded), (*C.uchar)(unsafe.Pointer(&art.Pixels)), &png, &size) == 0 {
		return nil
	}
	art.PNG = takeCoverPNG(png, size)
	return &art
}

func ReadCoverBytes(data []byte) *Cover {
	if len(data) == 0 {
		return nil
	}
	var art Cover
	var png *C.uchar
	var size C.size_t
	if C.audio_artwork_bytes((*C.uchar)(unsafe.Pointer(&data[0])), C.size_t(len(data)),
		(*C.uchar)(unsafe.Pointer(&art.Pixels)), &png, &size) == 0 {
		return nil
	}
	art.PNG = takeCoverPNG(png, size)
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

func Start(path string, volume int) error {
	filename := C.CString(path)
	defer C.free(unsafe.Pointer(filename))
	if message := C.audio_start(filename, C.float(volume)/100); message != nil {
		defer C.free(unsafe.Pointer(message))
		return errors.New(C.GoString(message))
	}
	return nil
}

func StartLive(address string, volume int) bool {
	url := C.CString(address)
	defer C.free(unsafe.Pointer(url))
	return C.audio_start_live(url, C.float(volume)/100) != 0
}

func LiveStatus() int { return int(C.audio_live_status()) }

func State() Playback {
	state := C.audio_state()
	return Playback{float64(state.elapsed), float64(state.duration), state.playing != 0}
}

func Resume() bool        { return C.audio_resume() != 0 }
func Pause()              { C.audio_pause() }
func Stop()               { C.audio_stop() }
func Seek(at float64)     { C.audio_seek(C.double(at)) }
func SetVolume(value int) { C.audio_volume(C.float(value) / 100) }
func OutputVolume() int   { return int(C.audio_output_volume()) }

func Spectrum(at float64) ([BandCount]float32, bool) {
	var levels [BandCount]float32
	ok := C.audio_spectrum(C.double(at), (*C.float)(unsafe.Pointer(&levels))) != 0
	return levels, ok
}

func OutputDevices() ([]OutputDevice, error) {
	raw := C.audio_devices()
	if raw == nil {
		return nil, errors.New("Cannot list system outputs")
	}
	defer C.free(unsafe.Pointer(raw))
	var devices []OutputDevice
	err := json.Unmarshal([]byte(C.GoString(raw)), &devices)
	return devices, err
}

func ChooseOutput(id string) error {
	n, err := strconv.ParseUint(id, 10, 32)
	if err != nil || C.audio_select_device(C.uint(n)) == 0 {
		return errors.New("Cannot switch system output")
	}
	return nil
}

func StartMediaControls()   { C.audio_media_start() }
func StopMediaControls()    { C.audio_media_stop() }
func PollMediaControl() int { return int(C.audio_media_poll()) }
func UpdateMediaControls(m MediaInfo, s Playback) {
	title, artist, album := C.CString(m.Title), C.CString(m.Artist), C.CString(m.Album)
	defer C.free(unsafe.Pointer(title))
	defer C.free(unsafe.Pointer(artist))
	defer C.free(unsafe.Pointer(album))
	C.audio_media_update(title, artist, album, C.double(s.Elapsed), C.double(s.Duration), flag(s.Playing))
}

const SwitchOutputRestarts = false
const CrossfadeSupported = true

func PrepareNext(path string, volume int, overlap float64) bool {
	name := C.CString(path)
	defer C.free(unsafe.Pointer(name))
	return C.audio_prepare_next(name, C.float(volume)/100, C.double(overlap)) != 0
}
func CancelNext()     { C.audio_cancel_next() }
func AdoptNext() bool { return C.audio_adopt_next() != 0 }
