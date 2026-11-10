package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type radioStation struct {
	name, description, provider, stream, backup string
	rainwaveID                                  int
}

var radioStations = []radioStation{
	{name: "Chiptune Radio", description: "Random retro games", provider: "chiptune.app"},
	{name: "Anime FM", description: "Anime songs and J-pop", provider: "LISTEN.moe", stream: "https://listen.moe/fallback"},
	{name: "Game OST", description: "Original game soundtracks", provider: "Rainwave", stream: "https://relay.rainwave.cc/game.mp3", backup: "https://allrelays.rainwave.cc/game.mp3", rainwaveID: 1},
	{name: "Remix Lab", description: "OverClocked ReMix", provider: "Rainwave", stream: "https://relay.rainwave.cc/ocremix.mp3", backup: "https://allrelays.rainwave.cc/ocremix.mp3", rainwaveID: 2},
	{name: "Vocaloid & Doujin", description: "Vocaloid, anime, doujin and game tracks", provider: radioProvider, stream: "https://stream.r-a-d.io/main.mp3", backup: "https://relay0.r-a-d.io/main.mp3"},
}

var stopRadioStation = radioStation{name: "Stop radio", description: "Return to the library"}

type liveRadio struct {
	station    radioStation
	paused     bool
	connecting time.Time
	nextInfo   time.Time
	ctx        context.Context
	cancel     context.CancelFunc
	info       <-chan liveTrackInfo
	artPath    string
	retried    bool
}

type liveTrackInfo struct {
	title, artist, album, artPath string
	artPaths                      []string
	art                           *cover
}

// coverCache remembers the most recent artwork a feed fetched, keyed by its source URL.
type coverCache struct {
	mu  sync.Mutex
	key string
	art *cover
}

func (c *coverCache) get(key string) *cover {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key == key {
		return c.art
	}
	return nil
}

func (c *coverCache) set(key string, art *cover) {
	c.mu.Lock()
	c.key, c.art = key, art
	c.mu.Unlock()
}

func (p *player) tuneStation(station radioStation) {
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
		if !radioDecoderAvailable() {
			p.notice = "Chiptune requires game-music-emu (" + gmeInstallHint + ")"
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
	if !startLiveAudio(station.stream, p.volume) {
		p.notice = "Could not open " + station.name
		return
	}

	p.stopRadio()
	p.clearLiveStation()
	p.leaveQueue()
	ctx, cancel := context.WithCancel(context.Background())
	p.live = &liveRadio{station: station, connecting: time.Now().Add(10 * time.Second), ctx: ctx, cancel: cancel}
	switch station.provider {
	case "LISTEN.moe":
		p.live.info = listenUpdates(ctx)
	case radioProvider:
		p.live.info = radioUpdates(ctx)
	}
	a := &album{title: station.name, radio: true, cover: radioStationArtwork()}
	a.tracks = []track{{title: station.name, artist: station.provider, album: station.description, source: a, radioFormat: "LIVE"}}
	p.active, p.playingIndex = a, 0
	p.state = playback{}
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
		stopAudio()
		p.active, p.expected, p.finished, p.popup = nil, false, false, false
		p.state = playback{}
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
	if startLiveAudio(live.station.stream, p.volume) {
		live.paused = false
		live.connecting = time.Now().Add(10 * time.Second)
		live.retried = false
		p.notice = "Reconnecting to " + live.station.provider + "..."
	} else {
		p.notice = "Could not reconnect. Press Space to retry."
	}
}

func (p *player) pauseLive() {
	pauseAudio()
	p.live.paused = true
	p.live.connecting = time.Time{}
	p.notice = ""
}

func (p *player) liveStatus() string {
	switch {
	case p.live.paused:
		return "PAUSED"
	case p.state.playing:
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
		switch liveAudioStatus() {
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
		result := make(chan liveTrackInfo, 1)
		live.info = result
		go func(ctx context.Context, sid int, knownArt string) {
			result <- rainwaveNowPlaying(ctx, &http.Client{Timeout: 4 * time.Second}, "https://rainwave.cc", sid, knownArt)
		}(live.ctx, live.station.rainwaveID, live.artPath)
	}
	select {
	case info := <-live.info:
		if live.station.rainwaveID != 0 {
			live.info = nil
		}
		if info.title != "" {
			t := p.current()
			t.title = info.title
			t.artist = live.station.provider
			if info.artist != "" {
				t.artist = info.artist
			}
			t.album = live.station.name
			if info.album != "" {
				t.album = info.album
			}
			if info.artPath != live.artPath || (info.art != nil && info.art != p.active.cover) {
				p.active.cover = info.art
				if p.active.cover == nil {
					p.active.cover = radioStationArtwork()
				}
				p.active.artworkSize = 0
				live.artPath = ""
				if info.art != nil {
					live.artPath = info.artPath
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
		if startLiveAudio(stream, p.volume) {
			live.connecting = now.Add(10 * time.Second)
			p.notice = "Retrying " + live.station.provider + "..."
			return
		}
	}
	p.pauseLive()
	p.notice = reason + ". Press Space to retry."
}

func rainwaveNowPlaying(ctx context.Context, client *http.Client, origin string, sid int, knownArt string) liveTrackInfo {
	data, err := radioGet(ctx, client, fmt.Sprintf("%s/api4/info?sid=%d", origin, sid), 1<<20)
	if err != nil {
		return liveTrackInfo{}
	}
	var response struct {
		Current struct {
			Songs []struct {
				Title   string
				Artists []struct{ Name string }
				Albums  []struct{ Name, Art string }
			}
		} `json:"sched_current"`
	}
	if json.Unmarshal(data, &response) != nil || len(response.Current.Songs) == 0 {
		return liveTrackInfo{}
	}
	song := response.Current.Songs[0]
	info := liveTrackInfo{title: song.Title}
	var artists []string
	for _, artist := range song.Artists {
		artists = append(artists, artist.Name)
	}
	info.artist = strings.Join(artists, ", ")
	if len(song.Albums) == 0 {
		return info
	}
	info.album = song.Albums[0].Name
	artPath := song.Albums[0].Art
	asset, err := url.Parse(artPath)
	if err != nil || !strings.HasPrefix(artPath, "/album_art/") || asset.Host != "" {
		return info
	}
	info.artPath = artPath
	if artPath != knownArt {
		if bytes, err := radioGet(ctx, client, origin+artPath+"_320.jpg", maxCoverBytes); err == nil {
			info.art = readCoverBytes(bytes)
		}
	}
	return info
}
