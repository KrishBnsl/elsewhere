package app

import (
	"elsewhere/internal/fsutil"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/width"
)

type boxStyle struct{ tl, tr, bl, br, h, v string }

type theme struct {
	name, description string

	// Terminal styles. screen paints the whole window; empty keeps the terminal background.
	text, dim, screen, panel, selected, rule string

	box              boxStyle
	barDone, barTodo string

	title, titleRight, nowPlaying, overlayTitle, visualTitle string
	play, pause, stop                                        string

	// 256-color indexes for the spectrum: bar body, bar top, falling glyphs and their leading glyph.
	bar, peak, rain, head int
	rainGlyphs            string

	// magi draws the three-computer verdict strip from Neon Genesis Evangelion.
	magi bool
}

var (
	singleBox  = boxStyle{"┌", "┐", "└", "┘", "─", "│"}
	roundBox   = boxStyle{"╭", "╮", "╰", "╯", "─", "│"}
	doubleBox  = boxStyle{"╔", "╗", "╚", "╝", "═", "║"}
	heavyBox   = boxStyle{"┏", "┓", "┗", "┛", "━", "┃"}
	slantedBox = boxStyle{"╱", "╲", "╲", "╱", "━", "┃"}
)

var themes = []theme{
	{
		name: "Amber", description: "Warm amber terminal (default)",
		text: "\x1b[38;5;221m", dim: "\x1b[38;5;179m", panel: "\x1b[48;5;235m", rule: "\x1b[48;5;232m",
		selected: "\x1b[38;5;232;48;5;214m",
		box:      singleBox, barDone: "━", barTodo: "─",
		title: " ELSEWHERE / LOCAL AUDIO", titleRight: "STEREO ", nowPlaying: "NOW PLAYING",
		overlayTitle: "NOW PLAYING", visualTitle: "SPECTRUM / AMBER RAIN",
		play: "PLAY", pause: "PAUSE", stop: "STOP",
		bar: 178, peak: 221, rain: 94, head: 136, rainGlyphs: "01+.:/|",
	},
	{
		name: "MAGI", description: "NERV MAGI system: orange on black, three-computer verdicts",
		text: "\x1b[38;5;208m", dim: "\x1b[38;5;166m", screen: "\x1b[48;5;16m", panel: "\x1b[48;5;233m", rule: "\x1b[48;5;16m",
		selected: "\x1b[38;5;16;48;5;208m",
		box:      slantedBox, barDone: "▓", barTodo: "░",
		title: " MAGI SYSTEM  //  CODE:473  FILE:ELSEWHERE", titleRight: "EX_MODE:OFF ", nowPlaying: "MAGI DELIBERATION",
		overlayTitle: "MAGI / ACTIVE FILE", visualTitle: "WAVEFORM / HEX STREAM",
		play: "承認 PLAY", pause: "保留 HOLD", stop: "否決 IDLE",
		bar: 166, peak: 208, rain: 22, head: 46, rainGlyphs: "0123456789ABCDEF", magi: true,
	},
	{
		name: "Phosphor", description: "Green CRT phosphor",
		text: "\x1b[38;5;46m", dim: "\x1b[38;5;34m", screen: "\x1b[48;5;16m", panel: "\x1b[48;5;233m", rule: "\x1b[48;5;16m",
		selected: "\x1b[38;5;16;48;5;46m",
		box:      singleBox, barDone: "█", barTodo: "░",
		title: " ELSEWHERE / PHOSPHOR TERMINAL", titleRight: "P1 GREEN ", nowPlaying: "NOW PLAYING",
		overlayTitle: "NOW PLAYING", visualTitle: "SPECTRUM / MATRIX RAIN",
		play: "PLAY", pause: "PAUSE", stop: "STOP",
		bar: 40, peak: 46, rain: 22, head: 82, rainGlyphs: "01+.:/|<>",
	},
	{
		name: "Synthwave", description: "Neon pink and cyan on midnight blue",
		text: "\x1b[38;5;213m", dim: "\x1b[38;5;141m", screen: "\x1b[48;5;17m", panel: "\x1b[48;5;53m", rule: "\x1b[48;5;17m",
		selected: "\x1b[38;5;16;48;5;51m",
		box:      doubleBox, barDone: "━", barTodo: "─",
		title: " ELSEWHERE / NEON DRIVE", titleRight: "OUTRUN 1986 ", nowPlaying: "NOW CRUISING",
		overlayTitle: "NOW CRUISING", visualTitle: "SPECTRUM / NEON GRID",
		play: "DRIVE", pause: "IDLE", stop: "PARKED",
		bar: 207, peak: 51, rain: 93, head: 201, rainGlyphs: "/\\|-+*",
	},
	{
		name: "Ice", description: "Cool frost blue with rounded frames",
		text: "\x1b[38;5;117m", dim: "\x1b[38;5;67m", panel: "\x1b[48;5;235m", rule: "\x1b[48;5;233m",
		selected: "\x1b[38;5;16;48;5;81m",
		box:      roundBox, barDone: "━", barTodo: "─",
		title: " ELSEWHERE / FROSTLINE", titleRight: "-12°C ", nowPlaying: "NOW PLAYING",
		overlayTitle: "NOW PLAYING", visualTitle: "SPECTRUM / SNOWFALL",
		play: "PLAY", pause: "PAUSE", stop: "STOP",
		bar: 75, peak: 123, rain: 24, head: 39, rainGlyphs: "*+.:'",
	},
	{
		name: "Sakura", description: "Soft pink with rounded frames",
		text: "\x1b[38;5;218m", dim: "\x1b[38;5;175m", panel: "\x1b[48;5;236m", rule: "\x1b[48;5;233m",
		selected: "\x1b[38;5;16;48;5;218m",
		box:      roundBox, barDone: "━", barTodo: "─",
		title: " ELSEWHERE / SAKURA", titleRight: "春 ", nowPlaying: "NOW PLAYING",
		overlayTitle: "NOW PLAYING", visualTitle: "SPECTRUM / PETAL FALL",
		play: "PLAY", pause: "PAUSE", stop: "STOP",
		bar: 211, peak: 225, rain: 132, head: 217, rainGlyphs: "*.o+",
	},
	{
		name: "Mono", description: "High-contrast black and white with heavy frames",
		text: "\x1b[38;5;255m", dim: "\x1b[38;5;245m", panel: "\x1b[48;5;236m", rule: "\x1b[48;5;232m",
		selected: "\x1b[38;5;16;48;5;255m",
		box:      heavyBox, barDone: "━", barTodo: "─",
		title: " ELSEWHERE / MONOCHROME", titleRight: "MONO ", nowPlaying: "NOW PLAYING",
		overlayTitle: "NOW PLAYING", visualTitle: "SPECTRUM / STATIC",
		play: "PLAY", pause: "PAUSE", stop: "STOP",
		bar: 250, peak: 255, rain: 240, head: 245, rainGlyphs: "01+.:/|",
	},
}

const reset = "\x1b[0m"

// The styles below always mirror the active theme so every drawing helper can use them directly.
var (
	activeTheme                        = &themes[0]
	amber, dim, black, panel, selected string
)

func init() { applyTheme(&themes[0]) }

func applyTheme(t *theme) {
	activeTheme = t
	amber, dim, black, panel, selected = t.text, t.dim, t.rule, t.panel, t.selected
}

func themeByName(name string) *theme {
	name = strings.TrimSpace(name)
	for i := range themes {
		if strings.EqualFold(themes[i].name, name) {
			return &themes[i]
		}
	}
	return &themes[0]
}

func themeIndex(t *theme) int {
	for i := range themes {
		if &themes[i] == t {
			return i
		}
	}
	return 0
}

func (p *player) saveTheme() {
	if p.themePath == "" {
		return
	}
	if err := fsutil.WriteFile(p.themePath, []byte(activeTheme.name+"\n")); err != nil {
		p.notice = "Could not save theme: " + err.Error()
	}
}

func runeWidth(r rune) int {
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
		return 0
	}
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	}
	return 1
}

func textWidth(s string) int {
	columns := 0
	for _, r := range s {
		columns += runeWidth(r)
	}
	return columns
}

// boxTop draws the top edge with a heading, boxBottom the bottom edge, boxSide wraps a content line.
func boxTop(heading string, columns int) string {
	b := activeTheme.box
	return b.tl + heading + strings.Repeat(b.h, max(0, columns-2-textWidth(heading))) + b.tr
}

func boxBottom(columns int) string {
	b := activeTheme.box
	return b.bl + strings.Repeat(b.h, max(0, columns-2)) + b.br
}

func boxSide(inner string) string {
	return activeTheme.box.v + inner + activeTheme.box.v
}

// ruleRow is the "now playing" divider. MAGI replaces its tail with the three computers' votes:
// MELCHIOR-1 follows playback, BALTHASAR-2 the like on the current track, CASPER-3 the queue.
func (p *player) ruleRow(columns int) string {
	t := activeTheme
	h := t.box.h
	head := " " + h + h + " " + t.nowPlaying + " "
	if !t.magi || columns < textWidth(head)+60 {
		return dim + cell(head+strings.Repeat(h, columns), columns) + reset
	}
	const (
		green = "\x1b[38;5;46m"
		red   = "\x1b[38;5;196m"
	)
	verdict := func(ok, hold bool) (string, string) {
		switch {
		case ok:
			return "承認", green
		case hold:
			return "保留", t.text
		}
		return "否決", red
	}
	current := p.current()
	type voter struct{ name, label, color string }
	var voters []voter
	label, color := verdict(p.state.playing, current != nil)
	voters = append(voters, voter{"MELCHIOR·1", label, color})
	label, color = verdict(current != nil && p.likes.contains(current.likeID), current != nil)
	voters = append(voters, voter{"BALTHASAR·2", label, color})
	label, color = verdict(len(p.queue) > 0, true)
	voters = append(voters, voter{"CASPER·3", label, color})

	var line strings.Builder
	line.WriteString(t.dim + head)
	used := textWidth(head)
	for _, v := range voters {
		line.WriteString(t.dim + h + h + " " + t.text + v.name + " " + v.color + v.label + " ")
		used += 2 + 1 + textWidth(v.name) + 1 + textWidth(v.label) + 1
	}
	line.WriteString(t.dim + strings.Repeat(h, max(0, columns-used)))
	return line.String() + reset
}

func themeLine(t *theme, selectedRow bool) string {
	text := fmt.Sprintf("   %-10s %s", t.name, t.description)
	if selectedRow {
		text = fmt.Sprintf(" › %-10s %s", t.name, t.description)
	}
	return text
}

type themePicker struct {
	index    int
	original *theme
}

func (p *player) handleThemePicker(key string) {
	picker := p.themePicker
	preview := func(index int) {
		picker.index = max(0, min(len(themes)-1, index))
		applyTheme(&themes[picker.index])
	}
	switch key {
	case "\x1b":
		applyTheme(picker.original)
		p.themePicker = nil
	case "\x1b[A", "k":
		preview(picker.index - 1)
	case "\x1b[B", "j":
		preview(picker.index + 1)
	case "\r", "\n":
		p.themePicker = nil
		p.saveTheme()
		p.notice = "Theme: " + activeTheme.name
	}
}

func (p *player) overlayThemePicker(rows []string, width int) {
	columns := min(75, width-4)
	row := func(text, style string) string {
		return boxSide(style + cell(text, columns-2) + panel + amber)
	}
	dialog := []string{boxTop(" THEMES ", columns)}
	for i := range themes {
		// Each entry is painted in its own colors, so the list doubles as a palette preview.
		style := themes[i].text
		if i == p.themePicker.index {
			style = themes[i].selected
		}
		dialog = append(dialog, row(themeLine(&themes[i], i == p.themePicker.index), style))
	}
	dialog = append(dialog, row(" ↑↓ Preview  Enter Keep  Esc Cancel", dim), boxBottom(columns))
	x, y := (width-columns)/2+1, max(0, (len(rows)-len(dialog))/2)
	cursor := fmt.Sprintf("\x1b[%dG", x)
	for i, line := range dialog {
		if y+i < len(rows) {
			rows[y+i] += cursor + panel + amber + line + reset
		}
	}
}
