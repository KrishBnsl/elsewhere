package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type listenEntry struct {
	Path    string
	At      time.Time
	Seconds float64
}

const maxListenHistory = 4096

func (p *player) rememberTrack(t track) {
	p.listenHistory = append(p.listenHistory, listenEntry{t.path, time.Now(), audioState().duration})
	if len(p.listenHistory) > maxListenHistory {
		p.listenHistory = p.listenHistory[1:]
	}
}

func (p *player) openTape(argument string, now time.Time) {
	n := 5
	if argument != "" {
		count, err := strconv.Atoi(argument)
		if err != nil || count < 1 || count > 10 {
			p.notice = "Use /tape or /tape 1–10"
			return
		}
		n = count
	}
	var ids []string
	var duration float64
	for i := len(p.listenHistory) - 1; i >= 0 && len(ids) < n; i-- {
		entry := p.listenHistory[i]
		if a, j := p.findTrack(entry.Path); a != nil {
			ids = append(ids, a.tracks[j].likeID)
			duration += entry.Seconds
		}
	}
	if len(ids) == 0 {
		p.notice = "Play a local track before making a tape"
		return
	}
	for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
		ids[i], ids[j] = ids[j], ids[i]
	}
	card := fmt.Sprintf("%s · %d tracks · %s", now.Format("02 Jan 2006"), len(ids), clock(duration))
	p.playlistDialog = &playlistDialog{kind: "np", tracks: ids, tapeCard: card, description: card}
}

func (p *player) timeCapsulePick(now time.Time) (*album, int, string) {
	type plays struct {
		count int
		last  time.Time
	}
	history := make(map[string]plays)
	for _, entry := range p.listenHistory {
		rec := history[entry.Path]
		rec.count++
		if entry.At.After(rec.last) {
			rec.last = entry.At
		}
		history[entry.Path] = rec
	}
	cutoff := now.AddDate(0, 0, -30)
	var best, forgotten *album
	var index, forgottenIndex int
	var bestRec plays
	current := p.current()
	for _, a := range p.albums {
		for i, t := range a.tracks {
			if current != nil && current.path == t.path {
				continue
			}
			rec := history[t.path]
			if rec.count == 0 {
				if forgotten == nil && p.likes.contains(t.likeID) {
					forgotten, forgottenIndex = a, i
				}
				continue
			}
			if rec.last.After(cutoff) {
				continue
			}
			if best == nil || rec.count > bestRec.count || rec.count == bestRec.count && rec.last.Before(bestRec.last) {
				best, index, bestRec = a, i, rec
			}
		}
	}
	if best != nil {
		return best, index, fmt.Sprintf("%d recorded %s · last heard %s", bestRec.count, playLabel(bestRec.count), bestRec.last.Local().Format("02 Jan 2006"))
	}
	if forgotten != nil {
		return forgotten, forgottenIndex, "In Liked Songs · no plays in Elsewhere's history"
	}
	return nil, 0, ""
}

func playLabel(n int) string {
	if n == 1 {
		return "play"
	}
	return "plays"
}

func (p *player) openTimeCapsule(now time.Time) {
	a, i, reason := p.timeCapsulePick(now)
	// Prefer dated imported plays to the liked-only fallback when available.
	if p.engine != nil && (a == nil || strings.HasPrefix(reason, "In Liked Songs")) {
		if reply, err := p.engine.request(map[string]any{"op": "timecapsule"}); err == nil {
			current := p.current()
			recent := make(map[string]bool)
			cutoff := now.AddDate(0, 0, -30)
			for _, entry := range p.listenHistory {
				if entry.At.After(cutoff) {
					recent[entry.Path] = true
				}
			}
			for _, pick := range reply.Songs {
				if recent[pick.Path] || current != nil && current.path == pick.Path {
					continue
				}
				if found, index := p.findTrack(pick.Path); found != nil && pick.PlayCount > 0 {
					a, i = found, index
					reason = fmt.Sprintf("%d imported %s · last heard %s", pick.PlayCount, playLabel(pick.PlayCount), time.Unix(pick.LastPlayed, 0).Local().Format("02 Jan 2006"))
					break
				}
			}
		}
	}
	if a == nil {
		p.notice = "Time capsule · no forgotten likes or tracks last heard 30+ days ago"
		return
	}
	p.leaving("choose")
	if p.start(a, i) {
		p.stopRadio()
		p.clearLiveStation()
		p.leaveQueue()
		p.melisma, p.popup = false, true
		p.tripDestination, p.tripDetour = "", false
		p.why = strings.SplitN(reason, " · ", 2)
		p.notice = "TIME CAPSULE"
	}
}

type tripLeg struct {
	Step int  `json:"step"`
	Of   int  `json:"of"`
	Hop  bool `json:"hop"`
}

func tripRoute(leg *tripLeg) string {
	if leg == nil || leg.Of < 0 || leg.Step < 0 || leg.Step > leg.Of {
		return "Home ───── ◇ Destination"
	}
	if leg.Hop {
		return "✦ Surprise detour"
	}
	if leg.Of == 0 || leg.Step == leg.Of {
		return "◆ Destination reached"
	}
	const stops = 5
	at := leg.Step * (stops - 1) / leg.Of
	dots := []rune("○○○○◇")
	dots[at] = '●'
	route := make([]string, stops)
	for i, dot := range dots {
		route[i] = string(dot)
	}
	return fmt.Sprintf("%s · %d/%d", strings.Join(route, "─"), leg.Step, leg.Of)
}

func (p *player) hiddenTrack() bool {
	return p.blind && !p.revealed && p.localSong() != nil
}

func (p *player) startTrip(destination string) {
	if destination == "" {
		p.notice = "Try /trip rainy neon or /trip to an artist"
		return
	}
	fields := map[string]any{"vibe": destination}
	if name, rest, ok := strings.Cut(destination, " "); ok && strings.EqualFold(name, "to") {
		destination = strings.TrimSpace(rest)
		fields = map[string]any{"to": destination}
	}
	if destination == "" {
		p.notice = "Add a song, album, or artist after /trip to"
		return
	}
	wasChoosing := p.melisma
	if !p.setMelismaMode(fields, "heading toward "+destination) {
		return
	}
	if t := p.localSong(); wasChoosing && t != nil && !p.finished && !p.restored {
		p.engine.tell("from", "path", t.path)
	}
	p.tripDestination = destination
	p.popup = p.active != nil
}
