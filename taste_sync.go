package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"time"

	"elsewhere/internal/friendshare"
)

type tasteSync struct {
	engine   *engine
	seen     []friendshare.SharedTaste
	next     time.Time
	listener string
}

func (p *player) tickTaste(now time.Time) {
	f := &p.social
	if p.engine == nil || f.reply.Me.ID == "" {
		return
	}
	if f.taste.engine != p.engine || !reflect.DeepEqual(f.taste.seen, f.reply.Tastes) {
		_, _ = p.engine.request(map[string]any{"op": "taste-import", "tastes": f.reply.Tastes})
		f.taste.engine, f.taste.seen = p.engine, f.reply.Tastes
	}
	if !f.reply.TasteEnabled || f.taste.listener != p.engine.user || len(f.reply.Friends) == 0 || now.Before(f.taste.next) || f.pending != "" {
		return
	}
	f.taste.next = now.Add(5 * time.Minute)
	reply, err := p.engine.request(map[string]any{"op": "taste-export"})
	if err != nil {
		return
	}
	var taste friendshare.Taste
	if json.Unmarshal(reply.Taste, &taste) == nil && taste.Version != 0 {
		p.friendsRequest("taste-update", friendshare.Request{Taste: &taste})
	}
}

func (p *player) setTasteSharing(value string) {
	f := &p.social
	if f.client == nil {
		p.notice = friendsDisabled
		return
	}
	path := f.client.credentialPath + ".taste-user"
	switch strings.ToLower(value) {
	case "on":
		if p.engine == nil || p.engine.user == "" || p.engine.user == "guest" {
			p.notice = "Select a Melisma listener before sharing taste"
			return
		}
		if err := writeLocalFile(path, []byte(p.engine.user)); err != nil {
			p.notice = "Could not save taste preference: " + err.Error()
			return
		}
		f.taste.listener = p.engine.user
		p.friendsRequest("taste-on", friendshare.Request{})
	case "off":
		f.taste.listener = ""
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			p.notice = "Could not clear local taste preference: " + err.Error()
			return
		}
		p.friendsRequest("taste-off", friendshare.Request{})
	default:
		p.notice = "Use /taste-sync on or /taste-sync off"
	}
}
