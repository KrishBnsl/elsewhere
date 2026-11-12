package app

import (
	"crypto/rand"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"elsewhere/internal/friendshare"
)

func TestFriendsDisconnectFromPicker(t *testing.T) {
	setup := rand.Text()
	handler, err := friendshare.Open(filepath.Join(t.TempDir(), "friends.json"), setup)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	alice := friendsClient{base: server.URL, token: rand.Text()}
	bob := friendsClient{base: server.URL, token: rand.Text()}
	request := func(client friendsClient, op string, req friendshare.Request) friendshare.Reply {
		t.Helper()
		reply, err := client.call(op, req)
		if err != nil {
			t.Fatal(err)
		}
		return reply
	}
	request(alice, "connect", friendshare.Request{Name: "Alice", Code: setup})
	invite := request(alice, "invite", friendshare.Request{}).Code
	request(bob, "accept", friendshare.Request{Name: "Bob", Code: invite})
	p := player{social: friendsState{
		client: &alice, reply: request(alice, "home", friendshare.Request{}),
		panel: &friendsPanel{mode: "friends"}, lastReady: time.Now(),
	}}

	p.handleFriends("D")
	p.handleFriends("\x1b")
	if p.social.panel.disconnect != nil || p.social.pending != "" {
		t.Fatal("cancel should dismiss confirmation without sending a request")
	}
	p.handleFriends("d")
	rows := make([]string, 20)
	p.overlayFriends(rows, 80)
	if !strings.Contains(strings.Join(rows, "\n"), "Disconnect from Bob?") || p.social.pending != "" {
		t.Fatal("lowercase d should show the selected friend before sending a request")
	}
	p.handleFriends("\r")
	select {
	case result := <-p.social.result:
		if result.err != nil {
			t.Fatal(result.err)
		}
		reply := make(chan friendsResult, 1)
		reply <- result
		p.social.result = reply
		p.tickFriends(time.Now())
	case <-time.After(2 * time.Second):
		t.Fatal("disconnect did not complete")
	}
	if p.notice != "Friend disconnected" || len(p.social.reply.Friends) != 0 || p.social.panel.disconnect != nil {
		t.Fatal("the Friends panel did not reflect the disconnect")
	}
	if len(request(bob, "home", friendshare.Request{}).Friends) != 0 {
		t.Fatal("the friend still sees the connection")
	}
}

func TestAcceptFormRemembersAndRenamesDisplayName(t *testing.T) {
	setup := rand.Text()
	handler, err := friendshare.Open(filepath.Join(t.TempDir(), "friends.json"), setup)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	t.Setenv("ELSEWHERE_FRIENDS_URL", server.URL)
	t.Setenv("ELSEWHERE_FRIENDS_NAME", "")
	config := t.TempDir()
	client, err := newFriendsClient(config)
	if err != nil {
		t.Fatal(err)
	}
	alice := friendsClient{base: server.URL, token: rand.Text()}
	if _, err := alice.call("connect", friendshare.Request{Name: "Alice", Code: setup}); err != nil {
		t.Fatal(err)
	}
	invite, err := alice.call("invite", friendshare.Request{})
	if err != nil {
		t.Fatal(err)
	}
	p := player{social: friendsState{client: client, lastReady: time.Now()}}
	finish := func() {
		t.Helper()
		select {
		case result := <-p.social.result:
			if result.err != nil {
				t.Fatal(result.err)
			}
			reply := make(chan friendsResult, 1)
			reply <- result
			p.social.result = reply
			p.tickFriends(time.Now())
		case <-time.After(2 * time.Second):
			t.Fatal("friends request did not complete")
		}
	}

	p.friendsCommand("accept", "", nil)
	rows := make([]string, 20)
	p.overlayFriends(rows, 80)
	if rendered := strings.Join(rows, "\n"); !strings.Contains(rendered, "Invite code") || !strings.Contains(rendered, "Display name") {
		t.Fatal("/accept should open an invite form")
	}
	p.handleFriends(invite.Code)
	p.handleFriends("\r")
	if !p.social.panel.form.nameFocused || p.social.pending != "" {
		t.Fatal("an empty name should focus the name field instead of sending")
	}
	p.handleFriends("Bob")
	p.handleFriends("\r")
	finish()
	if p.social.reply.Me.Name != "Bob" || p.social.panel.mode != "friends" {
		t.Fatal("submitting the form did not connect Bob")
	}

	p.handleFriends("R")
	for range "Bob" {
		p.handleFriends("\x7f")
	}
	p.handleFriends("Bobby")
	p.handleFriends("\r")
	finish()
	fresh, err := newFriendsClient(config)
	if err != nil {
		t.Fatal(err)
	}
	other, err := alice.call("home", friendshare.Request{})
	if err != nil || len(other.Friends) != 1 || other.Friends[0].Name != "Bobby" {
		t.Fatalf("the friend did not see the new display name: %+v, %v", other.Friends, err)
	}
	p = player{social: friendsState{client: fresh}}
	p.friendsCommand("accept", "", nil)
	if p.social.panel.form.name != "Bobby" {
		t.Fatal("the next invite did not prefill the saved display name")
	}
}
