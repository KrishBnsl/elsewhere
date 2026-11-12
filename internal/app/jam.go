package app

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"elsewhere/internal/friendshare"
)

type jamPlayback struct {
	id, track, path           string
	queuedKey                 string
	engine                    *engine
	paths                     map[string]string
	checked                   time.Time
	synced                    time.Time
	clockOffset               time.Duration
	starting                  bool
	waitingToPlay             bool
	transferID, transferError string
	transferNext              time.Time
	transferResult            <-chan jamTransfer
	cancelAudio               context.CancelFunc
	received                  map[string]string
}

func (p *player) inJam() bool {
	j := p.social.reply.Jam
	return j != nil && (j.Joined || j.Host.ID == p.social.reply.Me.ID)
}

func (p *player) jamCommand(arg string, selection *track) {
	switch strings.ToLower(arg) {
	case "":
		if p.social.client != nil && p.social.client.token != "" {
			p.social.panel = &friendsPanel{mode: "jam"}
		}
		p.friendsRequest("home", friendshare.Request{})
	case "leave":
		if p.inJam() && p.social.busy() {
			p.social.jam.queuedKey = "leave"
			return
		}
		p.friendsRequest("jam-leave", friendshare.Request{})
	case "add":
		if !p.inJam() {
			p.notice = "Join a jam before adding a song"
			return
		}
		if selection == nil {
			selection = p.localSong()
		}
		if selection == nil || !queueable(*selection) || p.engine == nil {
			p.notice = "Select an analyzed local track to add"
			return
		}
		reply, err := p.engine.request(map[string]any{"op": "recording", "path": selection.path})
		if err != nil || reply.Hash == "" || reply.DurationMS <= 0 {
			p.notice = "Analyze the song with a current Melisma build first"
			return
		}
		p.friendsRequest("jam-add", friendshare.Request{
			Duration: reply.DurationMS,
			Song:     friendshare.Song{Hash: reply.Hash, Title: selection.title, Artist: selection.artist, Album: selection.album},
		})
	default:
		p.notice = "Use /jam, /jam add, or /jam leave"
	}
}

func (p *player) jamTransport(key string) bool {
	if !p.inJam() {
		return false
	}
	f := &p.social
	jam := f.reply.Jam
	if jam.Host.ID != f.reply.Me.ID {
		p.notice = "The jam host controls playback"
		return true
	}
	if len(jam.Queue) == 0 {
		p.notice = "Add a song with /jam add"
		return true
	}
	if f.busy() {
		f.jam.queuedKey = key
		return true
	}
	req := friendshare.Request{Revision: jam.Revision}
	switch key {
	case " ", "play", "pause":
		op := "play"
		if key == "pause" || key == " " && (jam.Playing || f.jam.waitingToPlay) {
			op = "pause"
			f.jam.waitingToPlay = false
		} else if !jam.CanPlay() {
			f.jam.waitingToPlay = true
			p.notice = "Waiting for both listeners to have the track"
			return true
		}
		p.friendsRequest("jam-"+op, req)
	case "n", ">":
		f.jam.waitingToPlay = jam.Playing || f.jam.waitingToPlay
		p.friendsRequest("jam-next", req)
	case "[", "]":
		req.Position = jam.PositionAt(time.Now().Add(f.jam.clockOffset).UnixMilli())
		if key == "[" {
			req.Position -= 10_000
		} else {
			req.Position += 10_000
		}
		p.friendsRequest("jam-seek", req)
	}
	return true
}

func (p *player) tickJam(now time.Time) {
	f := &p.social
	play := &f.jam
	jam := f.reply.Jam
	if !p.inJam() {
		if play.path != "" && p.current() != nil && p.current().path == play.path {
			if play.received[play.track] == play.path {
				stopAudio()
				p.active = nil
			} else {
				pauseAudio()
			}
			p.expected = false
		}
		play.clearAudio()
		*play = jamPlayback{clockOffset: play.clockOffset, synced: play.synced}
		return
	}
	p.cancelTransition()
	if play.id != jam.ID {
		play.id, play.track, play.path = jam.ID, "", ""
		play.engine = nil
		play.checked = time.Time{}
		if p.active != nil && p.active.radio {
			p.stopStation()
		} else if p.active != nil && audioState().playing {
			pauseAudio()
			p.expected = false
		}
	}
	if play.queuedKey != "" && !f.busy() {
		key := play.queuedKey
		play.queuedKey = ""
		if key == "leave" {
			p.jamCommand("leave", nil)
		} else {
			p.jamTransport(key)
		}
	}
	if play.engine != p.engine || !now.Before(play.checked) {
		play.engine = p.engine
		play.paths = make(map[string]string)
		play.checked = now.Add(10 * time.Second)
	}
	ready := make([]string, 0, len(jam.Queue))
	for _, entry := range jam.Queue {
		path, checked := play.paths[entry.Song.Hash]
		if !checked && p.engine != nil {
			reply, err := p.engine.request(map[string]any{"op": "locate", "hash": entry.Song.Hash})
			if err == nil {
				for _, candidate := range reply.Paths {
					if a, _ := p.findTrack(candidate); a != nil {
						path = candidate
						break
					}
				}
			}
			play.paths[entry.Song.Hash] = path
		}
		if path != "" || play.received[entry.ID] != "" {
			ready = append(ready, entry.ID)
		}
	}
	p.tickJamAudio(now, jam, f.reply.Me.ID)
	if !slices.Equal(jam.Ready[f.reply.Me.ID], ready) && !f.busy() {
		p.friendsRequest("jam-ready", friendshare.Request{Ready: ready})
	}
	if len(jam.Queue) == 0 {
		play.waitingToPlay = false
		if play.path != "" {
			if play.received[play.track] == play.path {
				stopAudio()
				p.active = nil
			} else if audioState().playing {
				pauseAudio()
			}
		}
		p.expected = false
		play.path, play.track = "", ""
		return
	}
	entry := jam.Queue[0]
	if jam.Playing && !f.busy() {
		play.waitingToPlay = false
	} else if !jam.Playing && play.waitingToPlay && jam.CanPlay() && !f.busy() {
		play.waitingToPlay = false
		p.jamTransport("play")
	}
	serverNow := now.Add(play.clockOffset).UnixMilli()
	position := jam.PositionAt(serverNow)
	wantPlay := jam.Playing && jam.CanPlay() && serverNow >= jam.StartedAt &&
		position < entry.Duration && now.Sub(play.synced) < 6*time.Second
	path := play.paths[entry.Song.Hash]
	if path == "" {
		path = play.received[entry.ID]
		wantPlay = wantPlay && path != ""
	}
	if !wantPlay {
		if p.active != nil && audioState().playing {
			pauseAudio()
		}
		p.expected = false
		if play.track == entry.ID && play.path != "" && p.current() != nil && p.current().path == play.path && !p.finished {
			state := audioState()
			at := min(float64(position)/1000, max(0, state.duration-0.01))
			if state.duration > 0 && (state.elapsed-at > 0.4 || at-state.elapsed > 0.4) {
				seekAudio(at)
			}
		}
		if jam.Playing && position >= entry.Duration && now.Sub(play.synced) < 6*time.Second &&
			jam.Host.ID == f.reply.Me.ID && !f.busy() {
			play.waitingToPlay = true
			p.friendsRequest("jam-next", friendshare.Request{Revision: jam.Revision})
		}
		return
	}
	at := float64(position) / 1000
	if play.track != entry.ID || p.current() == nil || p.current().path != path || p.restored {
		a, i := p.findTrack(path)
		if path == play.received[entry.ID] {
			a, i = receivedJamAlbum(path, entry.Song), 0
		}
		p.leaving("choose")
		play.starting = true
		started := p.start(a, i)
		play.starting = false
		if !started {
			play.paths[entry.Song.Hash] = ""
			if play.received[entry.ID] != "" {
				_ = os.Remove(path)
				delete(play.received, entry.ID)
				play.transferError = "Could not play received audio"
				play.transferNext = now.Add(10 * time.Second)
			}
			return
		}
		p.stopRadio()
		p.clearLiveStation()
		p.leaveQueue()
		p.melisma, p.popup = false, true
		p.tripDestination, p.tripDetour = "", false
		play.path, play.track = path, entry.ID
		seekAudio(at)
	} else {
		state := audioState()
		if at-state.elapsed > 0.8 || state.elapsed-at > 0.8 {
			seekAudio(min(at, max(0, state.duration-0.01)))
		}
		if !state.playing {
			if !resumeAudio() {
				// Some backends discard a source at EOF. Reopen it if the host seeks back.
				play.track = ""
				return
			}
		}
	}
	p.expected, p.finished = true, false
}

func (p *player) jamPanel() ([]string, string) {
	f := &p.social
	jam := f.reply.Jam
	if jam == nil {
		var names []string
		for _, friend := range f.reply.Friends {
			names = append(names, friend.Name)
		}
		return names, " ↑↓ Select · Enter Invite to jam · Esc Close"
	}
	if !jam.Joined && jam.Guest.ID == f.reply.Me.ID {
		return []string{"Invite from " + jam.Host.Name}, " Enter Join · L Decline · Esc Close"
	}
	state := "Paused"
	if !jam.Joined {
		state = "Waiting for " + jam.Guest.Name
	} else if jam.Playing {
		state = "Playing"
	}
	items := []string{fmt.Sprintf("%s · %s + %s", state, jam.Host.Name, jam.Guest.Name)}
	for i, entry := range jam.Queue {
		mine := slices.Contains(jam.Ready[f.reply.Me.ID], entry.ID)
		other := slices.Contains(jam.Ready[jam.Host.ID], entry.ID) && slices.Contains(jam.Ready[jam.Guest.ID], entry.ID)
		label := "missing here"
		if other {
			label = "both ready"
		} else if mine {
			label = "waiting for friend"
		}
		if i == 0 && jam.Joined && !other {
			switch {
			case entry.AddedBy == "":
				label = "re-add to share audio"
			case f.jam.transferError != "":
				label = f.jam.transferError
			case !mine:
				label = "waiting for audio"
			case f.jam.transferResult != nil:
				label = "sending audio"
			}
		}
		items = append(items, entry.Song.Title+" · "+label)
	}
	return items, " Enter Play/Pause (host) · /jam add · L Leave · Esc Close"
}
