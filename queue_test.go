package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func testWAV(t *testing.T, dir, name string) string {
	t.Helper()
	wav := make([]byte, 44+8000*2)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 8000)
	binary.LittleEndian.PutUint32(wav[28:], 16000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], 16000)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, wav, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func queueFixture(t *testing.T) (*player, *album, *album) {
	dir := t.TempDir()
	a := &album{title: "A"}
	b := &album{title: "B"}
	for _, name := range []string{"a1", "a2", "a3"} {
		a.tracks = append(a.tracks, track{path: testWAV(t, dir, name+".wav"), title: name, source: a})
	}
	for _, name := range []string{"b1", "b2"} {
		b.tracks = append(b.tracks, track{path: testWAV(t, dir, name+".wav"), title: name, source: b})
	}
	p := &player{albums: []*album{a, b}, results: []*album{a, b}, volume: 70, likedSongs: &album{likedSongs: true}}
	t.Cleanup(stopAudio)
	return p, a, b
}

func TestQKeyTogglesQueueInsteadOfQuitting(t *testing.T) {
	p, a, _ := queueFixture(t)
	p.trackPane, p.trackIndex = true, 1
	if !p.handle("q") || len(p.queue) != 1 || p.queue[0].title != "a2" {
		t.Fatalf("q should queue the selected track, queue=%v", p.queue)
	}
	if !p.handle("q") || len(p.queue) != 0 {
		t.Fatalf("second q should remove it, queue=%v", p.queue)
	}
	p.trackPane = false
	p.handle("q")
	if len(p.queue) != len(a.tracks) {
		t.Fatalf("q on an album should queue all %d tracks, got %d", len(a.tracks), len(p.queue))
	}
	p.handle("q")
	if len(p.queue) != 0 {
		t.Fatalf("q on a fully queued album should unqueue it, got %d", len(p.queue))
	}
}

func TestColonQQuits(t *testing.T) {
	p, _, _ := queueFixture(t)
	for _, key := range []string{":", "q"} {
		if !p.handle(key) {
			t.Fatalf("%q quit the player", key)
		}
	}
	if p.handle("\r") {
		t.Fatal(":q should quit")
	}
	p.handle(":")
	p.handle("x")
	if !p.handle("\r") || p.notice != "Unknown command :x" {
		t.Fatalf("unknown command: %q", p.notice)
	}
	p.handle(":")
	if !p.handle("\x1b") || p.command != nil {
		t.Fatal("Esc should cancel the command prompt")
	}
}

func TestQueuePlaysInOrderThenResumesAlbum(t *testing.T) {
	p, a, b := queueFixture(t)
	if !p.start(a, 0) {
		t.Fatal("could not start album")
	}
	p.queue = []track{b.tracks[1], b.tracks[0]}
	for _, want := range []string{"b2", "b1", "a2", "a3"} {
		if !p.advance() || p.current().title != want {
			t.Fatalf("wanted %s, got %v", want, p.current())
		}
	}
	if p.fromQueue || p.advance() {
		t.Fatal("album should end normally after the queue is done")
	}
}

func TestSlashQQueueViewRemovesWithQ(t *testing.T) {
	p, a, b := queueFixture(t)
	p.queue = []track{a.tracks[0], b.tracks[0], b.tracks[1]}
	p.handle("/")
	for _, key := range []string{"q", "\r"} {
		p.handle(key)
	}
	if p.queueView == nil {
		t.Fatal("/q should open the queue popup")
	}
	p.handle("\x1b[B")
	p.handle("q")
	if len(p.queue) != 2 || p.queue[1].title != "b2" {
		t.Fatalf("q in the popup should remove the selected entry, queue=%v", p.queue)
	}
	p.handle("q")
	p.handle("q")
	if len(p.queue) != 0 || p.queueView.index != 0 {
		t.Fatalf("queue=%v index=%d", p.queue, p.queueView.index)
	}
	p.handle("\x1b")
	if p.queueView != nil {
		t.Fatal("Esc should close the popup")
	}
}
