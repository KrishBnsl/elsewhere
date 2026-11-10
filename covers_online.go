package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const releaseGroupSearch = "https://musicbrainz.org/ws/2/release-group"
const coverArchive = "https://coverartarchive.org/release-group/"
const maxCoverBytes = 2 << 20

var remasterSuffix = regexp.MustCompile(`(?i)\s*[(\[](?:(?:19|20)\d{2}\s+)?remaster(?:ed)?(?:\s+(?:19|20)\d{2})?[)\]]$`)
var releaseGroupID = regexp.MustCompile(`^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$`)

var coverSearchRate struct {
	sync.Mutex
	next time.Time
}

type coverLookup struct {
	album  *album
	cancel context.CancelFunc
	result <-chan coverResult
}

type coverResult struct {
	art      *cover
	filename string
	err      error
}

func (p *player) updateCover() {
	if p.coverLookup.result != nil {
		select {
		case result := <-p.coverLookup.result:
			a := p.coverLookup.album
			p.stopCoverLookup()
			if result.art != nil {
				a.cover = result.art
				a.artworkSize = 0
			}
			if t := p.current(); t != nil && t.source == a {
				switch {
				case result.err != nil:
					p.notice = "Cover: " + result.err.Error() + " (C retries)"
				case result.art == nil:
					p.notice = "No matching cover found (C retries)"
				default:
					p.notice = "Saved " + result.filename
				}
			}
		default:
		}
	}
	t := p.current()
	if !p.popup || t == nil || p.searching || p.playlistDialog != nil || p.radioPicker != nil || p.queueView != nil || p.themePicker != nil {
		p.coverPrompt = nil
		return
	}
	a := t.source
	if a.radio {
		a.updateRadioArtwork()
		p.coverPrompt = nil
		return
	}
	if a.artwork(*t) != nil || a.coverPromptDismissed || a.artist == "" || p.coverLookup.result != nil {
		p.coverPrompt = nil
		return
	}
	p.coverPrompt = a
}

func (p *player) answerCoverPrompt(accept bool) {
	a, t := p.coverPrompt, p.current()
	p.coverPrompt = nil
	if a == nil || t == nil || t.source != a {
		return
	}
	a.coverPromptDismissed = true
	if !accept {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan coverResult, 1)
	p.coverLookup = coverLookup{a, cancel, result}
	p.notice = "Looking up cover..."
	go func(title, artist, folder string) {
		art, data, err := onlineCover(ctx, title, artist)
		if err != nil || art == nil {
			result <- coverResult{err: err}
			return
		}
		if ctx.Err() != nil {
			return
		}
		name, err := saveAlbumCover(folder, art, data)
		if err != nil {
			result <- coverResult{err: coverStageError("Save failed", err)}
			return
		}
		result <- coverResult{art: art, filename: name}
	}(a.title, a.artist, filepath.Dir(t.path))
}

func saveAlbumCover(folder string, art *cover, data []byte) (string, error) {
	name := "cover.jpg"
	if http.DetectContentType(data) != "image/jpeg" {
		name = "cover.png"
		data = art.png
	}
	if len(data) == 0 {
		return "", errors.New("image could not be encoded")
	}
	file, err := os.CreateTemp(folder, ".elsewhere-cover-")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return "", err
	}
	// Linking commits the complete image without overwriting a cover added during the download.
	if err := os.Link(file.Name(), filepath.Join(folder, name)); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", errors.New(name + " already exists")
		}
		return "", err
	}
	return name, nil
}

func (p *player) stopCoverLookup() {
	if p.coverLookup.cancel != nil {
		p.coverLookup.cancel()
		p.coverLookup = coverLookup{}
	}
}

func onlineCover(ctx context.Context, title, artist string) (*cover, []byte, error) {
	title = strings.TrimSpace(remasterSuffix.ReplaceAllString(title, ""))
	var filename string
	if cache, err := os.UserCacheDir(); err == nil {
		key := sha256.Sum256([]byte(strings.ToLower(artist + "\x00" + title)))
		filename = filepath.Join(cache, "Elsewhere", "covers", fmt.Sprintf("%x.img", key))
		if info, err := os.Stat(filename); err == nil && info.Mode().IsRegular() && info.Size() <= maxCoverBytes {
			if data, err := os.ReadFile(filename); err == nil && len(data) <= maxCoverBytes {
				if art := readCoverBytes(data); art != nil {
					return art, data, nil
				}
			}
		}
	}

	search := &http.Client{Timeout: 8 * time.Second}
	group, err := findReleaseGroup(ctx, search, releaseGroupSearch, title, artist)
	if err != nil {
		return nil, nil, coverStageError("MusicBrainz search", err)
	}
	if group == "" {
		return nil, nil, nil
	}
	client := &http.Client{Timeout: 12 * time.Second}
	art, data, err := downloadCover(ctx, client, coverArchive+group+"/front-250")
	if err == nil && art != nil && filename != "" {
		_ = writeLocalFile(filename, data)
	}
	if err != nil {
		return nil, nil, coverStageError("Image download", err)
	}
	return art, data, nil
}

func coverStageError(stage string, err error) error {
	var status coverHTTPError
	var fileError *os.PathError
	var linkError *os.LinkError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%s timed out", stage)
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("%s canceled", stage)
	case errors.As(err, &status):
		return fmt.Errorf("%s returned HTTP %d", stage, status.code)
	case errors.As(err, &fileError):
		return fmt.Errorf("%s: %v", stage, fileError.Err)
	case errors.As(err, &linkError):
		return fmt.Errorf("%s: %v", stage, linkError.Err)
	default:
		return fmt.Errorf("%s: %s", stage, shortCoverError(err))
	}
}

func shortCoverError(err error) string {
	var request *url.Error
	if errors.As(err, &request) {
		return "connection failed"
	}
	return err.Error()
}

func curlCover(ctx context.Context, endpoint string, limit int) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/curl", "--fail", "--silent", "--location",
		"--proto", "=https", "--proto-redir", "=https", "--connect-timeout", "4",
		"--max-time", "8", "--max-filesize", strconv.Itoa(limit),
		"--user-agent", "Elsewhere/1.0 (macOS local music player)", "--", endpoint)
	data, err := cmd.Output()
	if len(data) > limit {
		return nil, errors.New("response exceeds size limit")
	}
	return data, err
}

func downloadCover(ctx context.Context, client *http.Client, endpoint string) (*cover, []byte, error) {
	if strings.HasPrefix(endpoint, coverArchive) {
		if data, err := curlCover(ctx, endpoint, maxCoverBytes); err == nil {
			if art := readCoverBytes(data); art != nil {
				return art, data, nil
			}
		}
	}
	var lastError error
	for attempt := range 2 {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		response, err := coverGET(ctx, client, endpoint)
		if err != nil {
			var status coverHTTPError
			if errors.As(err, &status) && status.code == http.StatusNotFound {
				return nil, nil, nil
			}
			lastError = err
			if attempt == 0 {
				var redirect *url.Error
				failedURL := status.endpoint
				if errors.As(err, &redirect) {
					failedURL = redirect.URL
				}
				if mirror := archiveCoverMirror(ctx, client, "https://archive.org", failedURL); mirror != "" {
					endpoint = mirror
				}
			}
			continue
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, maxCoverBytes+1))
		response.Body.Close()
		if err != nil {
			lastError = err
			continue
		}
		if len(data) > maxCoverBytes {
			return nil, nil, fmt.Errorf("cover image exceeds size limit")
		}
		if art := readCoverBytes(data); art != nil {
			return art, data, nil
		}
		lastError = fmt.Errorf("cover response was not a supported image")
	}
	return nil, nil, lastError
}

// Cover Art Archive redirects to Internet Archive storage. A stalled mirror can
// be bypassed using the item's current storage server and directory.
func archiveCoverMirror(ctx context.Context, client *http.Client, metadataOrigin, failedURL string) string {
	failed, err := url.Parse(failedURL)
	if err != nil || !isArchiveStorage(failed.Hostname()) {
		return ""
	}
	_, itemAndFile, ok := strings.Cut(failed.Path, "/items/")
	item, filename, separated := strings.Cut(itemAndFile, "/")
	if !ok || !separated || !strings.HasPrefix(item, "mbid-") ||
		!releaseGroupID.MatchString(strings.TrimPrefix(item, "mbid-")) ||
		filename == "" || path.Base(filename) != filename || !strings.HasPrefix(filename, item+"-") {
		return ""
	}
	response, err := coverGET(ctx, client, metadataOrigin+"/metadata/"+item)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	var storage struct {
		Server string `json:"server"`
		Dir    string `json:"dir"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&storage); err != nil ||
		!isArchiveStorage(storage.Server) || !strings.HasSuffix(storage.Dir, "/items/"+item) {
		return ""
	}
	return (&url.URL{Scheme: "https", Host: storage.Server, Path: path.Join(storage.Dir, filename)}).String()
}

func isArchiveStorage(host string) bool {
	return strings.HasSuffix(host, ".archive.org") && !strings.ContainsAny(host, ":/@")
}

func findReleaseGroup(ctx context.Context, client *http.Client, endpoint, title, artist string) (string, error) {
	quote := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace
	query := url.Values{
		"query": {`releasegroup:"` + quote(title) + `" AND artistname:"` + quote(artist) + `"`},
		"fmt":   {"json"},
		"limit": {"8"},
	}
	// MusicBrainz limits clients to one request per second.
	coverSearchRate.Lock()
	if delay := time.Until(coverSearchRate.next); delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			coverSearchRate.Unlock()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	coverSearchRate.next = time.Now().Add(time.Second)
	coverSearchRate.Unlock()
	body, err := coverSearchBody(ctx, client, endpoint, query.Encode())
	if err != nil {
		return "", err
	}
	defer body.Close()
	var results struct {
		Groups []struct {
			ID, Title string
			Credits   []struct {
				Name, Joinphrase string
			} `json:"artist-credit"`
		} `json:"release-groups"`
	}
	if err := json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&results); err != nil {
		return "", err
	}
	for _, group := range results.Groups {
		var credit strings.Builder
		for _, entry := range group.Credits {
			credit.WriteString(entry.Name)
			credit.WriteString(entry.Joinphrase)
		}
		if releaseGroupID.MatchString(group.ID) && strings.EqualFold(group.Title, title) &&
			strings.EqualFold(strings.TrimSpace(credit.String()), artist) {
			return group.ID, nil
		}
	}
	return "", nil
}

func coverSearchBody(ctx context.Context, client *http.Client, endpoint, query string) (io.ReadCloser, error) {
	address := endpoint + "?" + query
	if endpoint == releaseGroupSearch {
		if data, err := curlCover(ctx, address, 1<<20); err == nil && json.Valid(data) {
			return io.NopCloser(bytes.NewReader(data)), nil
		}
	}
	response, err := coverGET(ctx, client, address)
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

type coverHTTPError struct {
	code     int
	endpoint string
}

func (e coverHTTPError) Error() string {
	return fmt.Sprintf("cover lookup: HTTP %d", e.code)
}

func coverGET(ctx context.Context, client *http.Client, endpoint string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Elsewhere/1.0 (macOS local music player)")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, coverHTTPError{response.StatusCode, response.Request.URL.String()}
	}
	return response, nil
}
