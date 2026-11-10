package main

import (
	"fmt"
	"strings"
)

type commandPrompt struct{ input string }

type queueView struct {
	index int
	// What Melisma would play once the queue is done (a forecast).
	upcoming []enginePick
}

// handleCommand runs a ":" command. It reports false when the player should quit.
func (p *player) handleCommand(key string) bool {
	if p.movePalette(key) {
		return true
	}
	switch key {
	case "\r", "\n":
		command := strings.ToLower(strings.TrimSpace(p.command.input))
		if selected := p.selectedCommand(); selected != "" {
			command = selected
		}
		p.command = nil
		switch command {
		case "":
		case "q", "quit", "q!":
			return false
		case "themes", "theme", "radio", "queue", "melisma", "engine", "taste", "profile", "analyze":
			prompt := searchPrompt{previousQuery: p.query, albumIndex: p.albumIndex, trackIndex: p.trackIndex}
			p.runSlashCommand(command, prompt)
		default:
			p.notice = "Unknown command :" + command
		}
	case "\x1b":
		p.command = nil
	default:
		if editText(&p.command.input, key, 64) {
			p.paletteIndex = -1
		}
	}
	return true
}

func queueable(t track) bool {
	return t.path != "" && t.radioFormat == ""
}

func (p *player) queueIndex(path string) int {
	for i, t := range p.queue {
		if t.path == path {
			return i
		}
	}
	return -1
}

// toggleQueue adds the selected track (or every track of the selected album) to the queue,
// or removes them when they are all queued already. In Now Playing it acts on the playing track.
func (p *player) toggleQueue() {
	var targets []track
	switch {
	case p.popup:
		if t := p.current(); t != nil {
			targets = []track{*t}
		}
	case p.trackPane:
		if tracks := p.tracks(); p.trackIndex < len(tracks) {
			targets = []track{tracks[p.trackIndex]}
		}
	case len(p.results) > 0:
		targets = p.results[p.albumIndex].tracks
	}
	var usable []track
	for _, t := range targets {
		if queueable(t) {
			usable = append(usable, t)
		}
	}
	if len(usable) == 0 {
		if p.popup && (p.live != nil || p.active != nil && p.active.radio) {
			p.notice = "Radio can't be queued"
		}
		return
	}
	missing := 0
	for _, t := range usable {
		if p.queueIndex(t.path) < 0 {
			missing++
		}
	}
	if missing == 0 {
		for _, t := range usable {
			p.removeQueued(p.queueIndex(t.path))
		}
		if len(usable) == 1 {
			p.notice = "Removed from queue: " + usable[0].title
		} else {
			p.notice = fmt.Sprintf("Removed %d tracks from queue", len(usable))
		}
		return
	}
	for _, t := range usable {
		if p.queueIndex(t.path) < 0 {
			p.queue = append(p.queue, t)
		}
	}
	if len(usable) == 1 {
		p.notice = fmt.Sprintf("Queued: %s (%d in queue)", usable[0].title, len(p.queue))
	} else {
		p.notice = fmt.Sprintf("Queued %d tracks (%d in queue)", missing, len(p.queue))
	}
}

func (p *player) removeQueued(i int) {
	if i < 0 || i >= len(p.queue) {
		return
	}
	p.queue = append(p.queue[:i:i], p.queue[i+1:]...)
	if p.queueView != nil {
		p.queueView.index = max(0, min(p.queueView.index, len(p.queue)-1))
	}
}

// playQueued plays a queued track. The first one remembers where to continue once the queue ends.
func (p *player) playQueued(t track) bool {
	source, index := t.source, -1
	if source != nil {
		for i, candidate := range source.tracks {
			if candidate.path == t.path {
				index = i
				break
			}
		}
	}
	if index < 0 {
		source = &album{title: t.album, artist: t.albumArtist, tracks: []track{t}}
		source.tracks[0].source = source
		index = 0
	}
	resumeAlbum, resumeIndex := p.active, p.playingIndex+1
	if p.live != nil || p.radio != nil || resumeAlbum != nil && resumeAlbum.radio {
		resumeAlbum = nil
	}
	if !p.start(source, index) {
		return false
	}
	p.stopRadio()
	p.clearLiveStation()
	if !p.fromQueue {
		p.resumeAlbum, p.resumeIndex = resumeAlbum, resumeIndex
	}
	p.fromQueue = true
	return true
}

// advance moves to the next queued track, then back to the interrupted album, then through the album.
func (p *player) advance() bool {
	for len(p.queue) > 0 {
		t := p.queue[0]
		p.removeQueued(0)
		if p.playQueued(t) {
			return true
		}
	}
	if p.fromQueue {
		a, i := p.resumeAlbum, p.resumeIndex
		p.leaveQueue()
		if a != nil || !p.melisma {
			return p.start(a, i)
		}
	}
	if p.melisma && p.engine != nil {
		return p.playMelisma()
	}
	if p.start(p.active, p.playingIndex+1) {
		return true
	}
	// The album is over: Melisma carries on from here.
	if p.engine != nil && p.active != nil && !p.active.radio && p.live == nil {
		if p.playMelisma() {
			p.notice = "Album over · Melisma carries on"
			return true
		}
	}
	return false
}

func (p *player) leaveQueue() {
	p.fromQueue, p.resumeAlbum, p.resumeIndex = false, nil, 0
}

func (p *player) handleQueueView(key string) {
	view := p.queueView
	switch key {
	case "\x1b":
		p.queueView = nil
	case "\x1b[A", "k":
		view.index = max(0, view.index-1)
	case "\x1b[B", "j":
		view.index = min(max(0, len(p.queue)+len(view.upcoming)-1), view.index+1)
	case "q":
		if view.index < len(p.queue) {
			title := p.queue[view.index].title
			p.removeQueued(view.index)
			p.notice = "Removed from queue: " + title
			p.refreshForecast()
		}
	case "\r", "\n":
		if i := view.index - len(p.queue); i >= 0 && i < len(view.upcoming) {
			// A forecast song: play it now; Melisma carries on from it.
			pick := view.upcoming[i]
			p.queueView = nil
			p.leaving("choose")
			if p.playPick(pick) {
				p.popup = true
			}
			return
		}
		if view.index < len(p.queue) {
			t := p.queue[view.index]
			p.removeQueued(view.index)
			p.queueView = nil
			p.leaving("choose")
			if p.playQueued(t) {
				p.popup = true
			}
		}
	}
}

func (p *player) overlayQueue(rows []string, width int) {
	columns := min(75, width-4)
	heading := fmt.Sprintf(" QUEUE (%d) ", len(p.queue))
	row := func(text, style string) string {
		return boxSide(style + cell(text, columns-2) + panel + amber)
	}
	dialog := []string{boxTop(heading, columns)}
	upcoming := p.queueView.upcoming
	if len(p.queue) == 0 && len(upcoming) == 0 {
		dialog = append(dialog, row(" Queue is empty. Press Q on a track to add it.", dim))
	}
	const visible = 10
	first := max(0, min(p.queueView.index-visible/2, len(p.queue)-visible))
	for i := first; i < min(len(p.queue), first+visible); i++ {
		t := p.queue[i]
		text, style := "   ", dim
		if i == p.queueView.index {
			text, style = " › ", amber
		}
		text += fmt.Sprintf("%2d. %s / %s", i+1, t.title, t.artist)
		dialog = append(dialog, row(text, style))
	}
	if len(upcoming) > 0 {
		label := " THEN MELISMA · if you listen through"
		if len(p.queue) == 0 {
			label = " MELISMA NEXT · if you listen through"
		}
		dialog = append(dialog, row("", dim), row(label, amber))
		for i, s := range upcoming {
			text, style := fmt.Sprintf("   %2d. %s / %s", i+1, s.Title, s.Artist), dim
			if len(p.queue)+i == p.queueView.index {
				text, style = fmt.Sprintf(" › %2d. %s / %s", i+1, s.Title, s.Artist), amber
			}
			dialog = append(dialog, row(text, style))
			if i == 0 && len(s.Why) > 0 {
				dialog = append(dialog, row("       ↳ "+s.Why[0], dim))
			}
		}
	} else if p.engine != nil && !p.melisma {
		dialog = append(dialog, row("", dim), row(" /melisma lets Melisma choose after this", dim))
	}
	dialog = append(dialog, row(" ↑↓ Move  Q Remove  Enter Play now  Esc Close", dim),
		boxBottom(columns))
	x, y := (width-columns)/2+1, max(0, (len(rows)-len(dialog))/2)
	cursor := fmt.Sprintf("\x1b[%dG", x)
	for i, line := range dialog {
		if y+i < len(rows) {
			rows[y+i] += cursor + panel + amber + line + reset
		}
	}
}
