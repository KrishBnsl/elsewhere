package friendshare

import (
	"crypto/rand"
	"errors"
	"maps"
	"slices"
)

func jamFor(db database, person string) (string, Jam) {
	for id, jam := range db.Jams {
		if jam.Host.ID == person || jam.Guest.ID == person {
			return id, jam
		}
	}
	return "", Jam{}
}

func changeJam(db *database, me account, op string, req Request, now int64) error {
	id, jam := jamFor(*db, me.ID)
	if op == "create" {
		peer, key := findFriend(*db, me, req.Friend)
		if key == "" {
			return errors.New("choose a connected friend")
		}
		other, _ := jamFor(*db, peer.ID)
		if id != "" || other != "" {
			return errors.New("one of you is already in a jam")
		}
		id = rand.Text()
		db.Jams[id] = Jam{ID: id, Host: me.Person, Guest: peer.Person, Revision: 1, Ready: map[string][]string{}}
		return nil
	}
	if id == "" {
		return errors.New("no jam to change")
	}
	if op == "leave" {
		delete(db.Jams, id)
		return nil
	}
	host := jam.Host.ID == me.ID
	switch op {
	case "join":
		if host {
			return errors.New("the guest must accept")
		}
		if jam.Joined {
			return nil
		}
		jam.Joined = true
	case "add":
		if !host && !jam.Joined {
			return errors.New("join the jam first")
		}
		if !validSong(req.Song) || req.Duration < 1 || req.Duration > 24*60*60*1000 || len(jam.Queue) >= 32 {
			return errors.New("invalid song or jam queue full")
		}
		jam.Queue = append(slices.Clone(jam.Queue), JamTrack{rand.Text(), req.Song, req.Duration})
	case "ready":
		if !host && !jam.Joined {
			return errors.New("join the jam first")
		}
		var ready []string
		for _, track := range jam.Queue {
			if slices.Contains(req.Ready, track.ID) {
				ready = append(ready, track.ID)
			}
		}
		if slices.Equal(jam.Ready[me.ID], ready) {
			return nil
		}
		jam.Ready = maps.Clone(jam.Ready)
		jam.Ready[me.ID] = ready
		if jam.Playing && !jam.CanPlay() {
			jam.Position, jam.Playing = jam.PositionAt(now), false
		}
	case "play", "pause", "seek", "next":
		if !host || req.Revision != jam.Revision || len(jam.Queue) == 0 {
			return errors.New("only the host can control the current jam track")
		}
		switch op {
		case "play":
			if !jam.CanPlay() {
				return errors.New("both listeners need a local copy before playing")
			}
			if jam.Playing {
				return nil
			}
			if jam.Position >= jam.Queue[0].Duration {
				jam.Position = 0
			}
			jam.StartedAt, jam.Playing = now+1600, true
		case "pause":
			jam.Position, jam.Playing = jam.PositionAt(now), false
		case "seek":
			jam.Position = max(0, min(req.Position, jam.Queue[0].Duration))
			jam.StartedAt = now + 1600
		case "next":
			previous := jam.Queue[0].ID
			jam.Queue = jam.Queue[1:]
			jam.Ready = maps.Clone(jam.Ready)
			for person, ready := range jam.Ready {
				jam.Ready[person] = slices.DeleteFunc(slices.Clone(ready), func(id string) bool { return id == previous })
			}
			jam.Position, jam.StartedAt = 0, now+1600
			jam.Playing = jam.Playing && jam.CanPlay()
		}
	default:
		return errors.New("unknown jam action")
	}
	jam.Revision++
	db.Jams[id] = jam
	return nil
}

func (j Jam) CanPlay() bool {
	if !j.Joined || len(j.Queue) == 0 {
		return false
	}
	id := j.Queue[0].ID
	return slices.Contains(j.Ready[j.Host.ID], id) && slices.Contains(j.Ready[j.Guest.ID], id)
}
