package friendshare

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestInviteSongAndDisconnect(t *testing.T) {
	file, setup := filepath.Join(t.TempDir(), "friends.json"), rand.Text()
	handler, err := Open(file, setup)
	if err != nil {
		t.Fatal(err)
	}
	call := func(want int, token, op string, body Request) Reply {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/v1/"+op, bytes.NewReader(data))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: %d, %s", op, w.Code, w.Body.String())
		}
		var reply Reply
		if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		return reply
	}
	alice, bob, stranger := rand.Text(), rand.Text(), rand.Text()
	a := call(200, alice, "connect", Request{Name: "Alice", Code: setup}).Me
	call(401, stranger, "home", Request{})
	call(403, stranger, "connect", Request{Name: "Uninvited"})
	invite := call(200, alice, "invite", Request{}).Code
	b := call(200, bob, "accept", Request{Name: "Bob", Code: invite})
	if len(b.Friends) != 1 || b.Friends[0] != a {
		t.Fatalf("connections: %+v", b.Friends)
	}
	call(400, stranger, "accept", Request{Name: "Eve", Code: invite})
	song := Song{Hash: "0123456789abcdef", Title: "Finale"}
	call(200, alice, "send", Request{Friend: b.Me.ID, Song: song})

	handler, err = Open(file, setup)
	if err != nil {
		t.Fatal(err)
	}
	cards := call(200, bob, "home", Request{}).Cards
	if len(cards) != 1 || cards[0].From != a || cards[0].Song != song {
		t.Fatalf("persisted inbox: %+v", cards)
	}
	if cards := call(200, alice, "home", Request{}).Cards; len(cards) != 0 {
		t.Fatalf("sender received someone else's inbox: %+v", cards)
	}
	taste := &Taste{Version: 1, Regions: []Region{{Weight: 1, Sound: []float32{1, 2, 3}}}}
	call(400, alice, "taste-update", Request{Taste: taste})
	call(200, alice, "taste-on", Request{})
	call(200, alice, "taste-update", Request{Taste: taste})
	if shared := call(200, bob, "home", Request{}).Tastes; len(shared) != 1 || shared[0].ID != a.ID {
		t.Fatalf("friend taste visibility: %+v", shared)
	}
	call(200, alice, "taste-off", Request{})
	if shared := call(200, bob, "home", Request{}).Tastes; len(shared) != 0 {
		t.Fatalf("revoked taste visible: %+v", shared)
	}

	call(200, alice, "jam-create", Request{Friend: b.Me.ID})
	call(200, alice, "jam-add", Request{Song: song, Duration: 30_000})
	jam := call(200, bob, "jam-join", Request{}).Jam
	id := jam.Queue[0].ID
	call(409, alice, "jam-play", Request{Revision: jam.Revision})
	call(200, alice, "jam-ready", Request{Ready: []string{id}})
	jam = call(200, bob, "jam-ready", Request{Ready: []string{id}}).Jam
	call(409, bob, "jam-play", Request{Revision: jam.Revision})
	jam = call(200, alice, "jam-play", Request{Revision: jam.Revision}).Jam
	if !jam.Playing || jam.PositionAt(jam.StartedAt-1) != 0 || jam.PositionAt(jam.StartedAt+4000) != 4000 {
		t.Fatalf("invalid scheduled playback: %+v", jam)
	}
	call(409, alice, "jam-seek", Request{Revision: jam.Revision - 1, Position: 10_000})
	jam = call(200, alice, "jam-seek", Request{Revision: jam.Revision, Position: 10_000}).Jam
	jam = call(200, alice, "jam-pause", Request{Revision: jam.Revision}).Jam
	if jam.Playing || jam.Position != 10_000 || jam.PositionAt(jam.StartedAt+100_000) != 10_000 {
		t.Fatalf("pause did not freeze position: %+v", jam)
	}
	call(200, bob, "jam-leave", Request{})
	if jam := call(200, alice, "home", Request{}).Jam; jam != nil {
		t.Fatalf("room still exists: %+v", jam)
	}

	call(200, bob, "disconnect", Request{Friend: a.ID})
	call(403, alice, "send", Request{Friend: b.Me.ID, Song: song})
	if reply := call(200, bob, "home", Request{}); len(reply.Cards) != 0 || len(reply.Friends) != 0 {
		t.Fatalf("disconnect left data visible: %+v", reply)
	}
}
