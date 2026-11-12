package app

import (
	"elsewhere/internal/audio"
	"path/filepath"
	"testing"
)

func TestRestoreQueuedSessionAndFinder(t *testing.T) {
	a := &album{title: "Intervals", artist: "Test Artist"}
	a.tracks = []track{
		{path: "/one", title: "Opening", artist: a.artist, likeID: "one", source: a},
		{path: "/two", title: "Second Movement", artist: a.artist, likeID: "two", source: a},
	}
	p := player{
		path: "/music", sessionPath: filepath.Join(t.TempDir(), "session.json"),
		albums: []*album{a}, likedSongs: &album{title: "Liked Songs"},
		active: a, restored: true, volume: 55, repeat: "album",
		state: audio.Playback{Elapsed: 12.5, Duration: 120},
	}
	p.enqueue(a.tracks, false)
	p.enqueue(a.tracks[1:], true)
	if err := p.saveSession(); err != nil {
		t.Fatal(err)
	}
	p.active, p.queue, p.restored, p.volume = nil, nil, false, 70
	p.restoreSession()
	if !p.restored || p.active != a || p.state.Elapsed != 12.5 || p.volume != 55 || p.repeat != "album" {
		t.Fatalf("incorrect session restore: %#v", p)
	}
	if len(p.queue) != 2 || p.queue[0].path != "/two" || p.queue[1].path != "/one" {
		t.Fatalf("incorrect queue: %#v", p.queue)
	}
	p.finder = &libraryFinder{query: "second movement"}
	p.findMusic()
	p.handleFinder("\x1b[C")
	if p.finder != nil || p.results[p.albumIndex] != a || !p.trackPane || p.trackIndex != 1 {
		t.Fatalf("search did not reveal the matching song: %#v", p)
	}

	p.queueView = &queueView{}
	p.playlistPath = filepath.Join(t.TempDir(), "playlists.json")
	p.handleQueueView("J")
	p.handleQueueView("S")
	if p.playlistDialog == nil {
		t.Fatal("save did not open the playlist dialog")
	}
	p.playlistDialog.name = "Saved Queue"
	p.handlePlaylist("\r")
	if len(p.playlistRecords) != 1 || len(p.playlistRecords[0].Tracks) != 2 || p.playlistRecords[0].Tracks[0] != "one" {
		t.Fatalf("saved queue order: %#v", p.playlistRecords)
	}
}
