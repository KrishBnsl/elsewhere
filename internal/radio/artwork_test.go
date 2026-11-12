package radio

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRadioGameArtwork(t *testing.T) {
	const song = "Nintendo Game Boy/Example/Theme.gbs"
	picture := image.NewRGBA(image.Rect(0, 0, 1, 1))
	picture.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var data bytes.Buffer
	if err := png.Encode(&data, picture); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/metadata":
			if r.URL.Query().Get("path") != song {
				t.Errorf("unexpected track path %q", r.URL.Query().Get("path"))
			}
			w.Write([]byte(`{"imageUrl":"Nintendo%20Game%20Boy/Example/game%20art.png"}`))
		case "/catalog/Nintendo Game Boy/Example/game art.png":
			w.Write(data.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	art := GameArtwork(context.Background(), server.Client(), server.URL, song)
	if art == nil || art.Pixels[0][0] != [4]byte{255, 0, 0, 255} {
		t.Fatal("game artwork did not decode")
	}
	fallback := StationArtwork()
	if fallback == nil || len(fallback.PNG) == 0 || fallback.Pixels[0][0] == fallback.Pixels[24][24] {
		t.Fatal("station artwork is missing")
	}
}
