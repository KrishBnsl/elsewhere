package app

import (
	"context"
	"elsewhere/internal/audio"
	"elsewhere/internal/covers"
	"errors"
	"net/http"
	"os"
	"path/filepath"
)

type coverLookup struct {
	album  *album
	cancel context.CancelFunc
	result <-chan coverResult
}

type coverResult struct {
	art      *audio.Cover
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
	if !p.popup || t == nil || t.source == nil || t.jamAudio || p.searching || p.playlistDialog != nil || p.radioPicker != nil || p.queueView != nil || p.themePicker != nil {
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
		art, data, err := covers.Online(ctx, title, artist)
		if err != nil || art == nil {
			result <- coverResult{err: err}
			return
		}
		if ctx.Err() != nil {
			return
		}
		name, err := saveAlbumCover(folder, art, data)
		if err != nil {
			result <- coverResult{err: covers.StageError("Save failed", err)}
			return
		}
		result <- coverResult{art: art, filename: name}
	}(a.title, a.artist, filepath.Dir(t.path))
}

func saveAlbumCover(folder string, art *audio.Cover, data []byte) (string, error) {
	name := "cover.jpg"
	if http.DetectContentType(data) != "image/jpeg" {
		name = "cover.png"
		data = art.PNG
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
