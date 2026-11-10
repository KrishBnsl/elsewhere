package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestMain lets this test program stand in for `dhunn engine` when the tests below start it
// as the engine (with FAKE_ENGINE=1), on every platform.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_ENGINE") == "1" {
		fakeEngineMain()
	}
	os.Exit(m.Run())
}

func fakeEngineMain() {
	in := bufio.NewScanner(os.Stdin)
	log, _ := os.OpenFile(os.Getenv("FAKE_ENGINE_LOG"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	for in.Scan() {
		var req map[string]any
		json.Unmarshal(in.Bytes(), &req)
		fmt.Fprintln(log, req["op"], req["path"], req["to"])
		switch req["op"] {
		case "hello":
			fmt.Println(`{"ok":true,"user":"krish","tracks":2,"mode":"drift"}`)
		case "next":
			fmt.Println(`{"ok":true,"path":"/music/b.flac","title":"B","artist":"X","why":["close to A"],"mode":"drift"}`)
		case "upcoming":
			fmt.Println(`{"ok":true,"songs":[{"path":"/music/c.flac","title":"C","why":["near B"]}]}`)
		case "library":
			fmt.Println(`{"ok":true,"paths":["/music/a.flac","/music/b.flac"]}`)
		case "mode":
			fmt.Println(`{"ok":true,"mode":"toward Sonny Boy","exploration":0.5}`)
		case "profile":
			fmt.Println(`{"ok":true,"lines":["krish · 23 plays"]}`)
		case "die":
			os.Exit(1)
		case "further":
			fmt.Println(`{"ok":true,"message":"further out","mode":"drift"}`)
		case "quit":
			fmt.Println(`{"ok":true}`)
			os.Exit(0)
		default:
			fmt.Println(`{"ok":true}`)
		}
	}
	os.Exit(0)
}

// fakeEngine points MELISMA_ENGINE at the fake and returns where its requests are logged.
func fakeEngine(t *testing.T) string {
	logPath := t.TempDir() + "/requests"
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The engine inherits these, so `<this test program> engine` runs fakeEngineMain.
	t.Setenv("FAKE_ENGINE", "1")
	t.Setenv("FAKE_ENGINE_LOG", logPath)
	t.Setenv("MELISMA_ENGINE", exe)
	return logPath
}

func TestEngineConversation(t *testing.T) {
	logPath := fakeEngine(t)
	started := <-startEngine()
	if started.err != nil {
		t.Fatal(started.err)
	}
	e := started.engine
	if e.user != "krish" || e.tracks != 2 || e.mode != "drift" {
		t.Fatalf("hello: %+v", e)
	}
	pick, err := e.next()
	if err != nil || pick.Path != "/music/b.flac" || len(pick.Why) != 1 {
		t.Fatalf("next: %+v %v", pick, err)
	}
	e.tell("played", "path", pick.Path)
	if songs, err := e.upcoming(5); err != nil || len(songs) != 1 || songs[0].Title != "C" {
		t.Fatalf("upcoming: %+v %v", songs, err)
	}
	if msg, err := e.steer("further", "", 3); err != nil || msg != "further out" {
		t.Fatalf("steer: %q %v", msg, err)
	}
	e.close(12)
	requests, _ := os.ReadFile(logPath)
	want := "hello <nil> <nil>\nnext <nil> <nil>\nplayed /music/b.flac <nil>\nupcoming <nil> <nil>\nfurther <nil> <nil>\nquit <nil> <nil>\n"
	if string(requests) != want {
		t.Fatalf("requests:\n%s\nwant:\n%s", requests, want)
	}
}

func TestEngineOff(t *testing.T) {
	t.Setenv("MELISMA_ENGINE", "off")
	if started := <-startEngine(); started.err == nil {
		t.Fatal("engine should be off")
	}
	// Without an engine, the player's hooks do nothing.
	var e *engine
	e.tell("played", "path", "x")
	e.close(0)
}

func TestPlayerWithEngine(t *testing.T) {
	logPath := fakeEngine(t)
	a := &album{title: "Sonny Boy", tracks: []track{
		{path: "/music/a.flac", title: "A", likeID: likeKey("/music/a.flac")},
		{path: "/music/z.flac", title: "Z", likeID: likeKey("/music/z.flac")},
	}}
	for i := range a.tracks {
		a.tracks[i].source = a
	}
	likes := songLikes{dir: t.TempDir(), entries: map[string]bool{likeKey("/music/a.flac"): true}}
	p := &player{albums: []*album{a}, likes: likes, engineReady: startEngine()}
	p.refreshLikedSongs()
	for p.engine == nil && p.engineErr == "" {
		p.pollEngine()
	}
	if p.engine == nil {
		t.Fatal(p.engineErr)
	}
	// Songs Melisma hasn't analyzed are marked.
	if p.unknown(a.tracks[0]) || !p.unknown(a.tracks[1]) {
		t.Fatalf("known: %v", p.known)
	}
	if status := p.engineStatus(); status != "● ready · 2 songs · drift 0.00" {
		t.Fatalf("status %q", status)
	}
	// A song is playing: /toward hands over after it, without stopping it.
	p.active, p.playingIndex = a, 0
	if !p.runSlashCommand("toward sonny boy", searchPrompt{}) {
		t.Fatal("toward not handled")
	}
	if !p.melisma || !p.handover || p.engine.mode != "toward Sonny Boy" {
		t.Fatalf("melisma %v handover %v mode %q notice %q", p.melisma, p.handover, p.engine.mode, p.notice)
	}
	// Words are needed.
	p.runSlashCommand("vibe", searchPrompt{})
	if !strings.Contains(p.notice, "/vibe rainy night drive") {
		t.Fatalf("notice %q", p.notice)
	}
	// Unliking tells Melisma.
	p.popup = true
	p.toggleLike()
	p.runSlashCommand("taste", searchPrompt{})
	if p.tasteView == nil || p.tasteView.lines[0] != "krish · 23 plays" {
		t.Fatalf("taste %+v", p.tasteView)
	}
	// If the engine stops, the player notices and carries on without it.
	p.engine.tell("die")
	p.pollEngine()
	if p.engine != nil || p.melisma || p.engineStatus() != "stopped" {
		t.Fatalf("after stop: %v %v %q", p.engine, p.melisma, p.engineStatus())
	}
	requests, _ := os.ReadFile(logPath)
	want := "hello <nil> <nil>\nlibrary <nil> <nil>\nlikes <nil> <nil>\nmode <nil> sonny boy\nfrom /music/a.flac <nil>\nunlike /music/a.flac <nil>\nprofile <nil> <nil>\ndie <nil> <nil>\n"
	if string(requests) != want {
		t.Fatalf("requests:\n%s\nwant:\n%s", requests, want)
	}
}
