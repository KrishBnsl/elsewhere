package app

import (
	"context"
	"crypto/sha256"
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

const lyricsEndpoint = "https://lrclib.net/api/get"

type lyricsResult struct {
	lines  []lyricLine
	status string
}

func onlineLyrics(ctx context.Context, t track, duration float64) lyricsResult {
	if t.title == "" || t.artist == "" || t.artist == "Unknown artist" {
		return lyricsResult{status: "Add title and artist tags to search"}
	}
	query := url.Values{"track_name": {t.title}, "artist_name": {t.artist}}
	if t.taggedAlbum {
		query.Set("album_name", t.album)
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
			lines := parseLyrics(io.LimitReader(file, 1<<20))
			file.Close()
			if len(lines) > 0 {
				return lyricsResult{lines: lines}
			}
		}
	}

	client := &http.Client{Timeout: 8 * time.Second}
	raw, status := fetchLyrics(ctx, client, lyricsEndpoint+"?"+signature)
	if status != "" {
		return lyricsResult{status: status}
	}
	lines := parseLyrics(strings.NewReader(raw))
	if len(lines) == 0 {
		return lyricsResult{status: "No synced lyrics found"}
	}
	if filename != "" {
		_ = writeLocalFile(filename, []byte(raw))
	}
	return lyricsResult{lines: lines}
}

func fetchLyrics(ctx context.Context, client *http.Client, endpoint string) (string, string) {
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

func writeLocalFile(filename string, data []byte) error {
	folder := filepath.Dir(filename)
	if err := os.MkdirAll(folder, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(folder, ".elsewhere-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), filename)
}
