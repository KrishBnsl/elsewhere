//go:build !darwin || dhunn_audio

package audio

// Audio on Linux and Windows: Melisma's `dhunn audio` plays files and streams, measures the
// spectrum and reads tags and embedded covers, so Elsewhere needs no system audio code here.

import (
	"bufio"
	"elsewhere/internal/melisma"
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
	Devices     []OutputDevice `json:"devices"`
}

func (s *audioServer) start() {
	path, err := melisma.Command()
	if err != nil {
		s.err = errors.New("Elsewhere plays sound through Melisma here, and " + err.Error())
		return
	}
	cmd := exec.Command(path, "audio")
	if f := melisma.LogFile("audio.log"); f != nil {
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

func readNativeMetadata(path string) Tags {
	r, err := sound.call(map[string]any{"op": "metadata", "path": path})
	if err != nil {
		return Tags{}
	}
	return Tags{Title: strings.TrimSpace(r.Title), Artist: strings.TrimSpace(r.Artist), Album: strings.TrimSpace(r.Album),
		AlbumArtist: strings.TrimSpace(r.AlbumArtist), Artwork: r.Artwork, Disc: tagNumber(r.Disc), Number: tagNumber(r.Number),
		TrackGain: tagGain(r.TrackGain), AlbumGain: tagGain(r.AlbumGain)}
}

func ReadCover(path string, embedded bool) *Cover {
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

func ReadCoverBytes(data []byte) *Cover {
	if len(data) == 0 {
		return nil
	}
	return renderCover(data)
}

func Start(path string, volume int) error {
	_, err := sound.call(map[string]any{"op": "start", "path": path, "volume": float64(volume) / 100})
	return err
}

func StartLive(address string, volume int) bool {
	_, err := sound.call(map[string]any{"op": "live", "url": address, "volume": float64(volume) / 100})
	return err == nil
}

func LiveStatus() int {
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

func State() Playback {
	if !sound.started() {
		return Playback{} // Nothing has played yet: no need to start the player.
	}
	r, err := sound.call(map[string]any{"op": "state"})
	if err != nil {
		return Playback{}
	}
	return Playback{r.Elapsed, r.Duration, r.Playing}
}

func Resume() bool {
	_, err := sound.call(map[string]any{"op": "resume"})
	return err == nil
}

func Pause() { sound.call(map[string]any{"op": "pause"}) }

func Stop() {
	if sound.started() {
		sound.call(map[string]any{"op": "stop"})
	}
}

func Seek(at float64) { sound.call(map[string]any{"op": "seek", "to": at}) }

func SetVolume(value int) {
	sound.call(map[string]any{"op": "volume", "volume": float64(value) / 100})
}

// outputVolume is the system's own volume: only known on macOS.
func OutputVolume() int { return -1 }

func Spectrum(at float64) ([BandCount]float32, bool) {
	var levels [BandCount]float32
	r, err := sound.call(map[string]any{"op": "spectrum", "bands": BandCount})
	if err != nil {
		return levels, false
	}
	copy(levels[:], r.Bands)
	return levels, true
}

func OutputDevices() ([]OutputDevice, error) {
	reply, err := sound.call(map[string]any{"op": "devices"})
	return reply.Devices, err
}
func ChooseOutput(id string) error {
	_, err := sound.call(map[string]any{"op": "output", "id": id})
	return err
}
func StartMediaControls() {
	if runtime.GOOS != "darwin" {
		_, err := sound.call(map[string]any{"op": "media_start"})
		mediaEnabled = err == nil
	}
}
func StopMediaControls() {
	if mediaEnabled {
		sound.call(map[string]any{"op": "media_stop"})
	}
}
func PollMediaControl() int {
	if !mediaEnabled {
		return 0
	}
	r, _ := sound.call(map[string]any{"op": "media_poll"})
	return r.Action
}
func UpdateMediaControls(m MediaInfo, s Playback) {
	if mediaEnabled {
		sound.call(map[string]any{"op": "media_update", "path": m.Path, "title": m.Title, "artist": m.Artist,
			"album": m.Album, "elapsed": s.Elapsed, "duration": s.Duration, "playing": s.Playing})
	}
}

const SwitchOutputRestarts = true
const CrossfadeSupported = false

func PrepareNext(path string, volume int, _ float64) bool {
	_, err := sound.call(map[string]any{"op": "prepare_next", "path": path, "volume": float64(volume) / 100})
	return err == nil
}
func CancelNext() { sound.call(map[string]any{"op": "cancel_next"}) }
func AdoptNext() bool {
	r, _ := sound.call(map[string]any{"op": "adopt_next"})
	return r.Adopted
}
