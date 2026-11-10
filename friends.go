package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"elsewhere/internal/friendshare"
)

type friendsClient struct {
	base, credentialPath, token, displayName string
}

const defaultFriendsURL = "https://elsewhere-friends.onrender.com"
const friendsDisabled = "Friends is disabled · unset ELSEWHERE_FRIENDS_URL to use the hosted service"

func newFriendsClient(config string) (*friendsClient, error) {
	raw := strings.TrimSpace(os.Getenv("ELSEWHERE_FRIENDS_URL"))
	switch raw {
	case "":
		raw = defaultFriendsURL
	case "off":
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("ELSEWHERE_FRIENDS_URL must be a service URL")
	}
	ip := net.ParseIP(u.Hostname())
	loopback := u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, errors.New("the friends service requires HTTPS outside localhost")
	}
	base := strings.TrimRight(u.String(), "/")
	f := &friendsClient{
		base: base, credentialPath: filepath.Join(config, fmt.Sprintf("friends-%x.token", sha256.Sum256([]byte(base)))),
	}
	data, err := os.ReadFile(f.credentialPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	f.token = strings.TrimSpace(string(data))
	if name, err := os.ReadFile(f.credentialPath + ".name"); err == nil {
		f.displayName = strings.TrimSpace(string(name))
	}
	return f, nil
}

func (f *friendsClient) prepare() error {
	if f.token != "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(f.credentialPath), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(f.credentialPath+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := lockFile(lock); err != nil {
		return err
	}
	if data, err := os.ReadFile(f.credentialPath); err == nil {
		if token := strings.TrimSpace(string(data)); token != "" {
			f.token = token
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	token := rand.Text()
	if err := writeLocalFile(f.credentialPath, []byte(token)); err != nil {
		return err
	}
	f.token = token
	return nil
}

var friendsHTTP = &http.Client{
	Timeout:       6 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

const wakingFriends = "Waking friends service…"

func (f friendsClient) waitReady(ctx context.Context, delay time.Duration) error {
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.base+"/readyz", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		res, err := friendsHTTP.Do(req)
		if err == nil {
			status := res.StatusCode
			contentType := res.Header.Get("Content-Type")
			var oldServer friendshare.Reply
			if status == http.StatusMethodNotAllowed && strings.HasPrefix(contentType, "application/json") {
				_ = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&oldServer)
			}
			res.Body.Close()
			if status == http.StatusNoContent || oldServer.Error == "use POST" {
				return nil
			}
			switch {
			case status == http.StatusBadGateway || status == http.StatusServiceUnavailable ||
				status == http.StatusGatewayTimeout || status == http.StatusTooManyRequests:
			case status == http.StatusOK && strings.HasPrefix(contentType, "text/html"):
				// The free host can serve a warm-up page before the Go process starts.
			default:
				return fmt.Errorf("friends service readiness returned %s", res.Status)
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			if errors.Is(ctx.Err(), context.Canceled) {
				return ctx.Err()
			}
			return errors.New("friends service did not wake up in time · try again")
		case <-timer.C:
		}
	}
}

func (f friendsClient) call(op string, request friendshare.Request) (friendshare.Reply, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return friendshare.Reply{}, err
	}
	if op == "poll" {
		op = "home"
	}
	req, err := http.NewRequest(http.MethodPost, f.base+"/v1/"+op, bytes.NewReader(data))
	if err != nil {
		return friendshare.Reply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.token)
	res, err := friendsHTTP.Do(req)
	if err != nil {
		return friendshare.Reply{}, fmt.Errorf("friends service: %w", err)
	}
	defer res.Body.Close()
	var reply friendshare.Reply
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&reply); err != nil {
		return reply, fmt.Errorf("friends service returned %s", res.Status)
	}
	if res.StatusCode != http.StatusOK {
		if reply.Error != "" {
			return reply, errors.New(reply.Error)
		}
		return reply, fmt.Errorf("friends service returned %s", res.Status)
	}
	return reply, nil
}

type friendsResult struct {
	op       string
	reply    friendshare.Reply
	err      error
	midpoint time.Time
	ready    bool
}

type friendsPanel struct {
	mode       string
	index      int
	song       *track
	invite     string
	disconnect *friendshare.Person
	form       *friendForm
}

type friendsState struct {
	client     *friendsClient
	err        error
	panel      *friendsPanel
	result     <-chan friendsResult
	pending    string
	lastReady  time.Time
	cancelWake context.CancelFunc
	reply      friendshare.Reply
	next       time.Time
	jam        jamPlayback
	taste      tasteSync
}

// A fresh user action can supersede background work. Its response supplies
// the next snapshot, so an older refresh cannot overwrite newer state.
func (f *friendsState) busy() bool {
	return f.pending != "" && f.pending != "poll" && f.pending != "home" && f.pending != "taste-update"
}

func (p *player) friendsRequest(op string, request friendshare.Request) {
	f := &p.social
	switch {
	case f.err != nil:
		p.notice = f.err.Error()
		return
	case f.client == nil:
		p.notice = friendsDisabled
		return
	case f.busy():
		p.notice = "Friends request in progress"
		return
	case op == "accept" || op == "connect":
		if err := f.client.prepare(); err != nil {
			p.notice = "Save device credential: " + err.Error()
			return
		}
	case f.client.token == "":
		p.notice = "Use /accept CODE to join, or /connect NAME to set up the first device"
		return
	}
	if f.cancelWake != nil {
		f.cancelWake()
		f.cancelWake = nil
	}
	wake := time.Since(f.lastReady) >= 12*time.Minute
	var wakeCtx context.Context
	var cancel context.CancelFunc
	if wake {
		wakeCtx, cancel = context.WithTimeout(context.Background(), 85*time.Second)
		f.cancelWake = cancel
		if op != "poll" || f.panel != nil {
			p.notice = wakingFriends
		}
	}
	result := make(chan friendsResult, 1)
	f.result, f.pending = result, op
	client := *f.client
	go func() {
		if wake {
			defer cancel()
			if err := client.waitReady(wakeCtx, 2*time.Second); err != nil {
				result <- friendsResult{op: op, err: err}
				return
			}
			if wakeCtx.Err() != nil {
				result <- friendsResult{op: op, err: wakeCtx.Err()}
				return
			}
		}
		// Only the readiness probe retries. Sending a song or creating an invite is a single request.
		started := time.Now()
		reply, err := client.call(op, request)
		result <- friendsResult{op: op, reply: reply, err: err, midpoint: started.Add(time.Since(started) / 2), ready: wake || err == nil}
	}()
}

func (p *player) tickFriends(now time.Time) {
	f := &p.social
	select {
	case result := <-f.result:
		f.result, f.pending, f.cancelWake = nil, "", nil
		if result.ready {
			f.lastReady = now
		}
		if p.notice == wakingFriends {
			p.notice = ""
		}
		if result.err != nil {
			if result.op != "poll" || f.panel != nil {
				p.notice = "Friends · " + result.err.Error()
			}
			return
		}
		if result.op == "poll" && f.reply.Me.ID != "" && len(result.reply.Cards) > 0 &&
			!slices.ContainsFunc(f.reply.Cards, func(c friendshare.Card) bool { return c.ID == result.reply.Cards[len(result.reply.Cards)-1].ID }) {
			p.notice = "New song from a friend · /inbox"
		}
		if f.reply.Jam == nil && result.reply.Jam != nil && result.reply.Jam.Guest.ID == result.reply.Me.ID {
			p.notice = "Jam invite from " + result.reply.Jam.Host.Name + " · /jam"
		}
		f.reply = result.reply
		f.jam.clockOffset = time.UnixMilli(result.reply.Now).Sub(result.midpoint)
		f.jam.synced = now
		f.next = now.Add(f.pollInterval())
		switch result.op {
		case "invite":
			f.panel = &friendsPanel{mode: "friends", invite: result.reply.Code}
			p.notice = "Invite expires in 24 hours"
		case "accept", "connect", "name":
			f.panel = &friendsPanel{mode: "friends"}
			p.notice = "Connected as " + f.reply.Me.Name
			if result.op == "name" {
				p.notice = "Display name saved"
			}
			if err := f.client.rememberName(f.reply.Me.Name); err != nil {
				p.notice += " · could not save local name"
			}
		case "send":
			f.panel = nil
			p.notice = "Song sent"
		case "jam-create":
			p.notice = "Jam invite sent · /jam add to queue a track"
		case "jam-join":
			p.notice = "Joined the jam"
		case "jam-play":
			f.panel = nil
		case "jam-add":
			p.notice = "Added to the jam queue"
		case "jam-leave":
			p.notice = "Left the jam"
			f.panel = nil
		case "taste-on":
			f.taste.next = time.Time{}
			p.notice = "Taste sharing on"
		case "taste-off":
			p.notice = "Taste sharing off"
		case "disconnect":
			if f.panel != nil && f.panel.mode == "friends" {
				f.panel.disconnect = nil
				f.panel.index = min(f.panel.index, max(0, len(f.reply.Friends)-1))
			}
			p.notice = "Friend disconnected"
		}
	default:
		if f.result == nil && !f.next.IsZero() && !now.Before(f.next) {
			f.next = now.Add(f.pollInterval())
			p.friendsRequest("poll", friendshare.Request{})
		}
	}
}

func (f *friendsState) pollInterval() time.Duration {
	if f.reply.Jam != nil {
		return 700 * time.Millisecond
	}
	return 20 * time.Second
}

func (p *player) friendsCommand(name, arg string, selection *track) {
	switch name {
	case "jam":
		p.jamCommand(arg, selection)
	case "taste-sync":
		p.setTasteSharing(arg)
	case "connect":
		if arg == "" || os.Getenv("ELSEWHERE_FRIENDS_SETUP_KEY") == "" {
			p.notice = "Use /connect NAME with ELSEWHERE_FRIENDS_SETUP_KEY set"
			return
		}
		p.friendsRequest(name, friendshare.Request{Name: arg, Code: os.Getenv("ELSEWHERE_FRIENDS_SETUP_KEY")})
	case "accept":
		if p.social.err != nil {
			p.notice = p.social.err.Error()
			return
		}
		if p.social.client == nil {
			p.notice = friendsDisabled
			return
		}
		code, name, _ := strings.Cut(arg, " ")
		name = strings.TrimSpace(name)
		if name == "" {
			name = p.friendDisplayName()
		}
		if code != "" && name != "" {
			p.friendsRequest("accept", friendshare.Request{Code: code, Name: name})
		} else {
			p.social.panel = &friendsPanel{form: &friendForm{code: code, name: name, nameFocused: code != ""}}
			p.notice = ""
		}
	case "invite":
		p.friendsRequest(name, friendshare.Request{})
	case "friends", "send", "inbox":
		var song *track
		if name == "send" {
			song = p.localSong()
			if song == nil {
				song = selection
			}
			if song == nil || !queueable(*song) {
				p.notice = "Play or select a local song to send"
				return
			}
		}
		if p.social.client != nil && p.social.client.token != "" {
			p.social.panel = &friendsPanel{mode: name, song: song}
		}
		p.friendsRequest("home", friendshare.Request{})
	}
}

func (p *player) handleFriends(key string) {
	f := &p.social
	panel := f.panel
	if panel.form != nil {
		p.handleFriendForm(key)
		return
	}
	if target := panel.disconnect; target != nil {
		switch key {
		case "\x1b", "n", "N":
			panel.disconnect = nil
		case "\r", "\n", "y", "Y":
			if !slices.ContainsFunc(f.reply.Friends, func(friend friendshare.Person) bool { return friend.ID == target.ID }) {
				panel.disconnect = nil
				p.notice = "Friend is already disconnected"
				return
			}
			p.friendsRequest("disconnect", friendshare.Request{Friend: target.ID})
		}
		return
	}
	count := len(f.reply.Friends)
	if panel.mode == "jam" {
		if key == "L" && f.reply.Jam != nil {
			p.jamCommand("leave", nil)
			return
		}
		if f.reply.Jam != nil {
			items, _ := p.jamPanel()
			count = len(items)
		}
	}
	if panel.mode == "inbox" {
		count = len(f.reply.Cards)
	}
	panel.index = min(panel.index, max(0, count-1))
	switch key {
	case "\x1b":
		f.panel = nil
	case "/":
		f.panel = nil
		p.beginSearch()
	case "\x1b[A", "k":
		panel.index = max(0, panel.index-1)
	case "\x1b[B", "j":
		panel.index = min(max(0, count-1), panel.index+1)
	case "i", "I":
		if panel.mode == "friends" {
			p.friendsRequest("invite", friendshare.Request{})
		}
	case "r", "R":
		if panel.mode == "friends" && f.reply.Me.ID != "" {
			panel.form = &friendForm{name: p.friendDisplayName(), nameFocused: true, rename: true}
			p.notice = ""
		}
	case "d", "D":
		if panel.mode == "friends" && count > 0 {
			target := f.reply.Friends[panel.index]
			panel.disconnect = &target
		}
	case "\r", "\n":
		if count == 0 {
			return
		}
		switch panel.mode {
		case "jam":
			jam := f.reply.Jam
			switch {
			case jam == nil:
				p.friendsRequest("jam-create", friendshare.Request{Friend: f.reply.Friends[panel.index].ID})
			case !jam.Joined && jam.Guest.ID == f.reply.Me.ID:
				p.friendsRequest("jam-join", friendshare.Request{})
			default:
				p.jamTransport(" ")
			}
		case "send":
			p.sendFriendSong(*panel.song, f.reply.Friends[panel.index])
		case "inbox":
			p.playFriendSong(f.reply.Cards[count-1-panel.index])
		}
	}
}

func (p *player) sendFriendSong(t track, to friendshare.Person) {
	if p.engine == nil {
		p.notice = "Start Melisma and analyze the song before sending"
		return
	}
	reply, err := p.engine.request(map[string]any{"op": "recording", "path": t.path})
	if err != nil || reply.Hash == "" {
		p.notice = "Could not identify this recording · run /analyze with a current Melisma build"
		return
	}
	p.friendsRequest("send", friendshare.Request{
		Friend: to.ID,
		Song:   friendshare.Song{Hash: reply.Hash, Title: t.title, Artist: t.artist, Album: t.album},
	})
}

func (p *player) playFriendSong(card friendshare.Card) {
	if p.engine == nil {
		p.notice = "Start Melisma and analyze your library to match this song"
		return
	}
	reply, err := p.engine.request(map[string]any{"op": "locate", "hash": card.Song.Hash})
	if err != nil {
		p.notice = "Melisma · " + err.Error()
		return
	}
	for _, path := range reply.Paths {
		if a, i := p.findTrack(path); a != nil {
			p.leaving("choose")
			if !p.start(a, i) {
				continue
			}
			p.stopRadio()
			p.clearLiveStation()
			p.leaveQueue()
			p.social.panel = nil
			p.melisma, p.popup = false, true
			p.tripDestination, p.tripDetour = "", false
			p.why = []string{"Sent by " + card.From.Name}
			return
		}
	}
	p.notice = "No matching recording in this library · analyze a local copy to play it"
}

func (p *player) overlayFriends(rows []string, width int) {
	f, view := &p.social, p.social.panel
	columns := min(width-4, 80)
	heading, footer := " FRIENDS ", " I Invite · R Rename · d Disconnect · Esc Close"
	var items []string
	switch view.mode {
	case "jam":
		heading = " JAM ROOM "
		items, footer = p.jamPanel()
	case "inbox":
		heading, footer = " SONG INBOX ", " ↑↓ Select · Enter Play local match · Esc Close"
		for i := len(f.reply.Cards) - 1; i >= 0; i-- {
			card := f.reply.Cards[i]
			items = append(items, card.From.Name+" → "+card.Song.Title+" / "+card.Song.Artist)
		}
	case "send":
		heading, footer = " SEND SONG ", " ↑↓ Select · Enter Send · Esc Close"
	}
	if form := view.form; form != nil {
		heading, footer = " ACCEPT INVITE ", " Tab Field · Enter Accept · Esc Cancel"
		if form.rename {
			heading, footer = " FRIENDS DISPLAY NAME ", " Enter Save · Esc Cancel"
		}
	}
	if view.form == nil && view.mode != "inbox" && view.mode != "jam" {
		for _, person := range f.reply.Friends {
			items = append(items, person.Name)
		}
	}
	lines := []string{boxTop(heading, columns)}
	add := func(s, style string) {
		lines = append(lines, boxSide(style+cell(s, columns-2)+panel+amber))
	}
	if view.invite != "" && view.form == nil {
		add(" Send this code to your friend. They open /accept.", amber)
		add(" "+view.invite, selected)
	}
	if view.song != nil {
		add(" "+view.song.title+" / "+view.song.artist, amber)
	}
	if form := view.form; form != nil {
		code, name := form.code, form.name
		codeStyle, nameStyle := selected, dim
		if form.nameFocused {
			codeStyle, nameStyle = dim, selected
			name += "_"
		} else {
			code += "_"
		}
		if !form.rename {
			add(" Invite code    "+code, codeStyle)
		}
		add(" Display name   "+name, nameStyle)
		if f.pending == "accept" || f.pending == "name" {
			add(" Waiting for friends service…", dim)
		}
	} else {
		if len(items) == 0 {
			add(" Nothing here yet", dim)
		}
		n := min(8, len(rows)-8)
		start := max(0, min(view.index-n/2, len(items)-n))
		for i := start; i < min(len(items), start+n); i++ {
			if i == view.index {
				add(" › "+items[i], selected)
			} else {
				add("   "+items[i], dim)
			}
		}
	}
	if p.notice != "" {
		add(" "+p.notice, dim)
	}
	if target := view.disconnect; target != nil {
		add(" Disconnect from "+target.Name+"?", amber)
		footer = " Enter Disconnect · Esc Cancel"
	}
	add(footer, dim)
	lines = append(lines, boxBottom(columns))
	cursor := fmt.Sprintf("\x1b[%dG", (width-columns)/2+1)
	for i, line := range lines {
		y := (len(rows)-len(lines))/2 + i
		if y >= 0 && y < len(rows) {
			rows[y] += cursor + panel + amber + line + reset
		}
	}
}
