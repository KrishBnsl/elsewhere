package lyrics

import (
	"context"
	"crypto/sha256"
	"elsewhere/internal/fsutil"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const lrclibEndpoint = "https://lrclib.net/api/get"

// Result is what an online lookup found, or why it found nothing.
type Result struct {
	Lines  []Line
	Status string
}

// Query describes the track to look up.
type Query struct {
	Title, Artist, Album string
	TaggedAlbum          bool
}

// Online finds synced lyrics on LRCLIB, caching hits under the user cache directory.
func Online(ctx context.Context, t Query, duration float64) Result {
	if t.Title == "" || t.Artist == "" || t.Artist == "Unknown artist" {
		return Result{Status: "Add title and artist tags to search"}
	}
	query := url.Values{"track_name": {t.Title}, "artist_name": {t.Artist}}
	if t.TaggedAlbum {
		query.Set("album_name", t.Album)
	}
	if seconds := int(math.Round(duration)); seconds >= 1 && seconds <= 3600 {
		query.Set("duration", strconv.Itoa(seconds))
	}
	signature := query.Encode()
	cache, _ := os.UserCacheDir()
	var filename string
	if cache != "" {
		filename = filepath.Join(cache, "Elsewhere", "lyrics", fmt.Sprintf("%x.lrc", sha256.Sum256([]byte(signature))))
		if file, err := os.Open(filename); err == nil {
			lines := Parse(io.LimitReader(file, 1<<20))
			file.Close()
			if len(lines) > 0 {
				return Result{Lines: lines}
			}
		}
	}

	client := &http.Client{Timeout: 8 * time.Second}
	raw, status := fetch(ctx, client, lrclibEndpoint+"?"+signature)
	if status != "" {
		return Result{Status: status}
	}
	lines := Parse(strings.NewReader(raw))
	if len(lines) == 0 {
		return Result{Status: "No synced lyrics found"}
	}
	if filename != "" {
		_ = fsutil.WriteFile(filename, []byte(raw))
	}
	return Result{Lines: lines}
}

func fetch(ctx context.Context, client *http.Client, endpoint string) (string, string) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "Lyrics lookup unavailable"
	}
	request.Header.Set("User-Agent", "Elsewhere (macOS music player)")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "", "Lyrics lookup unavailable"
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return "", "No synced lyrics found"
	}
	if response.StatusCode != http.StatusOK {
		return "", "Lyrics lookup unavailable"
	}
	var record struct {
		SyncedLyrics string `json:"syncedLyrics"`
		Instrumental bool   `json:"instrumental"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&record); err != nil {
		return "", "Lyrics lookup unavailable"
	}
	if record.Instrumental {
		return "", "Instrumental track"
	}
	if strings.TrimSpace(record.SyncedLyrics) == "" {
		return "", "No synced lyrics found"
	}
	return record.SyncedLyrics, ""
}
