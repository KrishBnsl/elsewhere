package radio

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
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
