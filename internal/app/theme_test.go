package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func visible(s string) string { return ansi.ReplaceAllString(s, "") }

func TestEveryThemeKeepsRowsAlignedAndDistinct(t *testing.T) {
	t.Cleanup(func() { applyTheme(&themes[0]) })
	p, a, _ := queueFixture(t)
	p.active, p.playingIndex = a, 0
	p.state.playing = true
	p.queue = []track{a.tracks[1]}
	seen := map[string]string{}
	for i := range themes {
		applyTheme(&themes[i])
		for _, size := range [][2]int{{45, 13}, {80, 24}, {120, 40}} {
			for n, row := range p.frame(size[0], size[1]) {
				if got := textWidth(visible(row)); got != size[0] {
					t.Fatalf("%s %dx%d row %d is %d columns wide: %q", themes[i].name, size[0], size[1], n, got, visible(row))
				}
			}
		}
		p.popup = true
		p.themePicker = &themePicker{index: i, original: &themes[0]}
		for n, row := range p.frame(100, 30) {
			if strings.Contains(visible(row), themes[i].box.tl) && textWidth(visible(row)) < 100 {
				t.Fatalf("%s row %d lost columns: %q", themes[i].name, n, visible(row))
			}
		}
		p.popup, p.themePicker = false, nil
		key := strings.Join(p.frame(100, 30), "\n")
		if other, dup := seen[key]; dup {
			t.Fatalf("%s renders identically to %s", themes[i].name, other)
		}
		seen[key] = themes[i].name
	}
}

func TestMagiShowsThreeVerdicts(t *testing.T) {
	t.Cleanup(func() { applyTheme(&themes[0]) })
	p, a, _ := queueFixture(t)
	applyTheme(themeByName("magi"))
	line := visible(p.ruleRow(100))
	for _, want := range []string{"MELCHIOR·1 否決", "BALTHASAR·2 否決", "CASPER·3 保留"} {
		if !strings.Contains(line, want) {
			t.Fatalf("idle strip missing %q: %q", want, line)
		}
	}
	p.active, p.state.playing = a, true
	p.queue = []track{a.tracks[1]}
	line = visible(p.ruleRow(100))
	for _, want := range []string{"MELCHIOR·1 承認", "BALTHASAR·2 保留", "CASPER·3 承認"} {
		if !strings.Contains(line, want) {
			t.Fatalf("playing strip missing %q: %q", want, line)
		}
	}
}

func TestThemePickerPreviewsCancelsAndSaves(t *testing.T) {
	t.Cleanup(func() { applyTheme(&themes[0]) })
	p, _, _ := queueFixture(t)
	p.themePath = filepath.Join(t.TempDir(), "Elsewhere", "theme")
	for _, key := range []string{"/", "t", "h", "e", "m", "e", "s", "\r"} {
		p.handle(key)
	}
	if p.themePicker == nil {
		t.Fatal("/themes should open the picker")
	}
	p.handle("\x1b[B")
	if activeTheme.name != "MAGI" {
		t.Fatalf("moving should preview the next theme, got %s", activeTheme.name)
	}
	p.handle("\x1b")
	if activeTheme.name != "Amber" || p.themePicker != nil {
		t.Fatalf("Esc should restore Amber, got %s", activeTheme.name)
	}
	p.themePicker = &themePicker{original: activeTheme}
	p.handle("\x1b[B")
	p.handle("\r")
	saved, err := os.ReadFile(p.themePath)
	if err != nil || themeByName(string(saved)).name != "MAGI" {
		t.Fatalf("saved theme = %q, %v", saved, err)
	}
	if themeByName("nonsense").name != "Amber" {
		t.Fatal("unknown names should fall back to Amber")
	}
}
