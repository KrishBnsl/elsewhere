package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

func init() {
	runtime.LockOSThread()
}

func runTerminal(p *player) error {
	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	original, err := unix.IoctlGetTermios(in, unix.TIOCGETA)
	if err != nil {
		return fmt.Errorf("a terminal is required: %w", err)
	}
	if _, err := unix.IoctlGetWinsize(out, unix.TIOCGWINSZ); err != nil {
		return fmt.Errorf("a terminal is required: %w", err)
	}
	raw := *original
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN], raw.Cc[unix.VTIME] = 1, 0

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, unix.SIGTERM, unix.SIGHUP)
	defer signal.Stop(signals)
	if err := unix.IoctlSetTermios(in, unix.TIOCSETAF, &raw); err != nil {
		return err
	}
	var shownCover kittyPlacement
	p.kittyGraphics = supportsKittyGraphics()
	imageID := newKittyImageID()
	defer func() {
		if shownCover.art != nil {
			fmt.Fprint(os.Stdout, deleteKittyImage(imageID))
		}
		fmt.Fprint(os.Stdout, "\x1b[0m\x1b[?7h\x1b[?25h\x1b[?1049l")
		unix.IoctlSetTermios(in, unix.TIOCSETAF, original)
	}()
	defer p.stopRadio()
	defer p.clearLiveStation()
	defer stopAudio()
	defer p.stopLyricsLookup()
	defer p.stopCoverLookup()
	// Runs first: tell Melisma where the song was left while it's still playing.
	defer func() { p.engine.close(audioState().elapsed) }()
	if _, err := fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[?25l\x1b[?7l\x1b[2J"); err != nil {
		return err
	}

	var previous []string
	lastWidth, lastHeight := 0, 0
	poll := []unix.PollFd{{Fd: int32(in), Events: unix.POLLIN}}
	var buffer [256]byte
	var pending []byte
	for {
		select {
		case <-signals:
			return nil
		default:
		}
		p.tick(time.Now())
		size, err := unix.IoctlGetWinsize(out, unix.TIOCGWINSZ)
		if err != nil {
			return err
		}
		width, height := int(size.Col), int(size.Row)
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
		ready, err := unix.Poll(poll, timeout)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if ready > 0 {
			n, err := unix.Read(in, buffer[:])
			if err == unix.EINTR {
				continue
			}
			if err != nil || n == 0 {
				return err
			}
			pending = append(pending, buffer[:n]...)
		}
		for len(pending) > 0 {
			key, n := nextKey(pending, ready == 0)
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
	p.engineReady = startEngine()
	return runTerminal(p)
}

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		fmt.Println("Usage: elsewhere [music-folder]")
		return
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Elsewhere:", err)
		os.Exit(1)
	}
}
