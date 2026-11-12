package app

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestTapeAndTimeCapsuleUseRecordedPlays(t *testing.T) {
	p, a, b := queueFixture(t)
	for _, album := range []*album{a, b} {
		for i := range album.tracks {
			album.tracks[i].likeID = album.tracks[i].title
		}
	}
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	p.listenHistory = []listenEntry{
		{a.tracks[0].path, now.AddDate(0, -2, 0), 66},
		{a.tracks[0].path, now.AddDate(0, -1, -1), 66},
		{b.tracks[1].path, now.Add(-time.Hour), 240},
		{a.tracks[1].path, now, 180},
	}
	p.openTape("3", now)
	if tape := p.playlistDialog; tape == nil ||
		!slices.Equal(tape.tracks, []string{"a1", "b2", "a2"}) ||
		!strings.Contains(tape.tapeCard, "8:06") {
		t.Fatalf("tape preview: %+v", tape)
	}
	if found, index, reason := p.timeCapsulePick(now); found != a || index != 0 || !strings.Contains(reason, "2 recorded plays") {
		t.Fatalf("time capsule: %v %d %q", found, index, reason)
	}
}

func TestBlindNowPlayingMasksMetadataUntilReveal(t *testing.T) {
	p, a, _ := queueFixture(t)
	a.title = "Identifying album"
	a.tracks[0].title = "Identifying track"
	a.tracks[0].artist = "Identifying artist"
	p.active, p.popup, p.blind = a, true, true
	p.why = []string{"Identifying recommendation"}
	frame := strings.Join(p.frame(100, 30), "\n")
	if strings.Contains(frame, "Identifying") || !strings.Contains(frame, "R Reveal song") {
		t.Fatal("blind mode exposed metadata or omitted the reveal control")
	}
	p.handle("R")
	frame = strings.Join(p.frame(100, 30), "\n")
	if !strings.Contains(frame, "Identifying track") {
		t.Fatal("R did not reveal the track")
	}
}

func TestTripRouteUsesZeroBasedLeg(t *testing.T) {
	for _, tc := range []struct {
		leg  tripLeg
		want string
	}{
		{tripLeg{Step: 0, Of: 4}, "0/4"},
		{tripLeg{Step: 2, Of: 4}, "2/4"},
		{tripLeg{Step: 4, Of: 4}, "Destination reached"},
		{tripLeg{Step: 4, Of: 4, Hop: true}, "Surprise detour"},
	} {
		if route := tripRoute(&tc.leg); !strings.Contains(route, tc.want) {
			t.Errorf("tripRoute(%+v) = %q, want %q", tc.leg, route, tc.want)
		}
	}
}
