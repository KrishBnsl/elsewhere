package main

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestRainwaveArtworkUsesSizedJPEG(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api4/info":
			w.Write([]byte(`{"sched_current":{"songs":[{"title":"Town Theme","albums":[{"name":"Game","art":"/album_art/1_2"}]}]}}`))
		case "/album_art/1_2_320.jpg":
			w.Write(data.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	info := rainwaveNowPlaying(context.Background(), server.Client(), server.URL, 1, "")
	if info.title != "Town Theme" || info.album != "Game" || info.art == nil {
		t.Fatalf("unexpected Rainwave artwork result: %+v", info)
	}
}

func TestAnimeGatewayAlbumArtwork(t *testing.T) {
	info := listenTrack([]byte(`{"op":1,"t":"TRACK_UPDATE","d":{"song":{
		"title":"Opening Theme","artists":[{"name":"歌手","nameRomaji":"Singer"}],
		"albums":[{"name":"Soundtrack","image":"cover art.jpg"}]}}}`))
	if info.title != "Opening Theme" || info.artist != "Singer" ||
		info.album != "Soundtrack" || info.artPath != listenCovers+"cover%20art.jpg" {
		t.Fatalf("unexpected Anime FM artwork result: %+v", info)
	}
}

func TestAnimeGatewayFallsBackToSourceAndArtistArtwork(t *testing.T) {
	info := listenTrack([]byte(`{"op":1,"t":"TRACK_UPDATE","d":{"song":{
		"title":"Song","artists":[{"name":"Band","image":"band.jpg"}],
		"albums":[{"name":"Single","image":null}],
		"sources":[{"nameRomaji":"Show","image":"show_image.jpg"}]}}}`))
	want := []string{listenSources + "show_image.jpg", listenArtists + "band.jpg"}
	if !reflect.DeepEqual(info.artPaths, want) || info.artPath != want[0] {
		t.Fatalf("artwork candidates = %v (%s), want %v", info.artPaths, info.artPath, want)
	}
	info = listenTrack([]byte(`{"op":1,"t":"TRACK_UPDATE","d":{"song":{"title":"Song",
		"albums":[{"image":"../escape.jpg"},{"image":"a\\b.jpg"}],"artists":[],"sources":[]}}}`))
	if len(info.artPaths) != 0 {
		t.Fatalf("unsafe artwork names were accepted: %v", info.artPaths)
	}
}

func TestFetchListenArtRetriesAndFallsBack(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	var flaky atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing.jpg":
			http.NotFound(w, r)
		case "/flaky.jpg":
			if flaky.Add(1) == 1 {
				http.Error(w, "busy", http.StatusBadGateway)
				return
			}
			w.Write(data.Bytes())
		}
	}))
	defer server.Close()

	found, art := fetchListenArt(context.Background(), server.Client(), []string{server.URL + "/missing.jpg", server.URL + "/flaky.jpg"})
	if art == nil || found != server.URL+"/flaky.jpg" || flaky.Load() != 2 {
		t.Fatalf("got %q, %v after %d tries", found, art, flaky.Load())
	}
}

func TestRadioNowPlayingParsesArtistTitleAndDJ(t *testing.T) {
	info, avatar := radioTrack([]byte(`{"main":{"np":"Hiroshi Kitadani - We Go! - TV Size","dj":{"djname":"kipukun","djimage":"45-a.png"}}}`))
	if info.artist != "Hiroshi Kitadani" || info.title != "We Go! - TV Size" || info.album != "DJ kipukun" || avatar != "45-a.png" {
		t.Fatalf("unexpected R/a/dio track: %+v %q", info, avatar)
	}
	info, avatar = radioTrack([]byte(`{"main":{"np":"Untitled","dj":{"djimage":"../x.png"}}}`))
	if info.title != "Untitled" || info.artist != "" || avatar != "" {
		t.Fatalf("unexpected fallback track: %+v %q", info, avatar)
	}
	if info, _ = radioTrack([]byte(`{"main":{"np":""}}`)); info.title != "" {
		t.Fatalf("empty now-playing produced %+v", info)
	}
}
