package covers

import (
	"bytes"
	"context"
	"elsewhere/internal/audio"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	art := audio.ReadCoverBytes(data.Bytes())
	if art == nil || art.Pixels[0][0] != [4]byte{255, 0, 0, 255} {
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
	config, err := png.DecodeConfig(bytes.NewReader(art.PNG))
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
