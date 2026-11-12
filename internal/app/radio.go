package app

import (
	"context"
	"elsewhere/internal/audio"
	"elsewhere/internal/radio"
	"errors"
	"os"
)

type radioSession struct {
	ctx     context.Context
	cancel  context.CancelFunc
	result  <-chan radio.Song
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
	if !radio.DecoderAvailable() {
		p.notice = "Radio requires game-music-emu (" + radio.InstallHint + ")"
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
	session := p.radio
	if session.result != nil {
		return
	}
	result := make(chan radio.Song, 1)
	session.result = result
	lastGame := ""
	if len(session.history) > 0 {
		lastGame = session.history[len(session.history)-1].title
	}
	go func(ctx context.Context, last string) {
		result <- radio.FindSong(ctx, last)
	}(session.ctx, lastGame)
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
	session := p.radio
	if session == nil || !session.wanted {
		return
	}
	select {
	case song := <-session.result:
		session.result = nil
		session.wanted = false
		if song.Err != nil {
			p.notice = "Radio unavailable: " + song.Err.Error()
			return
		}
		file, err := os.CreateTemp("", "elsewhere-radio-*.wav")
		if err != nil {
			p.notice = "Radio: " + err.Error()
			return
		}
		_, writeErr := file.Write(song.WAV)
		closeErr := file.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			_ = os.Remove(file.Name())
			p.notice = "Radio: " + err.Error()
			return
		}
		a := &album{title: song.Game, radio: true, cover: radio.StationArtwork()}
		a.tracks = []track{{path: file.Name(), title: song.Title, artist: "chiptune.app", album: song.Game, source: a, radioFormat: song.Format}}
		if !p.start(a, 0) {
			_ = os.Remove(file.Name())
			return
		}
		a.radioArtwork = radio.RequestGameArtwork(session.ctx, song.CatalogPath)
		if len(session.history) == 0 {
			p.popup, p.lyricsMode = true, false
		}
		session.history = append(session.history, a)
		const historyLimit = 8
		if len(session.history) > historyLimit {
			_ = os.Remove(session.history[0].tracks[0].path)
			session.history[0] = nil
			session.history = session.history[1:]
		}
		session.index = len(session.history) - 1
		p.requestRadioSong()
	default:
	}
}
