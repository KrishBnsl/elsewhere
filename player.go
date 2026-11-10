package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

type player struct {
	social                                friendsState
	listenHistory                         []listenEntry
	blind, revealed, tripDetour           bool
	tripDestination                       string
	tripLeg                               *tripLeg
	sessionPath, repeat, gain             string
	restored, shuffle, crossfade          bool
	shuffled                              []int
	nextPrepared                          bool
	shuffledAlbum                         *album
	finder                                *libraryFinder
	albums, results                       []*album
	path, query, notice                   string
	albumIndex, trackIndex, playingIndex  int
	trackPane, searching, popup, visual   bool
	expected, finished, analysisAvailable bool
	active                                *album
	state                                 playback
	volume, output, phase                 int
	levels                                [bandCount]float32
	nextAnalysis, nextOutput, nextMedia   time.Time
	lyricsMode                            bool
	lyricsPath                            string
	lyrics                                []lyricLine
	lyricsStatus                          string
	lyricsRequest                         <-chan lyricsResult
	cancelLyrics                          context.CancelFunc
	coverLookup                           coverLookup
	likes                                 songLikes
	likedSongs                            *album
	playlistPath                          string
	playlistRecords                       []playlist
	playlists                             []*album
	playlistDialog                        *playlistDialog
	searchPrompt                          searchPrompt
	radio                                 *radioSession
	kittyGraphics                         bool
	coverPlacement                        kittyPlacement
	coverPrompt                           *album
	live                                  *liveRadio
	queue                                 []track
	fromQueue                             bool
	resumeAlbum                           *album
	resumeIndex                           int
	queueView                             *queueView
	command                               *commandPrompt
	themePicker                           *themePicker
	paletteIndex                          int
	themePath                             string
	radioPicker                           *radioPicker
	engine                                *engine
	engineReady                           <-chan engineStart
	engineErr                             string
	melisma                               bool
	picked                                *enginePick
	why                                   []string
	known                                 map[string]bool
	analysis                              *analysis
	tasteView                             *tasteView
	// Melisma was asked to take over while you chose this song: it does after it ends.
	handover bool
	// The last direction you gave Melisma (/toward, /usual …).
	melismaMode map[string]any
}

func (p *player) tracks() []track {
	if len(p.results) == 0 {
		return nil
	}
	return p.results[p.albumIndex].tracks
}

func (p *player) current() *track {
	if p.active == nil {
		return nil
	}
	return &p.active.tracks[p.playingIndex]
}

func (p *player) search() {
	p.albumIndex, p.trackIndex = 0, 0
	library := make([]*album, 0, len(p.albums)+len(p.playlists)+1)
	library = append(library, p.likedSongs)
	library = append(library, p.playlists...)
	library = append(library, p.albums...)
	if p.query == "" {
		p.results = library
		if len(p.likedSongs.tracks) == 0 && len(p.albums) > 0 {
			p.albumIndex = 1 + len(p.playlists)
		}
		return
	}
	query := strings.ToLower(p.query)
	p.results = nil
	for _, a := range library {
		match := strings.Contains(strings.ToLower(a.title), query) || strings.Contains(strings.ToLower(a.artist), query) || strings.Contains(strings.ToLower(a.description), query)
		for _, t := range a.tracks {
			if match {
				break
			}
			match = strings.Contains(strings.ToLower(t.title), query) || strings.Contains(strings.ToLower(t.artist), query)
		}
		if match {
			p.results = append(p.results, a)
		}
	}
}

func (p *player) start(a *album, i int) bool {
	if p.inJam() && !p.social.jam.starting {
		p.notice = "The jam controls playback · /jam leave to listen solo"
		return false
	}
	if a == nil || i < 0 || i >= len(a.tracks) {
		return false
	}
	t := a.tracks[i]
	p.cancelTransition()
	if err := startAudio(t.path, p.trackVolume(t)); err != nil {
		p.notice = fmt.Sprintf("Cannot play %s: %s", t.title, err)
		return false
	}
	p.nowPlaying(a, i)
	return true
}

func (p *player) nowPlaying(a *album, i int) {
	t := a.tracks[i]
	resumed := p.restored && p.current() != nil && p.current().path == t.path
	p.active, p.playingIndex, p.restored = a, i, false
	p.revealed = false
	p.tripLeg = nil
	p.expected, p.finished, p.analysisAvailable = true, false, true
	// Melisma hears about every local song, and explains the ones it chose.
	p.why, p.handover = nil, false
	if p.picked != nil && p.picked.Path == t.path {
		p.why = p.picked.Why
		p.tripLeg = p.picked.Leg
	}
	p.picked = nil
	if t.radioFormat == "" && !a.radio {
		if !resumed || len(p.listenHistory) == 0 {
			p.rememberTrack(t)
		}
		p.engine.tell("played", "path", t.path)
	}
	p.notice = ""
	p.levels = [bandCount]float32{}
	p.nextAnalysis, p.nextMedia = time.Time{}, time.Time{}
}

func (p *player) playSelection() {
	if len(p.results) == 0 {
		return
	}
	index := 0
	if p.trackPane {
		index = p.trackIndex
	}
	p.leaving("choose")
	if p.start(p.results[p.albumIndex], index) {
		p.melisma = false
		p.tripDestination, p.tripDetour = "", false
		p.stopRadio()
		p.clearLiveStation()
		p.leaveQueue()
		p.shuffledAlbum = nil
		p.popup = true
	}
}

func (p *player) tick(now time.Time) {
	p.tickFriends(now)
	switch pollMediaControl() {
	case 1:
		p.handlePlayback(" ")
	case 2:
		p.handlePlayback("n")
	case 3:
		p.handlePlayback("p")
	case 4:
		if !p.jamTransport("play") && !audioState().playing {
			p.handlePlayback(" ")
		}
	case 5:
		if !p.jamTransport("pause") && audioState().playing {
			p.handlePlayback(" ")
		}
	}
	p.pollEngine()
	p.tickAnalysis()
	p.tickRadio()
	p.tickJam(now)
	p.tickTaste(now)
	if p.active == nil {
		return
	}
	if p.restored {
		p.publishMedia(now)
		return
	}
	p.state = audioState()
	p.prepareTransition()
	if p.live != nil {
		p.updateLive(now)
	}
	if !p.inJam() && p.expected && !p.state.playing {
		p.expected, p.finished = false, true
		if !p.active.radio && p.live == nil {
			p.engineTell("finished")
		}
		if p.active.radio && p.radio != nil {
			p.advanceRadio()
		} else if p.nextPrepared && adoptNextAudio() {
			p.nextPrepared = false
			p.nowPlaying(p.active, p.playingIndex+1)
			p.state = audioState()
		} else if p.repeat == "track" && p.live == nil && !p.active.radio && p.start(p.active, p.playingIndex) {
			p.state = audioState()
		} else if p.advance() {
			p.state = audioState()
		}
	}
	if p.finished {
		p.state.elapsed = p.state.duration
	}
	p.publishMedia(now)
	if !p.popup || !p.lyricsMode || p.hiddenTrack() {
		p.stopLyricsLookup()
	}
	if !p.hiddenTrack() {
		p.updateCover()
	}
	if !p.popup {
		return
	}
	if !now.Before(p.nextOutput) {
		p.output = outputVolume()
		p.nextOutput = now.Add(500 * time.Millisecond)
	}
	if p.lyricsMode && !p.hiddenTrack() {
		p.updateLyrics()
		return
	}
	if !p.visual && !p.hiddenTrack() {
		return
	}
	if !p.state.playing {
		p.levels = [bandCount]float32{}
		return
	}
	if p.analysisAvailable && !now.Before(p.nextAnalysis) {
		p.nextAnalysis = now.Add(75 * time.Millisecond)
		levels, ok := readSpectrum(p.state.elapsed)
		p.analysisAvailable = ok
		for i, level := range levels {
			p.levels[i] = max(level, p.levels[i]*0.77)
		}
		p.phase = (p.phase + 1) % 100000
	}
}

func (p *player) publishMedia(now time.Time) {
	if t := p.current(); t != nil && !now.Before(p.nextMedia) {
		display := *t
		if p.hiddenTrack() {
			display = track{path: "elsewhere:blind", title: "Mystery track"}
		}
		updateMediaControls(display, p.state)
		p.nextMedia = now.Add(time.Second)
	}
}

func (p *player) stopLyricsLookup() {
	if p.cancelLyrics != nil {
		p.cancelLyrics()
		p.cancelLyrics, p.lyricsRequest = nil, nil
		p.lyricsPath = ""
	}
}

func (p *player) updateLyrics() {
	t := p.current()
	if t == nil {
		return
	}
	if t.radioFormat != "" {
		p.stopLyricsLookup()
		p.lyrics, p.lyricsPath = nil, t.path
		p.lyricsStatus = "Chiptune radio"
		return
	}
	if t.path != p.lyricsPath {
		p.stopLyricsLookup()
		p.lyricsPath = t.path
		p.lyrics = loadLyrics(t.path)
		p.lyricsStatus = ""
		if len(p.lyrics) == 0 {
			p.lyricsStatus = "Finding lyrics on LRCLIB..."
			ctx, cancel := context.WithCancel(context.Background())
			results := make(chan lyricsResult, 1)
			p.cancelLyrics, p.lyricsRequest = cancel, results
			go func(track track, duration float64) {
				results <- onlineLyrics(ctx, track, duration)
			}(*t, p.state.duration)
		}
	}
	select {
	case result := <-p.lyricsRequest:
		p.lyrics, p.lyricsStatus = result.lines, result.status
		p.cancelLyrics()
		p.cancelLyrics, p.lyricsRequest = nil, nil
	default:
	}
}

func (p *player) handle(key string) bool {
	if key == "\x03" {
		return false
	}
	if p.command != nil {
		return p.handleCommand(key)
	}
	if p.themePicker != nil {
		p.handleThemePicker(key)
		return true
	}
	if p.tasteView != nil {
		if key == "\x1b" || key == "q" || key == "\r" {
			p.tasteView = nil
		}
		return true
	}
	if p.social.panel != nil {
		p.handleFriends(key)
		return true
	}
	if p.finder != nil {
		p.handleFinder(key)
		return true
	}
	if p.queueView != nil {
		p.handleQueueView(key)
		return true
	}
	if p.radioPicker != nil {
		p.handleRadioPicker(key)
		return true
	}
	if p.playlistDialog != nil {
		p.handlePlaylist(key)
		return true
	}
	if p.searching {
		p.handleSearch(key)
		return true
	}
	if p.coverPrompt != nil {
		switch key {
		case "y", "Y", "\r", "\n":
			p.answerCoverPrompt(true)
		case "n", "N", "\x1b":
			p.answerCoverPrompt(false)
		}
		return true
	}
	if p.popup {
		switch key {
		case "r", "R":
			if p.hiddenTrack() {
				p.revealed = true
				p.nextMedia = time.Time{}
			}
			return true
		case "c", "C":
			if p.hiddenTrack() {
				return true
			}
			if t := p.current(); t != nil && t.source.artist != "" && t.source.cover == nil && p.coverLookup.result == nil {
				t.source.loaded = false
				t.source.coverPromptDismissed = false
			}
			return true
		case "\x1b", "m":
			p.popup = false
			return true
		case "l", "L":
			p.lyricsMode = !p.lyricsMode
			p.lyricsPath = ""
			return true
		case "v":
			if p.lyricsMode {
				p.lyricsMode = false
				p.visual = true
			} else {
				p.visual = !p.visual
			}
			return true
		case "f", "F":
			p.steerMelisma("further")
			return true
		case "b", "B":
			p.steerMelisma("back")
			return true
		case "s", "S":
			p.steerMelisma("surprise")
			return true
		case "d", "D":
			p.steerMelisma("dislike")
			return true
		case "\x1b[C":
			key = "]"
		case "\x1b[D":
			key = "["
		case "g", "N", "Q", "q", ":", " ", "n", "p", ">", "<", "[", "]", "-", "+", "=", "*", "/":
		default:
			return true
		}
	}
	switch key {
	case "g":
		p.finder = &libraryFinder{}
	case "N", "Q":
		p.enqueue(p.queueTargets(), key == "N")
	case "q":
		p.toggleQueue()
	case ":":
		p.command, p.paletteIndex = &commandPrompt{}, -1
	case "*":
		p.toggleLike()
	case "m":
		p.popup = p.active != nil
	case "L":
		if p.active != nil {
			p.popup, p.lyricsMode = true, true
			p.lyricsPath = ""
		}
	case "/", "f":
		p.beginSearch()
	case "\t":
		p.trackPane = !p.trackPane
	case "\x1b[C", "l":
		p.trackPane = true
	case "\x1b[D", "h":
		p.trackPane = false
	case "\x1b[A", "k":
		p.move(-1)
	case "\x1b[B", "j":
		p.move(1)
	case "\r", "\n":
		p.playSelection()
	case " ", "n", ">", "p", "<":
		p.handlePlayback(key)
	case "[", "]":
		p.handleSeek(key)
	case "-", "+", "=":
		delta := 5
		if key == "-" {
			delta = -5
		}
		p.cancelTransition()
		p.volume = min(100, max(0, p.volume+delta))
		if t := p.localSong(); t != nil {
			setVolume(p.trackVolume(*t))
		} else {
			setVolume(p.volume)
		}
	}
	return true
}

func (p *player) handlePlayback(key string) {
	if p.jamTransport(key) {
		return
	}
	switch key {
	case " ":
		if p.live != nil {
			p.toggleLivePlayback()
		} else if p.active == nil {
			p.playSelection()
		} else if p.restored {
			pos := p.state.elapsed
			if p.start(p.active, p.playingIndex) {
				seekAudio(max(0, min(pos, audioState().duration-0.01)))
			}
		} else if audioState().playing {
			p.cancelTransition()
			pauseAudio()
			p.expected = false
			p.engineTell("pause")
		} else {
			if p.finished {
				seekAudio(0)
			}
			p.expected = resumeAudio()
			if p.expected {
				p.finished = false
				p.engineTell("resume")
			} else {
				p.notice = "Cannot resume playback"
			}
		}
	case "n", ">":
		if p.live != nil {
			p.notice = "Live broadcast. Use /radio to switch stations."
		} else if p.radio != nil {
			p.advanceRadio()
		} else {
			p.leaving("skip")
			p.advance()
		}
	case "p", "<":
		if p.live != nil {
			p.notice = "Live broadcast. Use /radio to switch stations."
		} else if p.active != nil && p.active.radio && p.radio != nil {
			p.previousRadio()
		} else if p.active != nil && p.playingIndex > 0 {
			p.leaving("choose")
			p.start(p.active, p.playingIndex-1)
		}
	}
}

func (p *player) handleSeek(key string) {
	if p.jamTransport(key) {
		return
	}
	if p.live != nil {
		p.notice = "Live broadcast has no seek"
	} else if p.active != nil && !p.restored {
		state := audioState()
		delta := 10.0
		if key == "[" {
			delta = -10
		}
		if p.finished {
			state.elapsed = state.duration
		}
		to := max(0, min(state.elapsed+delta, state.duration-0.01))
		p.cancelTransition()
		seekAudio(to)
		p.engine.tell("seek", "pos", to)
		p.finished = false
		p.nextAnalysis = time.Time{}
	}
}

func (p *player) move(delta int) {
	if p.trackPane {
		p.trackIndex = max(0, min(len(p.tracks())-1, p.trackIndex+delta))
	} else {
		p.albumIndex = max(0, min(len(p.results)-1, p.albumIndex+delta))
		p.trackIndex = 0
	}
}

// nextAlbumIndex consumes a shuffled order without replaying a song in the same cycle.
func (p *player) nextAlbumIndex(a *album, current int) int {
	if a == nil || !p.shuffle {
		return current + 1
	}
	if p.shuffledAlbum != a {
		p.shuffledAlbum = a
		p.shuffled = rand.Perm(len(a.tracks))
	}
	for len(p.shuffled) > 0 {
		i := p.shuffled[0]
		p.shuffled = p.shuffled[1:]
		if i != current {
			return i
		}
	}
	return len(a.tracks)
}

func (p *player) cancelTransition() {
	if p.nextPrepared {
		cancelNextAudio()
		p.nextPrepared = false
	}
}

func (p *player) prepareTransition() {
	i := p.playingIndex + 1
	if p.inJam() || !p.expected || p.active.radio || p.live != nil || p.melisma ||
		p.fromQueue || len(p.queue) > 0 || p.shuffle || p.repeat == "track" || i >= len(p.active.tracks) {
		p.cancelTransition()
		return
	}
	if !p.state.playing || p.nextPrepared {
		return
	}
	remaining := p.state.duration - p.state.elapsed
	overlap := 0.0
	window := 1.0
	if p.crossfade && crossfadeSupported {
		overlap, window = 3, 3
	}
	if remaining > window || remaining <= 0 {
		return
	}
	next := p.active.tracks[i]
	if prepareNextAudio(next.path, p.trackVolume(next), overlap) {
		p.nextPrepared = true
	}
}
