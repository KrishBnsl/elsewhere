package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

func init() {
	runtime.LockOSThread()
}

func runTerminal(p *player) error {
	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	if !term.IsTerminal(in) || !term.IsTerminal(out) {
		return fmt.Errorf("a terminal is required")
	}
	restoreConsole, err := prepareConsole()
	if err != nil {
		return err
	}
	defer restoreConsole()
	original, err := term.MakeRaw(in)
	if err != nil {
		return fmt.Errorf("a terminal is required: %w", err)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, terminationSignals()...)
	defer signal.Stop(signals)
	var shownCover kittyPlacement
	p.kittyGraphics = supportsKittyGraphics()
	imageID := newKittyImageID()
	defer func() {
		if shownCover.art != nil {
			fmt.Fprint(os.Stdout, deleteKittyImage(imageID))
		}
		fmt.Fprint(os.Stdout, "\x1b[0m\x1b[?7h\x1b[?25h\x1b[?1049l")
		term.Restore(in, original)
	}()
	defer p.stopRadio()
	defer p.clearLiveStation()
	defer p.social.jam.clearAudio()
	defer stopAudio()
	startMediaControls()
	defer stopMediaControls()
	defer p.stopLyricsLookup()
	defer p.stopCoverLookup()
	// Runs first: tell Melisma where the song was left while it's still playing.
	defer func() { p.engine.close(audioState().elapsed) }()
	defer func() {
		if err := p.saveSession(); err != nil {
			fmt.Fprintln(os.Stderr, "Save session:", err)
		}
	}()
	if _, err := fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[?25l\x1b[?7l\x1b[2J"); err != nil {
		return err
	}

	// Keys arrive on a channel, so waiting for one can time out the same way everywhere.
	input := make(chan []byte, 16)
	readErr := make(chan error, 1)
	go func() {
		var buffer [256]byte
		for {
			n, err := os.Stdin.Read(buffer[:])
			if n > 0 {
				input <- append([]byte(nil), buffer[:n]...)
			}
			if err != nil {
				readErr <- err
				return
			}
		}
	}()

	var previous []string
	lastWidth, lastHeight := 0, 0
	var pending []byte
	for {
		select {
		case <-signals:
			return nil
		default:
		}
		p.tick(time.Now())
		width, height, err := term.GetSize(out)
		if err != nil {
			return err
		}
		if width == 0 || height == 0 {
			width, height = 80, 24
		}
		rows := p.frame(width, height)
		var output strings.Builder
		resized := width != lastWidth || height != lastHeight || len(rows) != len(previous)
		coverChanged := resized || shownCover != p.coverPlacement
		if coverChanged && shownCover.art != nil {
			output.WriteString(deleteKittyImage(imageID))
		}
		if resized {
			output.WriteString("\x1b[2J")
			previous = nil
		}
		for i, row := range rows {
			if i >= len(previous) || row != previous[i] {
				fmt.Fprintf(&output, "\x1b[%d;1H%s", i+1, row)
			}
		}
		if coverChanged && p.coverPlacement.art != nil {
			p.coverPlacement.write(&output, imageID)
		}
		shownCover = p.coverPlacement
		if output.Len() > 0 {
			if _, err := os.Stdout.WriteString(output.String()); err != nil {
				return err
			}
		}
		previous, lastWidth, lastHeight = rows, width, height

		timeout := 150
		if len(pending) > 0 {
			timeout = 35
		} else if p.popup && p.lyricsMode && p.state.playing {
			timeout = 75
		} else if p.popup && p.visual && p.state.playing && p.analysisAvailable {
			timeout = max(1, int((time.Until(p.nextAnalysis)+time.Millisecond-1)/time.Millisecond))
		}
		ready := false
		select {
		case data := <-input:
			pending, ready = append(pending, data...), true
		case err := <-readErr:
			if err == io.EOF {
				return nil
			}
			return err
		case <-signals:
			return nil
		case <-time.After(time.Duration(timeout) * time.Millisecond):
		}
		for len(pending) > 0 {
			key, n := nextKey(pending, !ready)
			if n == 0 {
				break
			}
			pending = pending[n:]
			if !p.handle(key) {
				return nil
			}
		}
	}
}

func nextKey(pending []byte, expired bool) (string, int) {
	if pending[0] == 27 {
		if len(pending) == 1 && !expired {
			return "", 0
		}
		if len(pending) >= 2 && (pending[1] == '[' || pending[1] == 'O') {
			for i, ch := range pending[2:] {
				if ch >= 64 && ch <= 126 {
					return "\x1b[" + string(pending[2:i+3]), i + 3
				}
			}
			if !expired {
				return "", 0
			}
			return "", len(pending)
		}
		return "\x1b", 1
	}
	if !utf8.FullRune(pending) && !expired {
		return "", 0
	}
	r, size := utf8.DecodeRune(pending)
	return string(r), size
}

func run(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: elsewhere [music-folder]")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, "Music")
	if len(args) == 1 {
		path = args[0]
		if path == "~" {
			path = home
		} else if strings.HasPrefix(path, "~/") {
			path = filepath.Join(home, path[2:])
		}
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	albums, err := scanLibrary(path)
	if err != nil {
		return err
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	likes, err := readLikes(filepath.Join(config, "Elsewhere", "likes"))
	if err != nil {
		return fmt.Errorf("read liked songs: %w", err)
	}
	playlistPath := filepath.Join(config, "Elsewhere", "playlists.json")
	lists, err := readPlaylists(playlistPath)
	if err != nil {
		return fmt.Errorf("read playlists: %w", err)
	}
	themePath := filepath.Join(config, "Elsewhere", "theme")
	if saved, err := os.ReadFile(themePath); err == nil {
		applyTheme(themeByName(string(saved)))
	}
	p := &player{themePath: themePath, albums: albums, path: path, volume: 70, output: -1, visual: true, likes: likes,
		playlistPath: playlistPath, playlistRecords: lists}
	p.rebuildPlaylists()
	p.refreshLikedSongs()
	p.sessionPath = filepath.Join(config, "Elsewhere", "sessions", fmt.Sprintf("%x.json", sha256.Sum256([]byte(path))))
	p.restoreSession()
	p.social.client, p.social.err = newFriendsClient(filepath.Join(config, "Elsewhere"))
	if p.social.client != nil && p.social.client.token != "" {
		p.social.next = time.Now()
		if listener, err := os.ReadFile(p.social.client.credentialPath + ".taste-user"); err == nil {
			p.social.taste.listener = strings.TrimSpace(string(listener))
		}
	}
	p.engineReady = startEngine()
	return runTerminal(p)
}

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		fmt.Println("Usage: elsewhere [music-folder] | elsewhere update | elsewhere --version")
		return
	}
	var err error
	switch {
	case len(os.Args) == 2 && os.Args[1] == "--version":
		fmt.Println("Elsewhere " + buildVersion)
		return
	case len(os.Args) >= 2 && os.Args[1] == "update":
		switch {
		case len(os.Args) == 2:
			err = updateRelease()
		case len(os.Args) == 3 && (os.Args[2] == "--help" || os.Args[2] == "-h"):
			fmt.Println("Usage: elsewhere update")
			return
		default:
			err = fmt.Errorf("usage: elsewhere update")
		}
	default:
		err = run(os.Args[1:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Elsewhere:", err)
		os.Exit(1)
	}
}
