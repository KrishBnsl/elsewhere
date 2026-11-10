package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

type songLikes struct {
	dir     string
	entries map[string]bool
}

func readLikes(dir string) (songLikes, error) {
	likes := songLikes{dir: dir, entries: make(map[string]bool)}
	files, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return likes, nil
	}
	if err != nil {
		return likes, err
	}
	for _, file := range files {
		if file.Type().IsRegular() {
			likes.entries[file.Name()] = true
		}
	}
	return likes, nil
}

func likeKey(path string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(path)))
}

func (l songLikes) contains(key string) bool {
	return l.entries[key]
}

func (l songLikes) toggle(key string) error {
	filename := filepath.Join(l.dir, key)
	if l.entries[key] {
		if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
			return err
		}
		delete(l.entries, key)
		return nil
	}
	if err := os.MkdirAll(l.dir, 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		if err := file.Close(); err != nil {
			_ = os.Remove(filename)
			return err
		}
	} else if !os.IsExist(err) {
		return err
	}
	l.entries[key] = true
	return nil
}

func (p *player) refreshLikedSongs() {
	liked := &album{title: "Liked Songs", likedSongs: true}
	for _, a := range p.albums {
		for _, t := range a.tracks {
			if p.likes.contains(t.likeID) {
				liked.tracks = append(liked.tracks, t)
			}
		}
	}
	// Replace the list so unliking a track cannot change an active playback queue.
	p.likedSongs = liked
	p.search()
}

func (p *player) toggleLike() {
	target := p.current()
	if !p.popup {
		tracks := p.tracks()
		if len(tracks) == 0 {
			return
		}
		target = &tracks[p.trackIndex]
	}
	if target == nil {
		return
	}
	if target.radioFormat != "" {
		p.notice = "Likes are available for local songs"
		return
	}
	var selected *album
	if len(p.results) > 0 {
		selected = p.results[p.albumIndex]
	}
	trackIndex := p.trackIndex
	if p.engine != nil {
		// Melisma learns from likes too; songs it hasn't analyzed are simply skipped.
		if p.likes.contains(target.likeID) {
			p.engine.tell("unlike", "path", target.path)
		} else {
			p.engine.steer("like", target.path, audioState().elapsed)
		}
	}
	if err := p.likes.toggle(target.likeID); err != nil {
		p.notice = fmt.Sprintf("Cannot save like: %s", err)
		return
	}
	p.notice = ""
	p.refreshLikedSongs()
	for i, a := range p.results {
		if a == selected || (selected != nil && selected.likedSongs && a.likedSongs) {
			p.albumIndex = i
			p.trackIndex = max(0, min(trackIndex, len(a.tracks)-1))
			break
		}
	}
}
