package app

// What every platform's audio layer shares. On macOS the system frameworks do the work
// (native_darwin.go); elsewhere, Melisma's `dhunn audio` does (native_other.go).

type playback struct {
	elapsed, duration float64
	playing           bool
}

// coverSize is the side of the small square cover drawn with half blocks (audio.h agrees).
const coverSize = 48
const bandCount = 24

type cover struct {
	pixels [coverSize][coverSize][4]byte
	png    []byte
}

type outputDevice struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Current bool   `json:"current"`
}
