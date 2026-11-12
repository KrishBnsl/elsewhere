package app

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

func radioGameArtwork(ctx context.Context, client *http.Client, origin, file string) *cover {
	query := url.Values{"path": {file}}
	data, err := radioGet(ctx, client, origin+"/api/metadata?"+query.Encode(), 1<<20)
	if err != nil {
		return nil
	}
	var metadata struct {
		ImageURL string `json:"imageUrl"`
	}
	if json.Unmarshal(data, &metadata) != nil {
		return nil
	}
	asset, err := url.PathUnescape(metadata.ImageURL)
	if err != nil || asset == "" || path.Clean(asset) != asset || strings.HasPrefix(asset, "/") ||
		strings.ContainsAny(asset, ":\\") || asset == ".." || strings.HasPrefix(asset, "../") {
		return nil
	}
	imagePath := (&url.URL{Path: "/catalog/" + asset}).EscapedPath()
	data, err = radioGet(ctx, client, origin+imagePath, maxCoverBytes)
	if err != nil {
		return nil
	}
	return readCoverBytes(data)
}

func requestRadioArtwork(ctx context.Context, file string) <-chan *cover {
	if file == "" {
		return nil
	}
	result := make(chan *cover, 1)
	go func() {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		result <- radioGameArtwork(ctx, &http.Client{}, chiptuneOrigin, file)
	}()
	return result
}

var radioStationArtwork = sync.OnceValue(func() *cover {
	const scale = 8
	img := image.NewRGBA(image.Rect(0, 0, 48*scale, 48*scale))
	rect := func(x, y, w, h int, rgb uint32) {
		paint := color.RGBA{uint8(rgb >> 16), uint8(rgb >> 8), uint8(rgb), 255}
		draw.Draw(img, image.Rect(x*scale, y*scale, (x+w)*scale, (y+h)*scale), image.NewUniform(paint), image.Point{}, draw.Src)
	}

	rect(0, 0, 48, 48, 0x161921)
	for y := 1; y < 48; y += 3 {
		rect(0, y, 48, 1, 0x1b2229)
	}
	for _, star := range [][2]int{{5, 9}, {11, 18}, {38, 7}, {42, 19}, {4, 33}, {41, 36}, {10, 42}, {36, 43}} {
		rect(star[0], star[1], 1, 1, 0xd79a55)
	}
	// A tiny amber CRT with a pixel invader and a lit coin slot.
	rect(12, 7, 24, 2, 0xe5ae64)
	rect(9, 9, 30, 24, 0x8c5141)
	rect(10, 9, 28, 21, 0xe5ae64)
	rect(13, 12, 22, 16, 0x303b39)
	rect(15, 14, 18, 12, 0x192a2a)
	for y := 15; y < 26; y += 2 {
		rect(15, y, 18, 1, 0x223737)
	}
	for y, row := range []string{"..#.....#..", "...#...#...", "..#######..", ".##.###.##.", "###########", "#.#######.#", "#.#.....#.#"} {
		for x, pixel := range row {
			if pixel == '#' {
				rect(19+x, 16+y, 1, 1, 0x8de0b0)
			}
		}
	}
	rect(10, 30, 28, 4, 0xf8cb79)
	rect(14, 29, 2, 4, 0x643a37)
	rect(13, 28, 4, 2, 0xe87c70)
	rect(28, 31, 2, 1, 0xd9685e)
	rect(32, 31, 2, 1, 0xd9685e)
	rect(12, 34, 24, 7, 0x9f5a47)
	rect(22, 36, 5, 1, 0xffdc8a)
	rect(11, 41, 26, 2, 0x643a37)

	var data bytes.Buffer
	if png.Encode(&data, img) != nil {
		return nil
	}
	return readCoverBytes(data.Bytes())
})

func (a *album) updateRadioArtwork() {
	if a.radioArtwork == nil {
		return
	}
	select {
	case art := <-a.radioArtwork:
		a.radioArtwork = nil
		if art != nil {
			a.cover = art
			a.artworkSize = 0
		}
	default:
	}
}
