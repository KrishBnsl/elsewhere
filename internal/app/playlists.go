package app

import (
	"elsewhere/internal/fsutil"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type playlist struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tracks      []string `json:"tracks,omitempty"`
}

func readPlaylists(path string) ([]playlist, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var lists []playlist
	if err := json.Unmarshal(data, &lists); err != nil {
		return nil, err
	}
	return lists, nil
}

func editPlaylists(path string, edit func(*[]playlist) error) ([]playlist, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err := fsutil.LockFile(lock); err != nil {
		return nil, fmt.Errorf("playlist store busy: %w", err)
	}
	lists, err := readPlaylists(path)
	if err != nil {
		return nil, err
	}
	if err := edit(&lists); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(lists, "", "  ")
	if err != nil {
		return nil, err
	}
	return lists, fsutil.WriteFile(path, data)
}

type searchPrompt struct {
	previousQuery          string
	albumIndex, trackIndex int
	track                  *track
	input                  string
}

type playlistDialog struct {
	tracks                    []string
	tapeCard                  string
	kind                      string
	name, description, filter string
	descriptionFocused        bool
	index                     int
	target                    *track
	deleteTarget              *playlist
	errorText                 string
}

func (p *player) rebuildPlaylists() {
	tracks := make(map[string]track)
	for _, a := range p.albums {
		for _, t := range a.tracks {
			tracks[t.likeID] = t
		}
	}
	p.playlists = nil
	for _, list := range p.playlistRecords {
		a := &album{title: list.Name, description: list.Description, isPlaylist: true}
		for _, id := range list.Tracks {
			if t, ok := tracks[id]; ok {
				a.tracks = append(a.tracks, t)
			}
		}
		p.playlists = append(p.playlists, a)
	}
}

func (p *player) beginSearch() {
	target := p.current()
	if !p.popup {
		target = nil
		if tracks := p.tracks(); len(tracks) > 0 {
			target = &tracks[p.trackIndex]
		}
	}
	p.searchPrompt = searchPrompt{p.query, p.albumIndex, p.trackIndex, target, ""}
	p.searching = true
	p.paletteIndex = -1
}

func editText(value *string, key string, limit int) bool {
	switch key {
	case "\x7f", "\b":
		if *value != "" {
			_, size := utf8.DecodeLastRuneInString(*value)
			*value = (*value)[:len(*value)-size]
		}
	default:
		if key == "" || len(*value)+len(key) > limit || strings.IndexFunc(key, unicode.IsControl) != -1 {
			return false
		}
		*value += key
	}
	return true
}

func (p *player) handleSearch(key string) {
	prompt := &p.searchPrompt
	if p.movePalette(key) {
		return
	}
	switch key {
	case "\r", "\n":
		command := strings.TrimSpace(prompt.input)
		if selected := p.selectedCommand(); selected != "" {
			command = selected
		}
		p.searching = false
		if !p.runSlashCommand(command, *prompt) && !p.popup {
			p.query = prompt.input
			p.search()
		}
	case "\x1b":
		p.searching = false
		if !p.popup {
			p.query = ""
			p.search()
		}
	default:
		if editText(&prompt.input, key, 512) {
			p.paletteIndex = -1
			if !p.popup {
				p.query = prompt.input
				p.search()
			}
		}
	}
}

func (p *player) handlePlaylist(key string) {
	d := p.playlistDialog
	if target := d.deleteTarget; target != nil {
		switch key {
		case "\x1b":
			d.deleteTarget, d.errorText = nil, ""
		case "\r", "\n":
			p.savePlaylist(func(lists *[]playlist) error {
				for i, list := range *lists {
					if list.Name != target.Name {
						continue
					}
					if list.Description != target.Description || !slices.Equal(list.Tracks, target.Tracks) {
						return fmt.Errorf("playlist changed in another window; restart Elsewhere and try again")
					}
					*lists = slices.Delete(*lists, i, i+1)
					return nil
				}
				return fmt.Errorf("playlist no longer exists")
			}, "Deleted playlist "+target.Name)
		}
		return
	}
	if key == "\x1b" {
		p.playlistDialog = nil
		return
	}
	if d.kind == "np" {
		switch key {
		case "\t", "\x1b[Z", "\x1b[A", "\x1b[B":
			d.descriptionFocused = !d.descriptionFocused
		case "\r", "\n":
			name := strings.TrimSpace(d.name)
			if name == "" {
				d.errorText = "Enter a playlist name"
				return
			}
			p.savePlaylist(func(lists *[]playlist) error {
				if strings.EqualFold(name, "Liked Songs") || slices.ContainsFunc(*lists, func(list playlist) bool {
					return strings.EqualFold(list.Name, name)
				}) {
					return fmt.Errorf("that playlist name is already used")
				}
				*lists = append(*lists, playlist{Name: name, Description: strings.TrimSpace(d.description), Tracks: d.tracks})
				return nil
			}, "Created playlist "+name)
		default:
			if d.descriptionFocused {
				editText(&d.description, key, 512)
			} else {
				editText(&d.name, key, 120)
			}
		}
		return
	}

	switch key {
	case "\x1b[A":
		d.index = max(0, d.index-1)
	case "\x1b[B":
		d.index = min(max(0, len(p.matchPlaylists(d.filter))-1), d.index+1)
	case "\r", "\n":
		matches := p.matchPlaylists(d.filter)
		if d.kind == "dp" {
			if d.index < len(matches) {
				d.deleteTarget = &matches[d.index]
				d.errorText = ""
			}
			return
		}
		if d.target == nil || d.target.radioFormat != "" {
			d.errorText = "Select or play a local song first"
			return
		}
		if d.index >= len(matches) {
			return
		}
		name := matches[d.index].Name
		p.savePlaylist(func(lists *[]playlist) error {
			for i := range *lists {
				list := &(*lists)[i]
				if list.Name == name {
					if !slices.Contains(list.Tracks, d.target.likeID) {
						list.Tracks = append(list.Tracks, d.target.likeID)
					}
					return nil
				}
			}
			return fmt.Errorf("playlist no longer exists")
		}, "Saved to "+name)
	default:
		if editText(&d.filter, key, 120) {
			d.index = 0
		}
	}
}

func (p *player) savePlaylist(edit func(*[]playlist) error, notice string) {
	lists, err := editPlaylists(p.playlistPath, edit)
	if err != nil {
		p.playlistDialog.errorText = err.Error()
		return
	}
	var current *album
	if len(p.results) > 0 {
		current = p.results[p.albumIndex]
	}
	index := p.trackIndex
	p.playlistRecords = lists
	p.rebuildPlaylists()
	p.search()
	for i, a := range p.results {
		if a == current || (current != nil && a.isPlaylist && current.isPlaylist && a.title == current.title) {
			p.albumIndex = i
			p.trackIndex = max(0, min(index, len(a.tracks)-1))
			break
		}
	}
	p.notice = notice
	p.playlistDialog = nil
}

func (p *player) matchPlaylists(query string) []playlist {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return p.playlistRecords
	}
	type match struct {
		list  playlist
		score int
	}
	var matches []match
	for _, list := range p.playlistRecords {
		text := strings.ToLower(list.Name + " " + list.Description)
		score := 0
		for _, word := range words {
			cost := fuzzyCost(text, word)
			if cost < 0 {
				score = -1
				break
			}
			score += cost
		}
		if score >= 0 {
			matches = append(matches, match{list, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score < matches[j].score })
	result := make([]playlist, len(matches))
	for i, m := range matches {
		result[i] = m.list
	}
	return result
}

func fuzzyCost(text, query string) int {
	if start := strings.Index(text, query); start >= 0 {
		return start
	}
	cost := len(text)
	for _, r := range query {
		at := strings.IndexRune(text, r)
		if at < 0 {
			return -1
		}
		cost += at
		text = text[at+utf8.RuneLen(r):]
	}
	return cost
}
