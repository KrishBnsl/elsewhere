package radio

import "testing"

func TestLofiFeedsParseTheSongOnAir(t *testing.T) {
	laut := lautTrack([]byte(`{"id":1,"type":"song","title":"better","album":"","artist":{"name":"eevee"}}`))
	if laut.Title != "better" || laut.Artist != "eevee" || laut.Album != "" {
		t.Fatalf("laut.fm song = %+v", laut)
	}
	flux := fluxTrack([]byte(`{"trackInfo":{"title":"Extraction","artistCredits":"Akīn & Drxnk","artwork":"https://img.example/a.jpg","release":{"title":"Chillhop Essentials"}}}`))
	if flux.Title != "Extraction" || flux.Artist != "Akīn & Drxnk" || flux.Album != "Chillhop Essentials" || len(flux.ArtPaths) != 1 {
		t.Fatalf("FluxFM song = %+v", flux)
	}
	if bare := fluxTrack([]byte(`{"trackInfo":{"title":"X","artistCredits":"Y","artwork":null,"release":null}}`)); bare.Title != "X" || len(bare.ArtPaths) != 0 {
		t.Fatalf("FluxFM song without art = %+v", bare)
	}
	if art := LofiArtwork(); art == nil || len(art.PNG) == 0 || art.Pixels[0][0] == art.Pixels[24][24] {
		t.Fatal("lo-fi artwork is missing")
	}
}
