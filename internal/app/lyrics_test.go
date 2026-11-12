package app

import (
	"strings"
	"testing"
)

func TestSyncedLyrics(t *testing.T) {
	lines := parseLyrics(strings.NewReader("\uFEFF[ar:Example]\n[00:04.25][00:08.50]Chorus\n[00:01.00]Verse\n[offset:250]\n[00:12.00]\n"))
	want := []lyricLine{{1.25, "Verse"}, {4.5, "Chorus"}, {8.75, "Chorus"}, {12.25, ""}}
	if len(lines) != len(want) {
		t.Fatalf("got %v, want %v", lines, want)
	}
	for i, line := range want {
		if lines[i] != line {
			t.Fatalf("line %d = %#v, want %#v", i, lines[i], line)
		}
	}
	for _, check := range []struct {
		seconds float64
		index   int
	}{{0, -1}, {1.25, 0}, {4.49, 0}, {4.5, 1}, {10, 2}, {12.25, 3}} {
		if got := currentLyric(lines, check.seconds); got != check.index {
			t.Errorf("at %.2f got line %d, want %d", check.seconds, got, check.index)
		}
	}
}
