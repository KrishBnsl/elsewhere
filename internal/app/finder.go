package app

import (
	"elsewhere/internal/audio"
	"fmt"
	"sort"
	"strings"
)

type libraryFinder struct {
	query   string
	index   int
	hits    []libraryHit
	outputs []audio.OutputDevice
}
type libraryHit struct {
	album *album
	index int
	label string
	cost  int
}

func (p *player) findMusic() {
	f := p.finder
	f.hits = nil
	query := strings.Join(strings.Fields(strings.ToLower(f.query)), " ")
	if query == "" {
		return
	}
	add := func(a *album, i int, label, search string) {
		search = strings.ToLower(search)
		cost := strings.Index(search, query)
		if cost < 0 {
			cost = fuzzyCost(search, query)
			if cost < 0 {
				return
			}
			cost += len(search) + 100
		}
		f.hits = append(f.hits, libraryHit{a, i, label, cost})
	}
	if p.likedSongs != nil {
		add(p.likedSongs, -1, "Playlist · "+p.likedSongs.title, p.likedSongs.title)
	}
	for _, a := range p.playlists {
		add(a, -1, "Playlist · "+a.title, a.title+" "+a.description)
	}
	for _, a := range p.albums {
		add(a, -1, "Album · "+a.title+" / "+a.artist, a.title+" "+a.artist)
		for i, t := range a.tracks {
			add(a, i, "Song · "+t.title+" / "+t.artist, t.title+" "+t.artist+" "+a.title)
		}
	}
	sort.SliceStable(f.hits, func(i, j int) bool { return f.hits[i].cost < f.hits[j].cost })
}

func (p *player) handleFinder(key string) {
	f := p.finder
	if f.outputs != nil {
		p.handleOutput(key)
		return
	}
	switch key {
	case "\x1b":
		p.finder = nil
	case "\x1b[A":
		f.index = max(0, f.index-1)
	case "\x1b[B":
		f.index = max(0, min(len(f.hits)-1, f.index+1))
	case "\r", "\n", "\t", "\x1b[C":
		if len(f.hits) == 0 {
			return
		}
		h := f.hits[f.index]
		if key == "\t" {
			targets := h.album.tracks
			if h.index >= 0 {
				targets = targets[h.index : h.index+1]
			}
			p.enqueue(targets, true)
			return
		}
		p.query = ""
		p.search()
		for i, a := range p.results {
			if a == h.album {
				p.albumIndex, p.trackIndex, p.trackPane = i, max(0, h.index), h.index >= 0
				break
			}
		}
		p.finder = nil
		p.popup = false
		if key != "\x1b[C" {
			p.playSelection()
		}
	default:
		if editText(&f.query, key, 120) {
			f.index = 0
			p.findMusic()
		}
	}
}

func (p *player) overlayFinder(rows []string, width int) {
	f := p.finder
	columns := min(width-4, 80)
	if columns < 12 || len(rows) < 6 {
		return
	}
	heading := " FIND MUSIC "
	if f.outputs != nil {
		heading = " AUDIO OUTPUT "
	}
	lines := []string{boxTop(heading, columns)}
	add := func(text, style string) { lines = append(lines, boxSide(style+cell(text, columns-2)+panel+amber)) }
	if f.outputs == nil {
		add(" "+f.query+"_", selected)
	}
	visible := min(10, len(rows)-5)
	first := max(0, min(f.index-visible/2, len(f.hits)-visible))
	for i := first; i < min(len(f.hits), first+visible); i++ {
		prefix, style := "   ", dim
		if i == f.index {
			prefix, style = " › ", selected
		}
		add(prefix+f.hits[i].label, style)
	}
	if len(f.hits) == 0 {
		add(" Type a song, artist, album or playlist", dim)
	}
	if f.outputs == nil {
		add(" ↑↓ Select  Enter Play  Tab Play next  → Go to album", dim)
	} else {
		add(" ↑↓ Select  Enter Switch  Esc Close", dim)
	}
	lines = append(lines, boxBottom(columns))
	for i, line := range lines {
		rows[(len(rows)-len(lines))/2+i] += fmt.Sprintf("\x1b[%dG", (width-columns)/2+1) + panel + amber + line + reset
	}
}

func (p *player) openOutput() {
	if p.live != nil || p.radio != nil {
		p.notice = "Stop radio before switching output"
		return
	}
	devices, err := audio.OutputDevices()
	if err != nil {
		p.notice = err.Error()
		return
	}
	if len(devices) == 0 {
		p.notice = "No audio outputs found"
		return
	}
	f := &libraryFinder{outputs: devices}
	for i, d := range devices {
		name := d.Name
		if d.Current {
			name += " · current"
			f.index = i
		}
		f.hits = append(f.hits, libraryHit{label: name})
	}
	p.finder = f
}

func (p *player) handleOutput(key string) {
	f := p.finder
	switch key {
	case "\x1b":
		p.finder = nil
	case "\x1b[A":
		f.index = max(0, f.index-1)
	case "\x1b[B":
		f.index = min(len(f.outputs)-1, f.index+1)
	case "\r", "\n":
		d := f.outputs[f.index]
		state := audio.State()
		p.cancelTransition()
		if err := audio.ChooseOutput(d.ID); err != nil {
			p.notice = err.Error()
			return
		}
		if audio.SwitchOutputRestarts && p.localSong() != nil && !p.restored && !p.finished && !audio.State().Playing && (state.Playing || state.Duration > 0) {
			t := p.current()
			if err := audio.Start(t.path, p.trackVolume(*t)); err != nil {
				p.notice = err.Error()
				p.expected = false
				return
			}
			if !state.Playing {
				audio.Pause()
			}
			audio.Seek(state.Elapsed)
			p.expected = state.Playing
		}
		p.finder = nil
		p.notice = "Output · " + d.Name
	}
}
