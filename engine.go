package main

// The Melisma engine: a recommender that runs beside Elsewhere as `dhunn engine`. Elsewhere
// plays the music; the engine chooses what comes next and learns from what you play, finish,
// skip and like. They talk in JSON lines over the engine's stdin and stdout.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type engine struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	in      *json.Encoder
	out     *bufio.Scanner
	closeIn func() error
	user    string
	tracks  int
	mode    string
	explore float64
	dead    bool
}

// enginePick is one song the engine chose, with its reasons.
type enginePick struct {
	OK      bool     `json:"ok"`
	Error   string   `json:"error"`
	Done    bool     `json:"done"`
	Path    string   `json:"path"`
	Title   string   `json:"title"`
	Artist  string   `json:"artist"`
	Album   string   `json:"album"`
	Why     []string `json:"why"`
	Facts   string   `json:"facts"`
	Mode    string   `json:"mode"`
	Explore float64  `json:"exploration"`
}

type engineReply struct {
	OK      bool         `json:"ok"`
	Error   string       `json:"error"`
	Message string       `json:"message"`
	Known   bool         `json:"known"`
	User    string       `json:"user"`
	Tracks  int          `json:"tracks"`
	Mode    string       `json:"mode"`
	Explore *float64     `json:"exploration"`
	Songs   []enginePick `json:"songs"`
	Paths   []string     `json:"paths"`
	Lines   []string     `json:"lines"`
	Added   int          `json:"added"`
}

type engineStart struct {
	engine *engine
	err    error
}

// engineCommand finds the engine: $MELISMA_ENGINE, else `dhunn` on the PATH.
func engineCommand() (string, error) {
	if path := os.Getenv("MELISMA_ENGINE"); path != "" {
		if path == "off" {
			return "", errors.New("turned off")
		}
		return path, nil
	}
	path, err := exec.LookPath("dhunn")
	if err != nil {
		return "", errors.New("dhunn isn't installed")
	}
	return path, nil
}

// logFile is where the engine's own messages go, never over the player's screen.
func logFile(name string) *os.File {
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil
	}
	dir = filepath.Join(dir, "Elsewhere")
	if os.MkdirAll(dir, 0700) != nil {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return nil
	}
	return f
}

// startEngine launches the engine in the background; the result arrives on the channel.
// Startup reads the whole library, so it can take a moment.
func startEngine() <-chan engineStart {
	ready := make(chan engineStart, 1)
	go func() {
		e, err := launchEngine()
		ready <- engineStart{e, err}
	}()
	return ready
}

func launchEngine() (*engine, error) {
	path, err := engineCommand()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, "engine")
	if f := logFile("engine.log"); f != nil {
		cmd.Stderr = f
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	out := bufio.NewScanner(stdout)
	out.Buffer(make([]byte, 64*1024), 4*1024*1024)
	e := &engine{cmd: cmd, in: json.NewEncoder(stdin), out: out, closeIn: stdin.Close}
	hello, err := e.request(map[string]any{"op": "hello"})
	if err != nil {
		e.kill()
		return nil, err
	}
	e.user, e.tracks = hello.User, hello.Tracks
	return e, nil
}

// call sends one request and reads its reply into reply (which may be nil).
func (e *engine) call(request map[string]any, reply any) error {
	if e == nil {
		return errors.New("no engine")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.dead {
		return errors.New("the engine stopped")
	}
	if err := e.in.Encode(request); err != nil {
		e.dead = true
		return err
	}
	if !e.out.Scan() {
		e.dead = true
		if err := e.out.Err(); err != nil {
			return err
		}
		return errors.New("the engine stopped (see engine.log in Elsewhere's cache folder)")
	}
	if reply == nil {
		return nil
	}
	return json.Unmarshal(e.out.Bytes(), reply)
}

// request sends a request, and fails if the engine says no. Replies that carry the mode or
// exploration level update them.
func (e *engine) request(request map[string]any) (engineReply, error) {
	var reply engineReply
	if err := e.call(request, &reply); err != nil {
		return reply, err
	}
	if !reply.OK {
		return reply, errors.New(reply.Error)
	}
	if reply.Mode != "" {
		e.mode = reply.Mode
	}
	if reply.Explore != nil {
		e.explore = *reply.Explore
	}
	return reply, nil
}

// stopped reports whether the engine has gone away.
func (e *engine) stopped() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dead
}

// tell reports something that happened; failures only matter to the engine.
func (e *engine) tell(op string, fields ...any) {
	if e == nil {
		return
	}
	request := map[string]any{"op": op}
	for i := 0; i+1 < len(fields); i += 2 {
		request[fields[i].(string)] = fields[i+1]
	}
	e.call(request, nil)
}

func (e *engine) next() (enginePick, error) {
	var pick enginePick
	if err := e.call(map[string]any{"op": "next"}, &pick); err != nil {
		return pick, err
	}
	if !pick.OK {
		return pick, errors.New(pick.Error)
	}
	if pick.Mode != "" {
		e.mode, e.explore = pick.Mode, pick.Explore
	}
	return pick, nil
}

// upcoming forecasts the engine's next n picks if you listen straight through.
func (e *engine) upcoming(n int) ([]enginePick, error) {
	reply, err := e.request(map[string]any{"op": "upcoming", "n": n})
	return reply.Songs, err
}

// steer sends like, dislike, further, back or surprise and returns what the engine says.
func (e *engine) steer(op string, path string, pos float64) (string, error) {
	request := map[string]any{"op": op, "pos": pos}
	if path != "" {
		request["path"] = path
	}
	reply, err := e.request(request)
	return reply.Message, err
}

// setMode changes direction: {"mode": "usual"}, {"to": "…"}, {"vibe": "…"} or {"friend": "…"}.
func (e *engine) setMode(fields map[string]any) error {
	fields["op"] = "mode"
	_, err := e.request(fields)
	return err
}

// library lists the paths of every song the engine knows.
func (e *engine) library() (map[string]bool, error) {
	reply, err := e.request(map[string]any{"op": "library"})
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(reply.Paths))
	for _, path := range reply.Paths {
		known[path] = true
	}
	return known, nil
}

// close ends the session cleanly, or gives up after a moment.
func (e *engine) close(pos float64) {
	if e == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		e.tell("quit", "pos", pos)
		e.closeIn()
		e.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		e.kill()
	}
}

func (e *engine) kill() {
	if e.cmd.Process != nil {
		e.cmd.Process.Kill()
	}
}

// ---- The player's side ----

// engineTell reports an event about the playing song when the engine is running.
func (p *player) engineTell(op string) {
	if p.engine == nil {
		return
	}
	p.engine.tell(op, "pos", p.state.elapsed)
}

// leaving reports that the current song is being left before its end, for a reason that
// says nothing about the song ("choose"), or because you skipped it ("skip").
func (p *player) leaving(op string) {
	if p.active != nil && !p.finished && p.live == nil && !p.active.radio {
		p.engine.tell(op, "pos", audioState().elapsed)
	}
}

// localSong is the playing song, if it's a local file (not radio).
func (p *player) localSong() *track {
	t := p.current()
	if t == nil || t.radioFormat != "" || p.live != nil || p.active.radio {
		return nil
	}
	return t
}

// findTrack locates a file in the scanned library.
func (p *player) findTrack(path string) (*album, int) {
	for _, a := range p.albums {
		for i, t := range a.tracks {
			if t.path == path {
				return a, i
			}
		}
	}
	return nil, -1
}

// playPick plays a song the engine chose (or forecast). It reports whether it started.
func (p *player) playPick(pick enginePick) bool {
	a, i := p.findTrack(pick.Path)
	if a == nil {
		// A song outside the folder Elsewhere scanned: play it on its own.
		t := track{path: pick.Path, title: pick.Title, artist: pick.Artist, album: pick.Album,
			albumArtist: pick.Artist, likeID: likeKey(pick.Path), hasArtwork: true}
		a = &album{title: pick.Album, artist: pick.Artist, tracks: []track{t}}
		a.tracks[0].source = a
		i = 0
	}
	p.picked = &pick
	if !p.start(a, i) {
		p.picked = nil
		return false
	}
	p.stopRadio()
	p.clearLiveStation()
	p.melisma = true
	return true
}

// playMelisma plays the engine's next pick. It reports whether something started.
func (p *player) playMelisma() bool {
	if p.engine == nil {
		p.notice = "Melisma isn't running: " + p.engineStatus()
		return false
	}
	for range 5 {
		pick, err := p.engine.next()
		if err != nil {
			p.notice = "Melisma: " + err.Error()
			p.melisma = false
			return false
		}
		if p.playPick(pick) {
			return true
		}
	}
	return false
}

// startMelisma hands the choosing to Melisma. A song that's playing keeps playing, and
// Melisma carries on from it; otherwise its first pick starts now.
func (p *player) startMelisma() {
	if p.engine == nil {
		p.notice = "Melisma isn't running: " + p.engineStatus()
		return
	}
	p.leaveQueue()
	if t := p.localSong(); t != nil && !p.finished {
		p.engine.tell("from", "path", t.path)
		p.melisma, p.handover = true, true
		p.popup = true
		p.notice = "Melisma takes over after this song · " + p.engine.mode
		return
	}
	if p.playMelisma() {
		p.popup = true
		p.notice = "Melisma is choosing · " + p.engine.mode
	}
}

// steerMelisma sends like/dislike/further/back/surprise about the playing song.
func (p *player) steerMelisma(op string) {
	t := p.localSong()
	if p.engine == nil {
		p.notice = "Melisma isn't running: " + p.engineStatus()
		return
	}
	if t == nil {
		return
	}
	message, err := p.engine.steer(op, t.path, audioState().elapsed)
	if err != nil {
		p.notice = "Melisma: " + err.Error()
		return
	}
	p.notice = "Melisma: " + message
	if op == "dislike" {
		p.finished = true
		p.melisma = true
		if !p.advance() {
			stopAudio()
		}
	}
}

// setMelismaMode changes where Melisma heads, and lets it choose from here on.
func (p *player) setMelismaMode(fields map[string]any, what string) {
	if p.engine == nil {
		p.notice = "Melisma isn't running: " + p.engineStatus()
		return
	}
	if err := p.engine.setMode(fields); err != nil {
		p.notice = "Melisma: " + err.Error()
		return
	}
	// Kept so a restarted engine (after /analyze) heads the same way.
	p.melismaMode = fields
	if !p.melisma {
		p.startMelisma()
	}
	if p.melisma {
		p.notice = "Melisma: " + what + " · " + p.engine.mode
	}
}

// pollEngine picks up the engine once it has started, and notices if it stops.
func (p *player) pollEngine() {
	if p.engine.stopped() {
		p.engine, p.melisma, p.engineErr = nil, false, "stopped"
		p.notice = "Melisma stopped · see engine.log in Elsewhere's cache folder"
	}
	if p.engineReady == nil {
		return
	}
	select {
	case started := <-p.engineReady:
		p.engineReady = nil
		if started.err != nil {
			p.engineErr = started.err.Error()
			return
		}
		p.engine, p.engineErr = started.engine, ""
		if known, err := p.engine.library(); err == nil {
			p.known = known
		}
		if p.melismaMode != nil {
			p.engine.setMode(p.melismaMode)
		}
		p.syncLikes()
		// A song that started before the engine was ready still counts.
		if t := p.localSong(); t != nil && !p.finished {
			p.engine.tell("played", "path", t.path)
			if p.melisma {
				p.engine.tell("from", "path", t.path)
			}
		}
	default:
	}
}

// syncLikes tells Melisma about songs liked in Elsewhere (it adds only new ones).
func (p *player) syncLikes() {
	var paths []string
	for _, a := range p.albums {
		for _, t := range a.tracks {
			if p.likes.contains(t.likeID) {
				paths = append(paths, t.path)
			}
		}
	}
	if len(paths) > 0 {
		p.engine.request(map[string]any{"op": "likes", "paths": paths})
	}
}

// engineStatus is a short description of the engine for the header and notices.
func (p *player) engineStatus() string {
	switch {
	case p.analysis != nil:
		return p.analysis.status()
	case p.engine != nil:
		state := fmt.Sprintf("%d songs · %s %.2f", p.engine.tracks, p.engine.mode, p.engine.explore)
		if p.melisma {
			return "▶ choosing · " + state
		}
		return "● ready · " + state
	case p.engineReady != nil:
		return "starting…"
	case p.engineErr == "turned off":
		return "off"
	case p.engineErr != "":
		return p.engineErr
	}
	return "off"
}

// unknown reports whether Melisma is running and hasn't analyzed this song.
func (p *player) unknown(t track) bool {
	return p.engine != nil && p.known != nil && t.radioFormat == "" && !p.known[t.path]
}

// openQueue shows the queue, with what Melisma would play after it.
func (p *player) openQueue() {
	p.queueView = &queueView{}
	p.refreshForecast()
}

// refreshForecast asks Melisma what comes after the queue, when it's choosing.
func (p *player) refreshForecast() {
	if p.queueView == nil {
		return
	}
	p.queueView.upcoming = nil
	if p.engine == nil || !p.melisma {
		return
	}
	if songs, err := p.engine.upcoming(5); err == nil {
		p.queueView.upcoming = songs
	}
}

func (p *player) melismaHeading() string {
	if p.engine == nil {
		return ""
	}
	return fmt.Sprintf("MELISMA · %s · %.2f", p.engine.mode, p.engine.explore)
}

// ---- /taste: what Melisma has learned ----

type tasteView struct{ lines []string }

func (p *player) openTaste() {
	if p.engine == nil {
		p.notice = "Melisma isn't running: " + p.engineStatus()
		return
	}
	reply, err := p.engine.request(map[string]any{"op": "profile"})
	if err != nil {
		p.notice = "Melisma: " + err.Error()
		return
	}
	p.tasteView = &tasteView{lines: reply.Lines}
}

func (p *player) overlayTaste(rows []string, width int) {
	columns := min(96, width-4)
	row := func(text, style string) string {
		return boxSide(style + cell(text, columns-2) + panel + amber)
	}
	dialog := []string{boxTop(" WHAT MELISMA HAS LEARNED ", columns)}
	limit := max(3, len(rows)-6)
	for i, line := range p.tasteView.lines {
		if i >= limit {
			break
		}
		style := dim
		if i == 0 || strings.HasPrefix(line, "Taste") || strings.HasPrefix(line, "Skip") {
			style = amber
		}
		dialog = append(dialog, row(" "+line, style))
	}
	dialog = append(dialog, row(" Esc Close", dim), boxBottom(columns))
	x, y := (width-columns)/2+1, max(0, (len(rows)-len(dialog))/2)
	cursor := fmt.Sprintf("\x1b[%dG", x)
	for i, line := range dialog {
		if y+i < len(rows) {
			rows[y+i] += cursor + panel + amber + line + reset
		}
	}
}

// ---- /analyze: teach Melisma new songs ----

type analysis struct {
	mu          sync.Mutex
	done, total int
	finished    bool
	err         error
	summary     string
}

var progressLine = regexp.MustCompile(`\[\s*(\d+)/(\d+)\]`)

func (a *analysis) status() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.total == 0 {
		return "analyzing · looking for new songs…"
	}
	return fmt.Sprintf("analyzing %d/%d", a.done, a.total)
}

// startAnalysis runs `dhunn analyze` on the music folder in the background.
func (p *player) startAnalysis() {
	if p.analysis != nil {
		p.notice = "Already analyzing: " + p.analysis.status()
		return
	}
	path, err := engineCommand()
	if err != nil {
		p.notice = "Can't analyze: " + err.Error()
		return
	}
	cmd := exec.Command(path, "analyze", p.path)
	if f := logFile("analyze.log"); f != nil {
		cmd.Stderr = f
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		p.notice = "Can't analyze: " + err.Error()
		return
	}
	if err := cmd.Start(); err != nil {
		p.notice = "Can't analyze: " + err.Error()
		return
	}
	a := &analysis{}
	p.analysis = a
	p.notice = "Melisma is listening to new songs in " + p.path
	go func() {
		lines := bufio.NewScanner(stdout)
		for lines.Scan() {
			text := lines.Text()
			a.mu.Lock()
			if m := progressLine.FindStringSubmatch(text); m != nil {
				a.done, _ = strconv.Atoi(m[1])
				a.total, _ = strconv.Atoi(m[2])
			}
			if strings.HasPrefix(text, "Done in") {
				a.summary = text
			}
			a.mu.Unlock()
		}
		err := cmd.Wait()
		a.mu.Lock()
		a.finished, a.err = true, err
		a.mu.Unlock()
	}()
}

// tickAnalysis restarts the engine when an analysis finishes, so it knows the new songs.
func (p *player) tickAnalysis() {
	a := p.analysis
	if a == nil {
		return
	}
	a.mu.Lock()
	finished, err, summary := a.finished, a.err, a.summary
	a.mu.Unlock()
	if !finished {
		return
	}
	p.analysis = nil
	if err != nil {
		p.notice = "Analysis failed: " + err.Error() + " (see analyze.log in Elsewhere's cache folder)"
		return
	}
	if strings.Contains(summary, " 0 analyzed,") {
		p.notice = "Melisma already knows every song in " + p.path
	} else {
		p.notice = "Melisma analyzed your music · " + strings.TrimPrefix(summary, "Done in ")
	}
	// The engine reads the library when it starts: start it again, keeping your place.
	if p.engine != nil {
		old, choosing := p.engine, p.melisma
		p.engine = nil
		old.close(audioState().elapsed)
		p.engineReady = startEngine()
		p.melisma = choosing
	} else if p.engineReady == nil {
		p.engineReady = startEngine()
	}
}
