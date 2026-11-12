package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	radioProvider  = "R/a/dio"
	radioAPI       = "https://r-a-d.io/api"
	radioDJImages  = "https://r-a-d.io/api/dj-image/"
	radioPollEvery = 10 * time.Second
)

// radioUpdates polls R/a/dio's now-playing API. The API only reports an "Artist - Title"
// string and the DJ, so artwork comes from a MusicBrainz lookup with the DJ's avatar as fallback.
func radioUpdates(ctx context.Context) <-chan liveTrackInfo {
	updates := make(chan liveTrackInfo, 1)
	go func() {
		client := &http.Client{Timeout: 12 * time.Second}
		var avatars coverCache
		var known string
		cancelArt := func() {}
		defer func() { cancelArt() }()
		for ctx.Err() == nil {
			if data, err := radioGet(ctx, client, radioAPI, 1<<20); err == nil {
				info, avatar := radioTrack(data)
				if key := info.artist + "\x00" + info.title; info.title != "" && key != known {
					known = key
					cancelArt()
					select {
					case updates <- info:
					case <-ctx.Done():
						return
					}
					lookup, cancel := context.WithCancel(ctx)
					cancelArt = cancel
					go func(info liveTrackInfo, avatar string) {
						art, source := radioArtwork(lookup, client, &avatars, info, avatar)
						if art == nil || lookup.Err() != nil {
							return
						}
						info.artPath, info.art = source, art
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

func radioArtwork(ctx context.Context, client *http.Client, avatars *coverCache, info liveTrackInfo, avatar string) (*cover, string) {
	if info.artist != "" {
		if art, _, err := onlineCover(ctx, info.title, info.artist); err == nil && art != nil {
			return art, "cover:" + info.artist + "\x00" + info.title
		}
	}
	if avatar == "" || ctx.Err() != nil {
		return nil, ""
	}
	source := radioDJImages + url.PathEscape(avatar)
	if art := avatars.get(source); art != nil {
		return art, source
	}
	data, err := radioGet(ctx, client, source, maxCoverBytes)
	if err != nil {
		return nil, ""
	}
	art := readCoverBytes(data)
	if art != nil {
		avatars.set(source, art)
	}
	return art, source
}

// radioTrack parses the "main" block of R/a/dio's API and returns the DJ avatar file name.
func radioTrack(data []byte) (liveTrackInfo, string) {
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
		return liveTrackInfo{}, ""
	}
	now := strings.TrimSpace(response.Main.NP)
	if now == "" {
		return liveTrackInfo{}, ""
	}
	info := liveTrackInfo{title: now}
	if artist, title, ok := strings.Cut(now, " - "); ok && strings.TrimSpace(artist) != "" && strings.TrimSpace(title) != "" {
		info.artist, info.title = strings.TrimSpace(artist), strings.TrimSpace(title)
	}
	if dj := strings.TrimSpace(response.Main.DJ.DJName); dj != "" {
		info.album = "DJ " + dj
	}
	avatar := response.Main.DJ.DJImage
	if strings.ContainsAny(avatar, "/\\") || avatar == "." || avatar == ".." {
		avatar = ""
	}
	return info, avatar
}
