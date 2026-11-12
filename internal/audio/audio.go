// Package audio plays music and reads tags and artwork. On macOS the system frameworks do the
// work (native_darwin.go); elsewhere, Melisma's `dhunn audio` does (native_other.go).
package audio

// Playback is the state of the current track.
type Playback struct {
	Elapsed, Duration float64
	Playing           bool
}

// CoverSize is the side of the small square cover drawn with half blocks (audio.h agrees).
const CoverSize = 48

// BandCount is how many spectrum bands Spectrum reports.
const BandCount = 24

// Cover is album art: a small square of RGBA pixels for half-block drawing, and a PNG for terminals
// that show real images.
type Cover struct {
	Pixels [CoverSize][CoverSize][4]byte
	PNG    []byte
}

// OutputDevice is one audio output the system offers.
type OutputDevice struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Current bool   `json:"current"`
}

// MediaInfo describes the playing track for the system's media controls.
type MediaInfo struct{ Path, Title, Artist, Album string }
