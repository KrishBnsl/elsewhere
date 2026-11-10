package main

import (
	"os"
	"strings"

	"elsewhere/internal/friendshare"
)

type friendForm struct {
	code, name          string
	nameFocused, rename bool
}

func (p *player) friendDisplayName() string {
	if name := p.social.reply.Me.Name; name != "" {
		return name
	}
	if f := p.social.client; f != nil && f.displayName != "" {
		return f.displayName
	}
	return strings.TrimSpace(os.Getenv("ELSEWHERE_FRIENDS_NAME"))
}

func (f *friendsClient) rememberName(name string) error {
	if name == "" || name == f.displayName {
		return nil
	}
	if err := writeLocalFile(f.credentialPath+".name", []byte(name)); err != nil {
		return err
	}
	f.displayName = name
	return nil
}

func (p *player) handleFriendForm(key string) {
	panel := p.social.panel
	form := panel.form
	switch key {
	case "\x1b":
		if form.rename {
			panel.form = nil
		} else {
			p.social.panel = nil
		}
		p.notice = ""
	case "\t", "\x1b[Z", "\x1b[A", "\x1b[B":
		if !form.rename {
			form.nameFocused = !form.nameFocused
		}
	case "\r", "\n":
		name, code := strings.TrimSpace(form.name), strings.TrimSpace(form.code)
		switch {
		case !form.rename && code == "":
			form.nameFocused = false
			p.notice = "Paste the invite code"
		case name == "":
			form.nameFocused = true
			p.notice = "Enter your display name"
		case form.rename:
			p.friendsRequest("name", friendshare.Request{Name: name})
		default:
			p.friendsRequest("accept", friendshare.Request{Code: code, Name: name})
		}
	default:
		if form.nameFocused {
			editText(&form.name, key, 64)
		} else {
			editText(&form.code, key, 128)
		}
	}
}
