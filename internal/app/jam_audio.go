package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"elsewhere/internal/friendshare"
)

type jamTransfer struct {
	id, path string
	err      error
}

func receivedJamAlbum(path string, song friendshare.Song) *album {
	// Temporary audio shares a directory with unrelated files.
	a := &album{title: song.Album, artist: song.Artist, loaded: true}
	a.tracks = []track{{
		path: path, title: song.Title, artist: song.Artist, album: song.Album, source: a, jamAudio: true,
	}}
	return a
}

func (play *jamPlayback) clearAudio() {
	if play.cancelAudio != nil {
		play.cancelAudio()
	}
	for _, path := range play.received {
		_ = os.Remove(path)
	}
}

func (p *player) tickJamAudio(now time.Time, room *friendshare.Jam, me string) {
	play := &p.social.jam
	id := ""
	if len(room.Queue) > 0 {
		id = room.Queue[0].ID
	}
	if play.transferID != id {
		if play.cancelAudio != nil {
			play.cancelAudio()
		}
		play.transferID, play.transferResult, play.cancelAudio = id, nil, nil
		play.transferNext, play.transferError = time.Time{}, ""
	}
	for entry, path := range play.received {
		if entry != id && path != play.path {
			if err := os.Remove(path); err == nil || os.IsNotExist(err) {
				delete(play.received, entry)
			}
		}
	}
	select {
	case result := <-play.transferResult:
		play.cancelAudio()
		play.transferResult, play.cancelAudio = nil, nil
		play.transferNext = now.Add(4 * time.Second)
		if result.err == nil && room.Queue[0].AddedBy == me {
			play.transferNext = now.Add(15 * time.Second)
		}
		if result.err != nil {
			play.transferError = result.err.Error()
		} else {
			play.transferError = ""
			if result.path != "" {
				if play.received == nil {
					play.received = make(map[string]string)
				}
				play.received[result.id] = result.path
			}
		}
	default:
	}
	if id == "" || !room.Joined || play.transferResult != nil || now.Before(play.transferNext) || p.social.client == nil {
		return
	}
	entry := room.Queue[0]
	owner := entry.AddedBy
	if owner == "" {
		return
	}
	path := play.paths[entry.Song.Hash]
	if owner == me {
		peer := room.Host.ID
		if peer == me {
			peer = room.Guest.ID
		}
		if path == "" || !slices.Contains(room.Ready[me], id) || slices.Contains(room.Ready[peer], id) {
			return
		}
	} else if path != "" || play.received[id] != "" {
		return
	}
	client := *p.social.client
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan jamTransfer)
	play.transferResult, play.cancelAudio = result, cancel
	go func() {
		file, err := client.transferJamAudio(ctx, id, path)
		select {
		case result <- jamTransfer{id: id, path: file, err: err}:
		case <-ctx.Done():
			if file != "" {
				_ = os.Remove(file)
			}
		}
	}()
}

func (f friendsClient) transferJamAudio(ctx context.Context, id, source string) (string, error) {
	method, ext := http.MethodGet, ""
	var body io.Reader
	var size int64
	if source != "" {
		method, ext = http.MethodPut, strings.ToLower(filepath.Ext(source))
		if ext == ".flac" {
			if ffmpeg := jamFFmpeg(); ffmpeg != "" {
				mp3, err := encodeJamMP3(ctx, ffmpeg, source)
				if err != nil {
					return "", err
				}
				defer os.Remove(mp3)
				source, ext = mp3, ".mp3"
			}
		}
		file, err := os.Open(source)
		if err != nil {
			return "", err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return "", err
		}
		if ext == ".flac" && info.Size() > friendshare.MaxJamAudio {
			return "", fmt.Errorf("install FFmpeg to send this FLAC as MP3")
		}
		if !info.Mode().IsRegular() || !friendshare.JamAudioExtension(ext) || info.Size() <= 0 || info.Size() > friendshare.MaxJamAudio {
			return "", fmt.Errorf("jam audio must be a supported file up to 32 MiB")
		}
		body, size = file, info.Size()
	}
	req, err := http.NewRequestWithContext(ctx, method, f.base+"/v1/jam-audio/"+id, body)
	if err != nil {
		return "", err
	}
	req.ContentLength = size
	if source != "" {
		req.Header.Set("Expect", "100-continue")
	}
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("X-Jam-Audio-Extension", ext)
	client := *friendsHTTP
	client.Timeout = 2 * time.Minute
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if source != "" && res.StatusCode == http.StatusNoContent {
		return "", nil
	}
	if source == "" && res.StatusCode == http.StatusNotFound {
		return "", nil // The other listener is still preparing the transfer.
	}
	if source != "" || res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("audio transfer returned %s", res.Status)
	}
	ext = res.Header.Get("X-Jam-Audio-Extension")
	if !friendshare.JamAudioExtension(ext) || res.ContentLength > friendshare.MaxJamAudio {
		return "", fmt.Errorf("invalid jam audio")
	}
	file, err := os.CreateTemp("", "elsewhere-jam-*"+ext)
	if err != nil {
		return "", err
	}
	n, copyErr := io.Copy(file, io.LimitReader(res.Body, friendshare.MaxJamAudio+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || n == 0 || n > friendshare.MaxJamAudio || res.ContentLength >= 0 && res.ContentLength != n {
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("could not receive complete jam audio")
	}
	return file.Name(), nil
}

func jamFFmpeg() string {
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path
	}
	if runtime.GOOS == "darwin" {
		for _, prefix := range []string{"/opt/homebrew", "/usr/local"} {
			path := filepath.Join(prefix, "opt/ffmpeg-full/bin/ffmpeg")
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
				return path
			}
		}
	}
	return ""
}

func encodeJamMP3(ctx context.Context, ffmpeg, source string) (string, error) {
	file, err := os.CreateTemp("", "elsewhere-jam-encode-*.mp3")
	if err != nil {
		return "", err
	}
	path := file.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(path)
		}
	}()
	if err := file.Close(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error", "-y", "-protocol_whitelist", "file", "-f", "flac", "-i", source,
		"-map", "0:a:0", "-map_metadata", "-1", "-map_chapters", "-1",
		"-c:a", "libmp3lame", "-b:a", "192k", "-ac", "2", "-ar", "48000",
		"-fs", strconv.Itoa(friendshare.MaxJamAudio), path)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("could not convert FLAC to MP3: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	// FFmpeg stops once it hits the limit. Never send a truncated track.
	if info.Size() <= 0 || info.Size() >= friendshare.MaxJamAudio {
		return "", fmt.Errorf("converted MP3 exceeds the jam transfer limit")
	}
	keep = true
	return path, nil
}
