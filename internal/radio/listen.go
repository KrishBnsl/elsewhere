package radio

import (
	"context"
	"elsewhere/internal/audio"
	"elsewhere/internal/covers"
	"encoding/json"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const listenGateway = "wss://listen.moe/gateway_v2"
const (
	listenCovers  = "https://cdn.listen.moe/covers/"
	listenSources = "https://cdn.listen.moe/sources/"
	listenArtists = "https://cdn.listen.moe/artists/"
)

// ListenProvider is the provider name of the LISTEN.moe stations.
const ListenProvider = "LISTEN.moe"

// ListenUpdates follows LISTEN.moe's gateway and reports each song on air, then its artwork
// once fetched (falling back from album to show to artist images).
func ListenUpdates(ctx context.Context) <-chan TrackInfo {
	updates := make(chan TrackInfo, 1)
	go func() {
		for ctx.Err() == nil {
			readListenUpdates(ctx, updates)
			timer := time.NewTimer(5 * time.Second)
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

func readListenUpdates(ctx context.Context, updates chan<- TrackInfo) {
	dial, cancel := context.WithTimeout(ctx, 8*time.Second)
	conn, _, err := websocket.Dial(dial, listenGateway, nil)
	cancel()
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)
	session, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		heartbeat := time.NewTicker(20 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-session.Done():
				return
			case <-heartbeat.C:
				write, cancel := context.WithTimeout(session, 5*time.Second)
				err := conn.Write(write, websocket.MessageText, []byte(`{"op":9}`))
				cancel()
				if err != nil {
					return
				}
			}
		}
	}()

	client := &http.Client{Timeout: 10 * time.Second}
	var cache coverCache
	var cancelArt context.CancelFunc = func() {}
	defer func() { cancelArt() }()
	for {
		read, cancel := context.WithTimeout(session, 50*time.Second)
		_, data, err := conn.Read(read)
		cancel()
		if err != nil {
			return
		}
		info := listenTrack(data)
		if info.Title == "" {
			continue
		}
		cancelArt()
		cancelArt = func() {}
		// Reuse artwork already fetched for this album so consecutive tracks do not flicker.
		cached := false
		for _, candidate := range info.ArtPaths {
			if art := cache.get(candidate); art != nil {
				info.ArtPath, info.Art, cached = candidate, art, true
				break
			}
		}
		select {
		case updates <- info:
		case <-session.Done():
			return
		}
		if cached || len(info.ArtPaths) == 0 {
			continue
		}
		// Fetch artwork off the read loop so slow or failing downloads never delay track text.
		lookup, cancel := context.WithCancel(session)
		cancelArt = cancel
		go func(info TrackInfo) {
			found, art := fetchListenArt(lookup, client, info.ArtPaths)
			if art == nil || lookup.Err() != nil {
				return
			}
			cache.set(found, art)
			info.ArtPath, info.Art = found, art
			select {
			case updates <- info:
			case <-lookup.Done():
			}
		}(info)
	}
}

// fetchListenArt tries each candidate in order, retrying once so a transient CDN failure
// does not leave the station without a cover.
func fetchListenArt(ctx context.Context, client *http.Client, candidates []string) (string, *audio.Cover) {
	for _, candidate := range candidates {
		for attempt := range 2 {
			if ctx.Err() != nil {
				return "", nil
			}
			if attempt > 0 {
				timer := time.NewTimer(750 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return "", nil
				case <-timer.C:
				}
			}
			data, err := Get(ctx, client, candidate, covers.MaxBytes)
			if err != nil {
				continue
			}
			if art := audio.ReadCoverBytes(data); art != nil {
				return candidate, art
			}
			break
		}
	}
	return "", nil
}

func listenTrack(data []byte) TrackInfo {
	type credit struct {
		Name, NameRomaji string
		Image            string
	}
	var event struct {
		Op int
		T  string
		D  struct {
			Song struct {
				Title   string
				Artists []credit
				Albums  []credit
				Sources []credit
			}
		}
	}
	if json.Unmarshal(data, &event) != nil || event.Op != 1 ||
		(event.T != "TRACK_UPDATE" && event.T != "TRACK_UPDATE_REQUEST") {
		return TrackInfo{}
	}
	name := func(c credit) string {
		if c.NameRomaji != "" {
			return c.NameRomaji
		}
		return c.Name
	}
	song := event.D.Song
	info := TrackInfo{Title: song.Title}
	var artists []string
	for _, artist := range song.Artists {
		if n := name(artist); n != "" {
			artists = append(artists, n)
		}
	}
	info.Artist = strings.Join(artists, ", ")
	for _, album := range song.Albums {
		if info.Album == "" {
			info.Album = name(album)
		}
	}
	// Many songs have no album image, so fall back to the show or the artist.
	for _, group := range []struct {
		base    string
		credits []credit
	}{{listenCovers, song.Albums}, {listenSources, song.Sources}, {listenArtists, song.Artists}} {
		for _, c := range group.credits {
			file := c.Image
			if file == "" || path.Base(file) != file || file == "." || file == ".." || strings.Contains(file, "\\") {
				continue
			}
			candidate := group.base + url.PathEscape(file)
			if !slices.Contains(info.ArtPaths, candidate) {
				info.ArtPaths = append(info.ArtPaths, candidate)
			}
		}
	}
	if len(info.ArtPaths) > 0 {
		info.ArtPath = info.ArtPaths[0]
	}
	return info
}
