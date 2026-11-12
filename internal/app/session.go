package app

import (
	"elsewhere/internal/audio"
	"elsewhere/internal/fsutil"
	"encoding/json"
	"os"
)

type savedSession struct {
	Library, Current, Collection, Continue         string
	Playlist, Liked, FromQueue, Shuffle, Crossfade bool
	Position, Duration                             float64
	Volume                                         int
	Repeat, Gain                                   string
	Queue                                          []string
	History                                        []listenEntry `json:",omitempty"`
}

func (p *player) saveSession() error {
	s := savedSession{Library: p.path, Volume: p.volume, Repeat: p.repeat, Shuffle: p.shuffle, Gain: p.gain, Crossfade: p.crossfade, History: p.listenHistory}
	if t := p.localSong(); t != nil {
		s.Current, s.Position = t.path, p.state.Elapsed
		s.Duration = p.state.Duration
		if !p.restored {
			state := audio.State()
			s.Position, s.Duration = state.Elapsed, state.Duration
			if p.finished {
				s.Position = 0
			}
		}
		s.Collection, s.Playlist, s.Liked = p.active.title, p.active.isPlaylist, p.active.likedSongs
		s.FromQueue = p.fromQueue
		if p.resumeAlbum != nil && p.resumeIndex < len(p.resumeAlbum.tracks) {
			s.Continue = p.resumeAlbum.tracks[p.resumeIndex].path
		}
	}
	for _, t := range p.queue {
		s.Queue = append(s.Queue, t.path)
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return fsutil.WriteFile(p.sessionPath, data)
}

func (p *player) restoreSession() {
	data, err := os.ReadFile(p.sessionPath)
	var s savedSession
	if err != nil || json.Unmarshal(data, &s) != nil || s.Library != p.path {
		return
	}
	p.listenHistory = s.History[max(0, len(s.History)-maxListenHistory):]
	p.volume, p.repeat, p.shuffle, p.gain = min(100, max(0, s.Volume)), s.Repeat, s.Shuffle, s.Gain
	for _, path := range s.Queue {
		if a, i := p.findTrack(path); a != nil {
			p.queue = append(p.queue, a.tracks[i])
		}
	}
	a, i := p.findTrack(s.Current)
	collections := p.playlists
	if s.Liked {
		collections = []*album{p.likedSongs}
	}
	if s.Playlist || s.Liked {
		for _, candidate := range collections {
			if candidate.title != s.Collection {
				continue
			}
			for j, t := range candidate.tracks {
				if t.path == s.Current {
					a, i = candidate, j
					break
				}
			}
		}
	}
	if a == nil {
		return
	}
	// Defer opening the audio file until the listener presses Space.
	p.active, p.playingIndex, p.restored = a, i, true
	p.state.Elapsed, p.state.Duration = max(0, s.Position), max(0, s.Duration)
	p.crossfade = s.Crossfade
	p.notice = "Session restored · Space to resume"
	p.fromQueue = s.FromQueue
	p.resumeAlbum, p.resumeIndex = p.findTrack(s.Continue)
}
