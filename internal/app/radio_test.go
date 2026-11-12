package app

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

func TestRadioCatalogFiltersGameFormats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/shuffle" || req.URL.Query().Get("path") != "Nintendo Game Boy" {
			t.Errorf("unexpected shuffle request: %s", req.URL)
		}
		w.Write([]byte(`{"items":["Nintendo Game Boy/Example/Theme.vgz","Nintendo Game Boy/Example/Song.mid",
			"Nintendo Game Boy/../Elsewhere.nsf","Outside/Game/Theme.nsf","/Nintendo Game Boy/Another/Game.gbs"]}`))
	}))
	defer server.Close()
	files, err := shuffledGameFiles(context.Background(), server.Client(), server.URL, "Nintendo Game Boy")
	want := []string{"Nintendo Game Boy/Example/Theme.vgz", "Nintendo Game Boy/Another/Game.gbs"}
	if err != nil || !reflect.DeepEqual(files, want) {
		t.Fatalf("got %v, %v; want %v", files, err, want)
	}
}

func TestUnpackCompressedVGM(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte("Vgm sample")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := unpackRadioFile(compressed.Bytes())
	if err != nil || string(data) != "Vgm sample" {
		t.Fatalf("unpack = %q, %v", data, err)
	}
}

func TestRadioPreviousAndForwardUseHistory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	t.Setenv("TMPDIR", t.TempDir())
	p := &player{radio: &radioSession{ctx: ctx, cancel: cancel}}
	t.Cleanup(func() {
		stopAudio()
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
		result := make(chan radioSong, 1)
		result <- radioSong{title: title, game: title, format: "NSF", wav: wav}
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
