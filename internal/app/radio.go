package app

import (
	"bytes"
	"compress/gzip"
	"context"
	"elsewhere/internal/audio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

const chiptuneOrigin = "https://chiptune.app"

var radioCatalogs = []string{
	"Nintendo", "Nintendo (NSFE)", "Nintendo Game Boy", "Nintendo SNES",
	"Sega Game Gear", "Sega Genesis", "Sega Master System", "VGM Rips",
}

type radioSong struct {
	title, game, format, catalogPath string
	wav                              []byte
	err                              error
}

type radioSession struct {
	ctx     context.Context
	cancel  context.CancelFunc
	result  <-chan radioSong
	wanted  bool
	history []*album
	index   int
}

func (p *player) toggleRadio() {
	if p.radio != nil {
		if p.active != nil && p.active.radio {
			audio.Stop()
			p.active, p.expected, p.finished, p.popup = nil, false, false, false
			p.state = audio.Playback{}
		}
		p.stopRadio()
		p.notice = "Chiptune radio off"
		return
	}
	if !radioDecoderAvailable() {
		p.notice = "Radio requires game-music-emu (" + gmeInstallHint + ")"
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.radio = &radioSession{ctx: ctx, cancel: cancel, wanted: true}
	p.notice = "Tuning in to chiptune.app..."
	p.requestRadioSong()
}

func (p *player) stopRadio() {
	if p.radio != nil {
		p.radio.cancel()
		for _, tune := range p.radio.history {
			_ = os.Remove(tune.tracks[0].path)
		}
		p.radio = nil
	}
}

func (p *player) requestRadioSong() {
	radio := p.radio
	if radio.result != nil {
		return
	}
	result := make(chan radioSong, 1)
	radio.result = result
	lastGame := ""
	if len(radio.history) > 0 {
		lastGame = radio.history[len(radio.history)-1].title
	}
	go func(ctx context.Context, last string) {
		result <- findRadioSong(ctx, last)
	}(radio.ctx, lastGame)
}

func (p *player) advanceRadio() {
	if next := p.radio.index + 1; next < len(p.radio.history) {
		p.playRadioHistory(next)
		return
	}
	p.radio.wanted = true
	p.notice = "Finding the next chiptune..."
	p.requestRadioSong()
}

func (p *player) previousRadio() {
	if p.radio.index > 0 {
		p.playRadioHistory(p.radio.index - 1)
	} else {
		p.notice = "No earlier tune in this radio session"
	}
}

func (p *player) playRadioHistory(index int) {
	if p.start(p.radio.history[index], 0) {
		p.radio.index = index
		p.radio.wanted = false
		p.state = audio.State()
	}
}

func (p *player) tickRadio() {
	radio := p.radio
	if radio == nil || !radio.wanted {
		return
	}
	select {
	case song := <-radio.result:
		radio.result = nil
		radio.wanted = false
		if song.err != nil {
			p.notice = "Radio unavailable: " + song.err.Error()
			return
		}
		file, err := os.CreateTemp("", "elsewhere-radio-*.wav")
		if err != nil {
			p.notice = "Radio: " + err.Error()
			return
		}
		_, writeErr := file.Write(song.wav)
		closeErr := file.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			_ = os.Remove(file.Name())
			p.notice = "Radio: " + err.Error()
			return
		}
		a := &album{title: song.game, radio: true, cover: radioStationArtwork()}
		a.tracks = []track{{path: file.Name(), title: song.title, artist: "chiptune.app", album: song.game, source: a, radioFormat: song.format}}
		if !p.start(a, 0) {
			_ = os.Remove(file.Name())
			return
		}
		a.radioArtwork = requestRadioArtwork(radio.ctx, song.catalogPath)
		if len(radio.history) == 0 {
			p.popup, p.lyricsMode = true, false
		}
		radio.history = append(radio.history, a)
		const historyLimit = 8
		if len(radio.history) > historyLimit {
			_ = os.Remove(radio.history[0].tracks[0].path)
			radio.history[0] = nil
			radio.history = radio.history[1:]
		}
		radio.index = len(radio.history) - 1
		p.requestRadioSong()
	default:
	}
}

func findRadioSong(ctx context.Context, lastGame string) radioSong {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 10 * time.Second}
	for _, index := range rand.Perm(len(radioCatalogs))[:3] {
		if ctx.Err() != nil {
			break
		}
		files, err := shuffledGameFiles(ctx, client, chiptuneOrigin, radioCatalogs[index])
		if err != nil {
			continue
		}
		for _, file := range files {
			if ctx.Err() != nil {
				break
			}
			game := path.Base(path.Dir(file))
			if game == lastGame {
				continue
			}
			data, err := radioGet(ctx, client, (&url.URL{Scheme: "https", Host: "chiptune.app", Path: "/catalog/" + file}).String(), 8<<20)
			if err != nil {
				continue
			}
			data, err = unpackRadioFile(data)
			if err != nil {
				continue
			}
			wav, err := renderRadioTrack(ctx, data)
			if err == nil {
				ext := path.Ext(file)
				return radioSong{title: strings.TrimSuffix(path.Base(file), ext), game: game,
					format: strings.ToUpper(strings.TrimPrefix(ext, ".")), catalogPath: file, wav: wav}
			}
		}
	}
	return radioSong{err: errors.New("no playable tracks found. Press > to retry")}
}

func unpackRadioFile(data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, []byte{0x1f, 0x8b}) {
		return data, nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	const limit = 16 << 20
	unpacked, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if len(unpacked) > limit {
		return nil, errors.New("uncompressed chiptune is too large")
	}
	return unpacked, err
}

func shuffledGameFiles(ctx context.Context, client *http.Client, origin, catalog string) ([]string, error) {
	query := url.Values{"path": {catalog}, "limit": {"12"}}
	data, err := radioGet(ctx, client, origin+"/api/shuffle?"+query.Encode(), 1<<20)
	if err != nil {
		return nil, err
	}
	var response struct{ Items []string }
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	files := make([]string, 0, 3)
	for _, item := range response.Items {
		file := strings.TrimPrefix(item, "/")
		if !strings.HasPrefix(file, catalog+"/") || path.Clean(file) != file || strings.Contains(file, "\\") {
			continue
		}
		switch strings.ToLower(path.Ext(file)) {
		case ".nsf", ".nsfe", ".gbs", ".spc", ".vgm", ".vgz", ".gym":
			files = append(files, file)
			if len(files) == 3 {
				return files, nil
			}
		}
	}
	return files, nil
}

func radioGet(ctx context.Context, client *http.Client, endpoint string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Elsewhere/1.0 (macOS music player)")
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("radio request: %s", response.Status)
	}
	if response.ContentLength > limit {
		return nil, errors.New("radio response too large")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if int64(len(data)) > limit {
		return nil, errors.New("radio response too large")
	}
	return data, err
}
