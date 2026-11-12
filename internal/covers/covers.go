// Package covers finds album art online: MusicBrainz for the release, the Cover Art Archive for the image.
package covers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"elsewhere/internal/audio"
	"elsewhere/internal/fsutil"
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

// MaxBytes bounds any downloaded cover image.
const MaxBytes = 2 << 20

var remasterSuffix = regexp.MustCompile(`(?i)\s*[(\[](?:(?:19|20)\d{2}\s+)?remaster(?:ed)?(?:\s+(?:19|20)\d{2})?[)\]]$`)

var releaseGroupID = regexp.MustCompile(`^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$`)

var coverSearchRate struct {
	sync.Mutex
	next time.Time
}

func Online(ctx context.Context, title, artist string) (*audio.Cover, []byte, error) {
	title = strings.TrimSpace(remasterSuffix.ReplaceAllString(title, ""))
	var filename string
	if cache, err := os.UserCacheDir(); err == nil {
		key := sha256.Sum256([]byte(strings.ToLower(artist + "\x00" + title)))
		filename = filepath.Join(cache, "Elsewhere", "covers", fmt.Sprintf("%x.img", key))
		if info, err := os.Stat(filename); err == nil && info.Mode().IsRegular() && info.Size() <= MaxBytes {
			if data, err := os.ReadFile(filename); err == nil && len(data) <= MaxBytes {
				if art := audio.ReadCoverBytes(data); art != nil {
					return art, data, nil
				}
			}
		}
	}

	search := &http.Client{Timeout: 8 * time.Second}
	group, err := findReleaseGroup(ctx, search, releaseGroupSearch, title, artist)
	if err != nil {
		return nil, nil, StageError("MusicBrainz search", err)
	}
	if group == "" {
		return nil, nil, nil
	}
	client := &http.Client{Timeout: 12 * time.Second}
	art, data, err := downloadCover(ctx, client, coverArchive+group+"/front-250")
	if err == nil && art != nil && filename != "" {
		_ = fsutil.WriteFile(filename, data)
	}
	if err != nil {
		return nil, nil, StageError("Image download", err)
	}
	return art, data, nil
}

func StageError(stage string, err error) error {
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

func downloadCover(ctx context.Context, client *http.Client, endpoint string) (*audio.Cover, []byte, error) {
	if strings.HasPrefix(endpoint, coverArchive) {
		if data, err := curlCover(ctx, endpoint, MaxBytes); err == nil {
			if art := audio.ReadCoverBytes(data); art != nil {
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
		data, err := io.ReadAll(io.LimitReader(response.Body, MaxBytes+1))
		response.Body.Close()
		if err != nil {
			lastError = err
			continue
		}
		if len(data) > MaxBytes {
			return nil, nil, fmt.Errorf("cover image exceeds size limit")
		}
		if art := audio.ReadCoverBytes(data); art != nil {
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
