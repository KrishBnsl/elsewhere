package app

import (
	"context"
	"elsewhere/internal/audio"
	"elsewhere/internal/radio"
	"net/http"
	"time"
)

type radioStation struct {
	name, description, provider, stream, backup string
	rainwaveID                                  int
}

var radioStations = []radioStation{
	{name: "Chiptune Radio", description: "Random retro games", provider: "chiptune.app"},
	{name: "Anime FM", description: "Anime songs and J-pop", provider: radio.ListenProvider, stream: "https://listen.moe/fallback"},
	{name: "Game OST", description: "Original game soundtracks", provider: "Rainwave", stream: "https://relay.rainwave.cc/game.mp3", backup: "https://allrelays.rainwave.cc/game.mp3", rainwaveID: 1},
	{name: "Remix Lab", description: "OverClocked ReMix", provider: "Rainwave", stream: "https://relay.rainwave.cc/ocremix.mp3", backup: "https://allrelays.rainwave.cc/ocremix.mp3", rainwaveID: 2},
	{name: "Vocaloid & Doujin", description: "Vocaloid, anime, doujin and game tracks", provider: radio.RadProvider, stream: "https://stream.r-a-d.io/main.mp3", backup: "https://relay0.r-a-d.io/main.mp3"},
}

var stopRadioStation = radioStation{name: "Stop radio", description: "Return to the library"}

type liveRadio struct {
	station    radioStation
	paused     bool
	connecting time.Time
	nextInfo   time.Time
	ctx        context.Context
	cancel     context.CancelFunc
	info       <-chan radio.TrackInfo
	artPath    string
	retried    bool
}

func (p *player) tuneStation(station radioStation) {
	if p.inJam() {
		p.notice = "Leave the jam before tuning to radio"
		return
	}
	if station == stopRadioStation {
		p.stopStation()
		return
	}
	if station.stream == "" {
		if p.radio != nil {
			if p.active != nil && p.active.radio {
				p.popup = true
			}
			return
		}
		if !radio.DecoderAvailable() {
			p.notice = "Chiptune requires game-music-emu (" + radio.InstallHint + ")"
			return
		}
		if p.live != nil {
			p.stopStation()
		}
		p.leaveQueue()
		p.toggleRadio()
		return
	}
	if p.live != nil && p.live.station == station {
		p.popup = true
		return
	}
	if !audio.StartLive(station.stream, p.volume) {
		p.notice = "Could not open " + station.name
		return
	}

	p.stopRadio()
	p.clearLiveStation()
	p.leaveQueue()
	ctx, cancel := context.WithCancel(context.Background())
	p.live = &liveRadio{station: station, connecting: time.Now().Add(10 * time.Second), ctx: ctx, cancel: cancel}
	switch station.provider {
	case radio.ListenProvider:
		p.live.info = radio.ListenUpdates(ctx)
	case radio.RadProvider:
		p.live.info = radio.RadUpdates(ctx)
	}
	a := &album{title: station.name, radio: true, cover: radio.StationArtwork()}
	a.tracks = []track{{title: station.name, artist: station.provider, album: station.description, source: a, radioFormat: "LIVE"}}
	p.active, p.playingIndex, p.restored = a, 0, false
	p.state = audio.Playback{}
	p.expected, p.finished, p.analysisAvailable = false, false, false
	p.popup, p.lyricsMode = true, false
	p.notice = "Connecting to " + station.provider + "..."
}

func (p *player) clearLiveStation() {
	if p.live != nil {
		p.live.cancel()
		p.live = nil
	}
}

func (p *player) stopStation() {
	if p.active != nil && p.active.radio {
		audio.Stop()
		p.active, p.expected, p.finished, p.popup = nil, false, false, false
		p.state = audio.Playback{}
	}
	p.stopRadio()
	p.clearLiveStation()
	p.notice = "Radio off"
}

func (p *player) toggleLivePlayback() {
	live := p.live
	if !live.paused {
		p.pauseLive()
		return
	}
	if audio.StartLive(live.station.stream, p.volume) {
		live.paused = false
		live.connecting = time.Now().Add(10 * time.Second)
		live.retried = false
		p.notice = "Reconnecting to " + live.station.provider + "..."
	} else {
		p.notice = "Could not reconnect. Press Space to retry."
	}
}

func (p *player) pauseLive() {
	audio.Pause()
	p.live.paused = true
	p.live.connecting = time.Time{}
	p.notice = ""
}

func (p *player) liveStatus() string {
	switch {
	case p.live.paused:
		return "PAUSED"
	case p.state.Playing:
		return "ON AIR"
	case !p.live.connecting.IsZero():
		return "CONNECTING"
	default:
		return "BUFFERING"
	}
}

func (p *player) updateLive(now time.Time) {
	live := p.live
	if !live.paused {
		switch audio.LiveStatus() {
		case 1:
			if !live.connecting.IsZero() {
				live.connecting = time.Time{}
				p.notice = ""
			}
		case -1:
			p.retryLive(now, "Stream unavailable")
		case 0:
			if live.connecting.IsZero() {
				live.connecting = now.Add(10 * time.Second)
			} else if !now.Before(live.connecting) {
				p.retryLive(now, "Connection timed out")
			}
		}
	}
	if live.station.rainwaveID != 0 && !live.paused && live.info == nil && !now.Before(live.nextInfo) {
		live.nextInfo = now.Add(15 * time.Second)
		result := make(chan radio.TrackInfo, 1)
		live.info = result
		go func(ctx context.Context, sid int, knownArt string) {
			result <- radio.Rainwave(ctx, &http.Client{Timeout: 4 * time.Second}, "https://rainwave.cc", sid, knownArt)
		}(live.ctx, live.station.rainwaveID, live.artPath)
	}
	select {
	case info := <-live.info:
		if live.station.rainwaveID != 0 {
			live.info = nil
		}
		if info.Title != "" {
			t := p.current()
			t.title = info.Title
			t.artist = live.station.provider
			if info.Artist != "" {
				t.artist = info.Artist
			}
			t.album = live.station.name
			if info.Album != "" {
				t.album = info.Album
			}
			if info.ArtPath != live.artPath || (info.Art != nil && info.Art != p.active.cover) {
				p.active.cover = info.Art
				if p.active.cover == nil {
					p.active.cover = radio.StationArtwork()
				}
				p.active.artworkSize = 0
				live.artPath = ""
				if info.Art != nil {
					live.artPath = info.ArtPath
				}
			}
		}
	default:
	}
}

func (p *player) retryLive(now time.Time, reason string) {
	live := p.live
	if !live.retried {
		live.retried = true
		stream := live.station.backup
		if stream == "" {
			stream = live.station.stream
		}
		if audio.StartLive(stream, p.volume) {
			live.connecting = now.Add(10 * time.Second)
			p.notice = "Retrying " + live.station.provider + "..."
			return
		}
	}
	p.pauseLive()
	p.notice = reason + ". Press Space to retry."
}
