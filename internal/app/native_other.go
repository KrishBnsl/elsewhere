//go:build !darwin || dhunn_audio

package app

// Audio on Linux and Windows: Melisma's `dhunn audio` plays files and streams, measures the
// spectrum and reads tags and embedded covers, so Elsewhere needs no system audio code here.

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

type audioServer struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	in   *json.Encoder
	out  *bufio.Scanner
	err  error
	once sync.Once
}

var sound audioServer
var mediaEnabled bool

type audioReply struct {
	OK          bool           `json:"ok"`
	Error       string         `json:"error"`
	Status      int            `json:"status"`
	Elapsed     float64        `json:"elapsed"`
	Duration    float64        `json:"duration"`
	Playing     bool           `json:"playing"`
	Bands       []float32      `json:"bands"`
	Title       string         `json:"title"`
	Artist      string         `json:"artist"`
	Album       string         `json:"album"`
	AlbumArtist string         `json:"album_artist"`
	Artwork     bool           `json:"artwork"`
	Data        string         `json:"data"`
	Disc        string         `json:"disc"`
	Number      string         `json:"number"`
	TrackGain   string         `json:"track_gain"`
	AlbumGain   string         `json:"album_gain"`
	Action      int            `json:"action"`
	Adopted     bool           `json:"adopted"`
	Devices     []outputDevice `json:"devices"`
}

func (s *audioServer) start() {
	path, err := engineCommand()
	if err != nil {
		s.err = errors.New("Elsewhere plays sound through Melisma here, and " + err.Error())
		return
	}
	cmd := exec.Command(path, "audio")
	if f := logFile("audio.log"); f != nil {
		cmd.Stderr = f
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		s.err = err
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.err = err
		return
	}
	if err := cmd.Start(); err != nil {
		s.err = err
		return
	}
	s.cmd, s.in = cmd, json.NewEncoder(stdin)
	s.out = bufio.NewScanner(stdout)
	s.out.Buffer(make([]byte, 64*1024), 64*1024*1024)
}

// call sends one request to the audio server, starting it the first time.
func (s *audioServer) call(request map[string]any) (audioReply, error) {
	s.once.Do(s.start)
	s.mu.Lock()
	defer s.mu.Unlock()
	var reply audioReply
	if s.err != nil {
		return reply, s.err
	}
	if err := s.in.Encode(request); err != nil {
		s.err = errors.New("the audio player stopped")
		return reply, s.err
	}
	if !s.out.Scan() {
		s.err = errors.New("the audio player stopped (see audio.log in Elsewhere's cache folder)")
		return reply, s.err
	}
	if err := json.Unmarshal(s.out.Bytes(), &reply); err != nil {
		return reply, err
	}
	if !reply.OK {
		return reply, errors.New(reply.Error)
	}
	return reply, nil
}

func readNativeMetadata(path string) tags {
	r, err := sound.call(map[string]any{"op": "metadata", "path": path})
	if err != nil {
		return tags{}
	}
	return tags{title: strings.TrimSpace(r.Title), artist: strings.TrimSpace(r.Artist), album: strings.TrimSpace(r.Album),
		albumArtist: strings.TrimSpace(r.AlbumArtist), artwork: r.Artwork, disc: tagNumber(r.Disc), number: tagNumber(r.Number),
		trackGain: tagGain(r.TrackGain), albumGain: tagGain(r.AlbumGain)}
}

func readCover(path string, embedded bool) *cover {
	if !embedded {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		return renderCover(data)
	}
	r, err := sound.call(map[string]any{"op": "artwork", "path": path})
	if err != nil {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(r.Data)
	if err != nil {
		return nil
	}
	return renderCover(data)
}

func readCoverBytes(data []byte) *cover {
	if len(data) == 0 {
		return nil
	}
	return renderCover(data)
}

func startAudio(path string, volume int) error {
	_, err := sound.call(map[string]any{"op": "start", "path": path, "volume": float64(volume) / 100})
	return err
}

func startLiveAudio(address string, volume int) bool {
	_, err := sound.call(map[string]any{"op": "live", "url": address, "volume": float64(volume) / 100})
	return err == nil
}

func liveAudioStatus() int {
	r, err := sound.call(map[string]any{"op": "live_status"})
	if err != nil {
		return -1
	}
	return r.Status
}

// started reports whether the audio server has been asked for anything yet.
func (s *audioServer) started() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cmd != nil || s.err != nil
}

func audioState() playback {
	if !sound.started() {
		return playback{} // Nothing has played yet: no need to start the player.
	}
	r, err := sound.call(map[string]any{"op": "state"})
	if err != nil {
		return playback{}
	}
	return playback{r.Elapsed, r.Duration, r.Playing}
}

func resumeAudio() bool {
	_, err := sound.call(map[string]any{"op": "resume"})
	return err == nil
}

func pauseAudio() { sound.call(map[string]any{"op": "pause"}) }

func stopAudio() {
	if sound.started() {
		sound.call(map[string]any{"op": "stop"})
	}
}

func seekAudio(at float64) { sound.call(map[string]any{"op": "seek", "to": at}) }

func setVolume(value int) {
	sound.call(map[string]any{"op": "volume", "volume": float64(value) / 100})
}

// outputVolume is the system's own volume: only known on macOS.
func outputVolume() int { return -1 }

func readSpectrum(at float64) ([bandCount]float32, bool) {
	var levels [bandCount]float32
	r, err := sound.call(map[string]any{"op": "spectrum", "bands": bandCount})
	if err != nil {
		return levels, false
	}
	copy(levels[:], r.Bands)
	return levels, true
}

func outputDevices() ([]outputDevice, error) {
	reply, err := sound.call(map[string]any{"op": "devices"})
	return reply.Devices, err
}
func chooseOutput(id string) error {
	_, err := sound.call(map[string]any{"op": "output", "id": id})
	return err
}
func startMediaControls() {
	if runtime.GOOS != "darwin" {
		_, err := sound.call(map[string]any{"op": "media_start"})
		mediaEnabled = err == nil
	}
}
func stopMediaControls() {
	if mediaEnabled {
		sound.call(map[string]any{"op": "media_stop"})
	}
}
func pollMediaControl() int {
	if !mediaEnabled {
		return 0
	}
	r, _ := sound.call(map[string]any{"op": "media_poll"})
	return r.Action
}
func updateMediaControls(t track, s playback) {
	if mediaEnabled {
		sound.call(map[string]any{"op": "media_update", "path": t.path, "title": t.title, "artist": t.artist,
			"album": t.album, "elapsed": s.elapsed, "duration": s.duration, "playing": s.playing})
	}
}

const switchOutputRestarts = true
const crossfadeSupported = false

func prepareNextAudio(path string, volume int, _ float64) bool {
	_, err := sound.call(map[string]any{"op": "prepare_next", "path": path, "volume": float64(volume) / 100})
	return err == nil
}
func cancelNextAudio() { sound.call(map[string]any{"op": "cancel_next"}) }
func adoptNextAudio() bool {
	r, _ := sound.call(map[string]any{"op": "adopt_next"})
	return r.Adopted
}
