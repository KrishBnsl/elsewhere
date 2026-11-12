package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"elsewhere/internal/friendshare"
)

func TestJamTransfersMissingTrackAndStartsWhenReady(t *testing.T) {
	setup := rand.Text()
	serverHandler, err := friendshare.Open(filepath.Join(t.TempDir(), "friends.json"), setup)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(serverHandler)
	defer server.Close()
	host := friendsClient{base: server.URL, token: rand.Text()}
	guest := friendsClient{base: server.URL, token: rand.Text()}
	outsider := friendsClient{base: server.URL, token: rand.Text()}
	call := func(client friendsClient, op string, req friendshare.Request) friendshare.Reply {
		t.Helper()
		reply, err := client.call(op, req)
		if err != nil {
			t.Fatal(err)
		}
		return reply
	}
	call(host, "connect", friendshare.Request{Name: "Host", Code: setup})
	call(outsider, "connect", friendshare.Request{Name: "Outsider", Code: setup})
	invite := call(host, "invite", friendshare.Request{}).Code
	peer := call(guest, "accept", friendshare.Request{Name: "Guest", Code: invite}).Me
	call(host, "jam-create", friendshare.Request{Friend: peer.ID})
	song := friendshare.Song{Hash: "0123456789abcdef", Title: "Shared track"}
	room := call(host, "jam-add", friendshare.Request{Song: song, Duration: 5000}).Jam
	id := room.Queue[0].ID
	call(guest, "jam-join", friendshare.Request{})
	hostState := call(host, "jam-ready", friendshare.Request{Ready: []string{id}})
	guestState := call(guest, "home", friendshare.Request{})

	path := filepath.Join(t.TempDir(), "track.mp3")
	audio := []byte("test transfer contents")
	if err := os.WriteFile(path, audio, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := guest.transferJamAudio(context.Background(), id, path); err == nil {
		t.Fatal("guest must not upload a track queued by the host")
	}
	h := player{social: friendsState{client: &host, reply: hostState, lastReady: time.Now()}}
	g := player{social: friendsState{client: &guest, reply: guestState, lastReady: time.Now()}}
	h.social.jam.id = room.ID
	h.social.jam.checked = time.Now().Add(time.Minute)
	h.social.jam.paths = map[string]string{song.Hash: path}
	defer h.social.jam.clearAudio()
	defer g.social.jam.clearAudio()
	h.jamTransport("play")
	if !h.social.jam.waitingToPlay {
		t.Fatal("play should wait for the guest's audio")
	}

	deadline := time.Now().Add(3 * time.Second)
	for g.social.jam.received[id] == "" && time.Now().Before(deadline) {
		h.tickJamAudio(time.Now(), hostState.Jam, hostState.Me.ID)
		// Advance retry time so the test doesn't wait for the network polling interval.
		g.tickJamAudio(time.Now().Add(time.Minute), guestState.Jam, guestState.Me.ID)
		g.social.jam.transferNext = time.Time{}
		time.Sleep(time.Millisecond)
	}
	received := g.social.jam.received[id]
	data, err := os.ReadFile(received)
	if err != nil || !bytes.Equal(data, audio) {
		t.Fatalf("guest did not get the queued audio: %q, %v", data, err)
	}
	if leaked, err := outsider.transferJamAudio(context.Background(), id, ""); err != nil || leaked != "" {
		t.Fatalf("outsider must not be able to read jam audio: %q, %v", leaked, err)
	}

	g.tickJam(time.Now())
	select {
	case result := <-g.social.result:
		if result.err != nil || !result.reply.Jam.CanPlay() {
			t.Fatalf("guest did not automatically become ready: %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("guest did not report readiness")
	}
	h.social.reply = call(host, "home", friendshare.Request{})
	h.tickJam(time.Now())
	select {
	case result := <-h.social.result:
		if result.err != nil || !result.reply.Jam.Playing {
			t.Fatalf("host did not start playback when ready: %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("host did not request playback")
	}
	call(guest, "jam-leave", friendshare.Request{})
	g.social.reply = call(guest, "home", friendshare.Request{})
	g.tickJam(time.Now())
	if _, err := os.Stat(received); !os.IsNotExist(err) {
		t.Fatalf("received audio still exists after leaving: %v", err)
	}
}

func TestJamSendsLargeFLACAsMP3(t *testing.T) {
	ffmpeg := jamFFmpeg()
	if ffmpeg == "" {
		t.Skip("FFmpeg unavailable")
	}
	encoders, err := exec.Command(ffmpeg, "-hide_banner", "-encoders").Output()
	if err != nil || !bytes.Contains(encoders, []byte("libmp3lame")) {
		t.Skip("FFmpeg LAME encoder unavailable")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "large.flac")
	if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi",
		"-i", "anoisesrc=r=96000:d=80:a=0.9:s=1", "-f", "lavfi",
		"-i", "anoisesrc=r=96000:d=80:a=0.9:s=2", "-filter_complex", "amerge=inputs=2",
		"-c:a", "flac", "-sample_fmt", "s32", source).CombinedOutput(); err != nil {
		t.Fatalf("generate FLAC: %v: %s", err, out)
	}
	before, err := os.Stat(source)
	if err != nil || before.Size() <= friendshare.MaxJamAudio {
		t.Fatalf("fixture must exceed transfer limit: %v, %v", before, err)
	}

	received := filepath.Join(dir, "received.mp3")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.Header.Get("X-Jam-Audio-Extension") != ".mp3" ||
			r.ContentLength <= 0 || r.ContentLength >= friendshare.MaxJamAudio {
			http.Error(w, "expected a bounded MP3 upload", http.StatusBadRequest)
			return
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, friendshare.MaxJamAudio))
		if err != nil || os.WriteFile(received, data, 0600) != nil {
			http.Error(w, "could not read upload", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := friendsClient{base: server.URL, token: rand.Text()}
	if _, err := client.transferJamAudio(context.Background(), "entry", source); err != nil {
		t.Fatal(err)
	}
	pcm, err := exec.Command(ffmpeg, "-v", "error", "-f", "mp3", "-i", received,
		"-ar", "8000", "-ac", "1", "-f", "s16le", "-").Output()
	if err != nil || len(pcm) != 80*8000*2 {
		t.Fatalf("expected 80 seconds of decodable MP3: got %d PCM bytes, %v", len(pcm), err)
	}
	after, err := os.Stat(source)
	if err != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("source FLAC changed")
	}
}

func TestReceivedJamAudioSkipsLibraryCoverLookup(t *testing.T) {
	a := receivedJamAlbum(filepath.Join(t.TempDir(), "track.mp3"), friendshare.Song{
		Title: "Shared track", Artist: "Artist", Album: "Album",
	})
	p := &player{active: a, popup: true}
	p.updateCover()
	p.handle("C")
	if p.current().source != a || !a.loaded || p.coverPrompt != nil || p.coverLookup.result != nil {
		t.Fatal("received track must have a display album and skip library cover lookup")
	}
}
