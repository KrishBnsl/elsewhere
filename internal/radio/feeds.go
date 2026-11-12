package radio

import (
	"context"
	"elsewhere/internal/audio"
	"elsewhere/internal/covers"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// TrackInfo is what a live station reports about the song on air. ArtPaths lists candidate
// artwork URLs in preference order; Art is the one that was fetched.
type TrackInfo struct {
	Title, Artist, Album, ArtPath string
	ArtPaths                      []string
	Art                           *audio.Cover
}

// coverCache remembers the most recent artwork a feed fetched, keyed by its source URL.
type coverCache struct {
	mu  sync.Mutex
	key string
	art *audio.Cover
}

func (c *coverCache) get(key string) *audio.Cover {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key == key {
		return c.art
	}
	return nil
}

func (c *coverCache) set(key string, art *audio.Cover) {
	c.mu.Lock()
	c.key, c.art = key, art
	c.mu.Unlock()
}

// Rainwave fetches the song on air at a Rainwave station and its album art, unless knownArt
// is already shown.
func Rainwave(ctx context.Context, client *http.Client, origin string, sid int, knownArt string) TrackInfo {
	data, err := Get(ctx, client, fmt.Sprintf("%s/api4/info?sid=%d", origin, sid), 1<<20)
	if err != nil {
		return TrackInfo{}
	}
	var response struct {
		Current struct {
			Songs []struct {
				Title   string
				Artists []struct{ Name string }
				Albums  []struct{ Name, Art string }
			}
		} `json:"sched_current"`
	}
	if json.Unmarshal(data, &response) != nil || len(response.Current.Songs) == 0 {
		return TrackInfo{}
	}
	song := response.Current.Songs[0]
	info := TrackInfo{Title: song.Title}
	var artists []string
	for _, artist := range song.Artists {
		artists = append(artists, artist.Name)
	}
	info.Artist = strings.Join(artists, ", ")
	if len(song.Albums) == 0 {
		return info
	}
	info.Album = song.Albums[0].Name
	artPath := song.Albums[0].Art
	asset, err := url.Parse(artPath)
	if err != nil || !strings.HasPrefix(artPath, "/album_art/") || asset.Host != "" {
		return info
	}
	info.ArtPath = artPath
	if artPath != knownArt {
		if bytes, err := Get(ctx, client, origin+artPath+"_320.jpg", covers.MaxBytes); err == nil {
			info.Art = audio.ReadCoverBytes(bytes)
		}
	}
	return info
}
