package radio

import (
	"bytes"
	"context"
	"elsewhere/internal/audio"
	"elsewhere/internal/covers"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Providers of the lo-fi stations.
const (
	LautProvider = "laut.fm"
	FluxProvider = "FluxFM"
)

const (
	lautNowPlaying = "https://api.laut.fm/station/lofi/current_song"
	fluxNowPlaying = "https://fluxmusic.api.radiosphere.io/channels/chillhop/current-track"
	lofiPollEvery  = 15 * time.Second
)

// LofiUpdates polls the now-playing API of a lo-fi station (laut.fm or FluxFM) and reports each
// song on air, then its cover once found: the station's own art when it has some, otherwise a
// MusicBrainz lookup. Without either, the player keeps LofiArtwork.
func LofiUpdates(ctx context.Context, provider string) <-chan TrackInfo {
	source, parse := lautNowPlaying, lautTrack
	if provider == FluxProvider {
		source, parse = fluxNowPlaying, fluxTrack
	}
	updates := make(chan TrackInfo, 1)
	go func() {
		client := &http.Client{Timeout: 12 * time.Second}
		var known string
		cancelArt := func() {}
		defer func() { cancelArt() }()
		for ctx.Err() == nil {
			if data, err := Get(ctx, client, source, 1<<20); err == nil {
				info := parse(data)
				if key := info.Artist + "\x00" + info.Title; info.Title != "" && key != known {
					known = key
					cancelArt()
					select {
					case updates <- info:
					case <-ctx.Done():
						return
					}
					lookup, cancel := context.WithCancel(ctx)
					cancelArt = cancel
					go func(info TrackInfo) {
						art, path := lofiArtwork(lookup, client, info)
						if art == nil || lookup.Err() != nil {
							return
						}
						info.ArtPath, info.Art = path, art
						select {
						case updates <- info:
						case <-lookup.Done():
						}
					}(info)
				}
			}
			timer := time.NewTimer(lofiPollEvery)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return updates
}

func lofiArtwork(ctx context.Context, client *http.Client, info TrackInfo) (*audio.Cover, string) {
	for _, source := range info.ArtPaths {
		if data, err := Get(ctx, client, source, covers.MaxBytes); err == nil {
			if art := audio.ReadCoverBytes(data); art != nil {
				return art, source
			}
		}
	}
	if info.Artist == "" || ctx.Err() != nil {
		return nil, ""
	}
	album := info.Album
	if album == "" {
		album = info.Title
	}
	if art, _, err := covers.Online(ctx, album, info.Artist); err == nil && art != nil {
		return art, "cover:" + info.Artist + "\x00" + album
	}
	return nil, ""
}

// lautTrack reads laut.fm's current_song reply.
func lautTrack(data []byte) TrackInfo {
	var song struct {
		Title, Album string
		Artist       struct{ Name string }
	}
	if json.Unmarshal(data, &song) != nil {
		return TrackInfo{}
	}
	return TrackInfo{Title: strings.TrimSpace(song.Title), Artist: strings.TrimSpace(song.Artist.Name), Album: strings.TrimSpace(song.Album)}
}

// fluxTrack reads FluxFM's current-track reply, whose artwork is a URL when present.
func fluxTrack(data []byte) TrackInfo {
	var reply struct {
		TrackInfo struct {
			Title         string
			ArtistCredits string
			Artwork       json.RawMessage
			Release       json.RawMessage
		}
	}
	if json.Unmarshal(data, &reply) != nil {
		return TrackInfo{}
	}
	t := reply.TrackInfo
	info := TrackInfo{Title: strings.TrimSpace(t.Title), Artist: strings.TrimSpace(t.ArtistCredits)}
	var artwork string
	if json.Unmarshal(t.Artwork, &artwork) == nil && strings.HasPrefix(artwork, "https://") {
		info.ArtPaths = []string{artwork}
	}
	var release struct{ Title string }
	if json.Unmarshal(t.Release, &release) == nil {
		info.Album = strings.TrimSpace(release.Title)
	}
	return info
}

// LofiArtwork is the lo-fi stations' cover: a desk by a window at night, lamp on, tea steaming.
var LofiArtwork = sync.OnceValue(func() *audio.Cover {
	const scale = 8
	img := image.NewRGBA(image.Rect(0, 0, 48*scale, 48*scale))
	rect := func(x, y, w, h int, rgb uint32) {
		paint := color.RGBA{uint8(rgb >> 16), uint8(rgb >> 8), uint8(rgb), 255}
		draw.Draw(img, image.Rect(x*scale, y*scale, (x+w)*scale, (y+h)*scale), image.NewUniform(paint), image.Point{}, draw.Src)
	}

	// The room, lit warm near the lamp.
	rect(0, 0, 48, 48, 0x221d33)
	rect(0, 18, 20, 16, 0x2c2338)
	// The window and the night outside: a sky that deepens upward, stars, a moon and a skyline.
	rect(6, 3, 36, 27, 0x3a2f45)
	for i, band := range []uint32{0x18204a, 0x1d2758, 0x232f66, 0x2b3872, 0x34417a} {
		rect(8, 5+i*4, 32, 4, band)
	}
	rect(8, 25, 32, 3, 0x3c4782)
	for _, star := range [][2]int{{10, 7}, {15, 10}, {20, 6}, {13, 15}, {37, 17}, {27, 7}} {
		rect(star[0], star[1], 1, 1, 0xd9d4f0)
	}
	rect(31, 8, 4, 6, 0xf3e3b0)
	rect(30, 9, 6, 4, 0xf3e3b0)
	rect(33, 9, 2, 2, 0xd9c690) // a crater
	for _, b := range [][3]int{{8, 21, 5}, {13, 18, 4}, {17, 22, 6}, {23, 19, 5}, {28, 23, 4}, {32, 17, 4}, {36, 21, 4}} {
		rect(b[0], b[1], b[2], 28-b[1], 0x141833)
	}
	for _, w := range [][2]int{{9, 23}, {14, 20}, {15, 24}, {19, 24}, {24, 21}, {25, 25}, {33, 19}, {34, 23}, {37, 23}} {
		rect(w[0], w[1], 1, 1, 0xf2c46d)
	}
	rect(23, 5, 2, 23, 0x3a2f45)
	rect(8, 16, 32, 1, 0x3a2f45)
	rect(5, 29, 38, 2, 0x4a3b52) // the sill
	// The desk.
	rect(0, 34, 48, 3, 0x7a5442)
	rect(0, 37, 48, 11, 0x3d2b2c)
	rect(0, 37, 48, 1, 0x56392f)
	// The lamp and its pool of light.
	rect(4, 22, 9, 4, 0xe8a85c)
	rect(5, 21, 7, 1, 0xc98845)
	rect(8, 26, 1, 6, 0x2c2c3a)
	rect(5, 32, 7, 2, 0x2c2c3a)
	rect(3, 34, 13, 1, 0xa97a55)
	// Books, a mug with steam, and a plant.
	rect(16, 31, 11, 1, 0xb5654a)
	rect(17, 32, 10, 1, 0x6b8fb3)
	rect(16, 33, 11, 1, 0xd9b56f)
	rect(30, 29, 5, 5, 0xd9d2c5)
	rect(30, 29, 5, 1, 0x8c5c3c)
	rect(35, 30, 1, 3, 0xd9d2c5)
	for _, s := range [][2]int{{31, 27}, {32, 26}, {31, 25}, {33, 24}, {32, 23}} {
		rect(s[0], s[1], 1, 1, 0x8a86a6)
	}
	rect(39, 30, 6, 4, 0xb5654a)
	for _, leaf := range [][2]int{{41, 27}, {40, 26}, {43, 26}, {42, 25}, {39, 28}, {44, 28}, {41, 24}, {43, 29}, {40, 29}} {
		rect(leaf[0], leaf[1], 1, 2, 0x5f9e6e)
	}

	var data bytes.Buffer
	if png.Encode(&data, img) != nil {
		return nil
	}
	return audio.ReadCoverBytes(data.Bytes())
})
