package app

import (
	"elsewhere/internal/audio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

type track struct {
	path, title, artist, album, albumArtist string
	taggedAlbum, hasArtwork, jamAudio       bool
	source                                  *album
	likeID                                  string
	radioFormat                             string
	disc, number                            int
	trackGain, albumGain                    *float64
}

type album struct {
	title, artist        string
	tracks               []track
	cover                *audio.Cover
	loaded               bool
	artworkSize          int
	artworkRows          []string
	coverPromptDismissed bool
	likedSongs           bool
	isPlaylist           bool
	radio                bool
	description          string
	radioArtwork         <-chan *audio.Cover
}

func scanLibrary(root string) ([]*album, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", root)
	}

	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}

	var files []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil
		}
		if path != root && strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".mp3", ".m4a", ".aac", ".flac", ".wav", ".aif", ".aiff", ".caf", ".alac":
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	order := collate.New(language.Und, collate.Numeric)
	sort.SliceStable(files, func(i, j int) bool { return order.CompareString(files[i], files[j]) < 0 })
	tracks := make([]track, len(files))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(len(files), min(8, runtime.GOMAXPROCS(0))) {
		workers.Go(func() {
			for i := range jobs {
				path := files[i]
				meta := audio.ReadMetadata(path)
				taggedAlbum := meta.Album != ""
				if meta.Title == "" {
					meta.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
				}
				if meta.AlbumArtist == "" && taggedAlbum {
					meta.AlbumArtist = meta.Artist
				}
				if meta.Artist == "" {
					meta.Artist = meta.AlbumArtist
				}
				if meta.Artist == "" {
					meta.Artist = "Unknown artist"
				}
				if !taggedAlbum {
					meta.Album = filepath.Base(filepath.Dir(path))
				}
				tracks[i] = track{
					path: path, title: meta.Title, artist: meta.Artist, album: meta.Album,
					albumArtist: meta.AlbumArtist, taggedAlbum: taggedAlbum, hasArtwork: meta.Artwork,
					likeID: likeKey(path), disc: meta.Disc, number: meta.Number, trackGain: meta.TrackGain, albumGain: meta.AlbumGain,
				}
			}
		})
	}
	for i := range files {
		jobs <- i
	}
	close(jobs)
	workers.Wait()

	type albumKey struct{ folder, title, artist string }
	grouped := make(map[albumKey]*album)
	var albums []*album
	for _, t := range tracks {
		key := albumKey{folder: filepath.Dir(t.path), title: strings.ToLower(t.album)}
		if t.taggedAlbum && t.albumArtist != "" {
			key.folder = ""
			key.artist = strings.ToLower(t.albumArtist)
		}
		group := grouped[key]
		if group == nil {
			group = &album{title: t.album, artist: t.albumArtist}
			grouped[key] = group
			albums = append(albums, group)
		}
		t.source = group
		group.tracks = append(group.tracks, t)
	}
	for _, a := range albums {
		sort.SliceStable(a.tracks, func(i, j int) bool {
			left, right := a.tracks[i], a.tracks[j]
			if max(1, left.disc) != max(1, right.disc) {
				return max(1, left.disc) < max(1, right.disc)
			}
			if left.number == 0 {
				return false
			}
			return right.number == 0 || left.number < right.number
		})
	}
	sort.SliceStable(albums, func(i, j int) bool { return order.CompareString(albums[i].title, albums[j].title) < 0 })
	return albums, nil
}

func (a *album) artwork(preferred track) *audio.Cover {
	if a.loaded {
		return a.cover
	}
	a.loaded = true
	folder := filepath.Dir(preferred.path)
	for _, name := range []string{"cover", "folder", "front"} {
		for _, ext := range []string{".jpg", ".jpeg", ".png"} {
			path := filepath.Join(folder, name+ext)
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				if a.cover = audio.ReadCover(path, false); a.cover != nil {
					return a.cover
				}
			}
		}
	}
	if preferred.hasArtwork {
		a.cover = audio.ReadCover(preferred.path, true)
	}
	for _, t := range a.tracks {
		if a.cover != nil {
			break
		}
		if t.hasArtwork && t.path != preferred.path {
			a.cover = audio.ReadCover(t.path, true)
		}
	}
	return a.cover
}
