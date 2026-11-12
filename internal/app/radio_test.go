package app

import (
	"context"
	"elsewhere/internal/audio"
	"elsewhere/internal/radio"
	"encoding/binary"
	"os"
	"testing"
)

func TestRadioPreviousAndForwardUseHistory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	t.Setenv("TMPDIR", t.TempDir())
	p := &player{radio: &radioSession{ctx: ctx, cancel: cancel}}
	t.Cleanup(func() {
		audio.Stop()
		p.stopRadio()
	})

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

	for _, title := range []string{"First tune", "Second tune"} {
		result := make(chan radio.Song, 1)
		result <- radio.Song{Title: title, Game: title, Format: "NSF", WAV: wav}
		p.radio.result, p.radio.wanted = result, true
		p.tickRadio()
		if current := p.current(); current == nil || current.title != title {
			t.Fatalf("wanted %q, got %v (%s)", title, current, p.notice)
		}
	}
	p.handle("<")
	if got := p.current().title; got != "First tune" {
		t.Fatalf("previous played %q", got)
	}
	p.handle(">")
	if got := p.current().title; got != "Second tune" {
		t.Fatalf("forward played %q", got)
	}

	files := []string{p.radio.history[0].tracks[0].path, p.radio.history[1].tracks[0].path}
	p.toggleRadio()
	for _, file := range files {
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Errorf("radio history file remained after stopping: %s (%v)", file, err)
		}
	}
}
