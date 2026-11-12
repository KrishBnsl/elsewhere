// Package radio talks to the live stations and the chiptune catalog Elsewhere can tune in to.
package radio

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const chiptuneOrigin = "https://chiptune.app"

var radioCatalogs = []string{
	"Nintendo", "Nintendo (NSFE)", "Nintendo Game Boy", "Nintendo SNES",
	"Sega Game Gear", "Sega Genesis", "Sega Master System", "VGM Rips",
}

// Song is a rendered chiptune: WAV is the playable audio, Err is set when nothing could be found.
type Song struct {
	Title, Game, Format, CatalogPath string
	WAV                              []byte
	Err                              error
}

// FindSong picks a random playable tune from the catalog, avoiding lastGame.
func FindSong(ctx context.Context, lastGame string) Song {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 10 * time.Second}
	for _, index := range rand.Perm(len(radioCatalogs))[:3] {
		if ctx.Err() != nil {
			break
		}
		files, err := shuffledGameFiles(ctx, client, chiptuneOrigin, radioCatalogs[index])
		if err != nil {
			continue
		}
		for _, file := range files {
			if ctx.Err() != nil {
				break
			}
			game := path.Base(path.Dir(file))
			if game == lastGame {
				continue
			}
			data, err := Get(ctx, client, (&url.URL{Scheme: "https", Host: "chiptune.app", Path: "/catalog/" + file}).String(), 8<<20)
			if err != nil {
				continue
			}
			data, err = unpackRadioFile(data)
			if err != nil {
				continue
			}
			wav, err := renderRadioTrack(ctx, data)
			if err == nil {
				ext := path.Ext(file)
				return Song{Title: strings.TrimSuffix(path.Base(file), ext), Game: game,
					Format: strings.ToUpper(strings.TrimPrefix(ext, ".")), CatalogPath: file, WAV: wav}
			}
		}
	}
	return Song{Err: errors.New("no playable tracks found. Press > to retry")}
}

func unpackRadioFile(data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, []byte{0x1f, 0x8b}) {
		return data, nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	const limit = 16 << 20
	unpacked, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if len(unpacked) > limit {
		return nil, errors.New("uncompressed chiptune is too large")
	}
	return unpacked, err
}

func shuffledGameFiles(ctx context.Context, client *http.Client, origin, catalog string) ([]string, error) {
	query := url.Values{"path": {catalog}, "limit": {"12"}}
	data, err := Get(ctx, client, origin+"/api/shuffle?"+query.Encode(), 1<<20)
	if err != nil {
		return nil, err
	}
	var response struct{ Items []string }
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	files := make([]string, 0, 3)
	for _, item := range response.Items {
		file := strings.TrimPrefix(item, "/")
		if !strings.HasPrefix(file, catalog+"/") || path.Clean(file) != file || strings.Contains(file, "\\") {
			continue
		}
		switch strings.ToLower(path.Ext(file)) {
		case ".nsf", ".nsfe", ".gbs", ".spc", ".vgm", ".vgz", ".gym":
			files = append(files, file)
			if len(files) == 3 {
				return files, nil
			}
		}
	}
	return files, nil
}

// Get fetches a URL with a size limit and the player's User-Agent.
func Get(ctx context.Context, client *http.Client, endpoint string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Elsewhere/1.0 (macOS music player)")
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("radio request: %s", response.Status)
	}
	if response.ContentLength > limit {
		return nil, errors.New("radio response too large")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if int64(len(data)) > limit {
		return nil, errors.New("radio response too large")
	}
	return data, err
}
