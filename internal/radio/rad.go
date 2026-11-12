package radio

import (
	"context"
	"elsewhere/internal/audio"
	"elsewhere/internal/covers"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	RadProvider    = "R/a/dio"
	radioAPI       = "https://r-a-d.io/api"
	radioDJImages  = "https://r-a-d.io/api/dj-image/"
	radioPollEvery = 10 * time.Second
)

// radioUpdates polls R/a/dio's now-playing API. The API only reports an "Artist - Title"
// string and the DJ, so artwork comes from a MusicBrainz lookup with the DJ's avatar as fallback.
func RadUpdates(ctx context.Context) <-chan TrackInfo {
	updates := make(chan TrackInfo, 1)
	go func() {
		client := &http.Client{Timeout: 12 * time.Second}
		var avatars coverCache
		var known string
		cancelArt := func() {}
		defer func() { cancelArt() }()
		for ctx.Err() == nil {
			if data, err := Get(ctx, client, radioAPI, 1<<20); err == nil {
				info, avatar := radioTrack(data)
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
					go func(info TrackInfo, avatar string) {
						art, source := radioArtwork(lookup, client, &avatars, info, avatar)
						if art == nil || lookup.Err() != nil {
							return
						}
						info.ArtPath, info.Art = source, art
						select {
						case updates <- info:
						case <-lookup.Done():
						}
					}(info, avatar)
				}
			}
			timer := time.NewTimer(radioPollEvery)
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

func radioArtwork(ctx context.Context, client *http.Client, avatars *coverCache, info TrackInfo, avatar string) (*audio.Cover, string) {
	if info.Artist != "" {
		if art, _, err := covers.Online(ctx, info.Title, info.Artist); err == nil && art != nil {
			return art, "cover:" + info.Artist + "\x00" + info.Title
		}
	}
	if avatar == "" || ctx.Err() != nil {
		return nil, ""
	}
	source := radioDJImages + url.PathEscape(avatar)
	if art := avatars.get(source); art != nil {
		return art, source
	}
	data, err := Get(ctx, client, source, covers.MaxBytes)
	if err != nil {
		return nil, ""
	}
	art := audio.ReadCoverBytes(data)
	if art != nil {
		avatars.set(source, art)
	}
	return art, source
}

// radioTrack parses the "main" block of R/a/dio's API and returns the DJ avatar file name.
func radioTrack(data []byte) (TrackInfo, string) {
	var response struct {
		Main struct {
			NP string
			DJ struct {
				DJName  string
				DJImage string
			}
		}
	}
	if json.Unmarshal(data, &response) != nil {
		return TrackInfo{}, ""
	}
	now := strings.TrimSpace(response.Main.NP)
	if now == "" {
		return TrackInfo{}, ""
	}
	info := TrackInfo{Title: now}
	if artist, title, ok := strings.Cut(now, " - "); ok && strings.TrimSpace(artist) != "" && strings.TrimSpace(title) != "" {
		info.Artist, info.Title = strings.TrimSpace(artist), strings.TrimSpace(title)
	}
	if dj := strings.TrimSpace(response.Main.DJ.DJName); dj != "" {
		info.Album = "DJ " + dj
	}
	avatar := response.Main.DJ.DJImage
	if strings.ContainsAny(avatar, "/\\") || avatar == "." || avatar == ".." {
		avatar = ""
	}
	return info, avatar
}
