package app

import (
	"bytes"
	"crypto/sha256"
	"elsewhere/internal/fsutil"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCoverPromptSavesOnlyAfterAcceptance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	folder := t.TempDir()
	a := &album{title: "Test Album", artist: "Test Artist"}
	a.tracks = []track{{path: filepath.Join(folder, "Song.flac"), source: a}}
	p := &player{active: a, popup: true}
	t.Cleanup(p.stopCoverLookup)

	var data bytes.Buffer
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if err := png.Encode(&data, picture); err != nil {
		t.Fatal(err)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	key := sha256.Sum256([]byte("test artist\x00test album"))
	if err := fsutil.WriteFile(filepath.Join(cache, "Elsewhere", "covers", fmt.Sprintf("%x.img", key)), data.Bytes()); err != nil {
		t.Fatal(err)
	}

	p.updateCover()
	if p.coverPrompt != a || p.coverLookup.result != nil {
		t.Fatal("missing confirmation or lookup started before consent")
	}
	p.handle("n")
	p.updateCover()
	if p.coverPrompt != nil || a.cover != nil {
		t.Fatal("declining did not dismiss the prompt")
	}
	destination := filepath.Join(folder, "cover.png")
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("declining created a file: %v", err)
	}

	p.handle("c")
	p.updateCover()
	p.handle("y")
	p.popup = false
	deadline := time.Now().Add(3 * time.Second)
	for p.coverLookup.result != nil && time.Now().Before(deadline) {
		p.updateCover()
		time.Sleep(time.Millisecond)
	}
	if a.cover == nil {
		t.Fatalf("cover did not refresh: %s", p.notice)
	}
	saved, err := os.ReadFile(destination)
	if err != nil || !bytes.HasPrefix(saved, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("saved cover is missing or invalid: %v", err)
	}
	if _, err := saveAlbumCover(folder, a.cover, data.Bytes()); err == nil {
		t.Fatal("existing cover was overwritten")
	}
}
