package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCoverReleaseMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if query := r.URL.Query().Get("query"); !strings.Contains(query, `artistname:"Test Artist"`) {
			t.Errorf("artist missing from search: %q", query)
		}
		w.Write([]byte(`{"release-groups":[
			{"id":"11111111-1111-1111-1111-111111111111","title":"Test Album","artist-credit":[{"name":"Wrong Artist"}]},
			{"id":"22222222-2222-2222-2222-222222222222","title":"Test Album","artist-credit":[{"name":"Test Artist"}]}
		]}`))
	}))
	defer server.Close()
	got, err := findReleaseGroup(context.Background(), server.Client(), server.URL, "Test Album", "Test Artist")
	if err != nil || got != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("matched release group %q: %v", got, err)
	}
}

func TestDownloadedCoverDecoding(t *testing.T) {
	var data bytes.Buffer
	picture := image.NewRGBA(image.Rect(0, 0, 1, 1))
	picture.Set(0, 0, color.RGBA{255, 0, 0, 255})
	if err := png.Encode(&data, picture); err != nil {
		t.Fatal(err)
	}
	art := readCoverBytes(data.Bytes())
	if art == nil || art.pixels[0][0] != [4]byte{255, 0, 0, 255} {
		t.Fatal("downloaded image did not render as red cover art")
	}
}

func TestCoverDownloadRetriesServerFailure(t *testing.T) {
	var data bytes.Buffer
	picture := image.NewRGBA(image.Rect(0, 0, 300, 300))
	if err := png.Encode(&data, picture); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write(data.Bytes())
	}))
	defer server.Close()
	art, _, err := downloadCover(context.Background(), server.Client(), server.URL)
	if err != nil || art == nil || attempts != 2 {
		t.Fatalf("download failed after %d attempts: %v", attempts, err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(art.png))
	if err != nil || config.Width != 300 || config.Height != 300 {
		t.Fatalf("native cover dimensions = %dx%d: %v", config.Width, config.Height, err)
	}
}

func TestCoverArchiveMirrorUsesItemStorage(t *testing.T) {
	const item = "mbid-1fdd5c7f-e475-4b3f-ab2a-5f3fc4070e38"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metadata/"+item {
			t.Errorf("unexpected metadata path: %q", r.URL.Path)
		}
		w.Write([]byte(`{"server":"ia601808.us.archive.org","dir":"/30/items/` + item + `"}`))
	}))
	defer server.Close()

	file := item + "-28487695145_thumb250.jpg"
	failed := "https://dn710003.ca.archive.org/0/items/" + item + "/" + file
	want := "https://ia601808.us.archive.org/30/items/" + item + "/" + file
	if got := archiveCoverMirror(context.Background(), server.Client(), server.URL, failed); got != want {
		t.Fatalf("got mirror %q, want %q", got, want)
	}
}

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
	if err := writeLocalFile(filepath.Join(cache, "Elsewhere", "covers", fmt.Sprintf("%x.img", key)), data.Bytes()); err != nil {
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
