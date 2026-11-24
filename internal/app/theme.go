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
	// accent marks what matters most (titles, keys, the played part of a bar), muted what matters
	// least (secondary labels), border draws frames and dividers.
	accent, muted, border string

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
		accent: "\x1b[38;5;214m", muted: "\x1b[38;5;137m", border: "\x1b[38;5;94m",
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
		accent: "\x1b[38;5;214m", muted: "\x1b[38;5;130m", border: "\x1b[38;5;130m",
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
		accent: "\x1b[38;5;118m", muted: "\x1b[38;5;28m", border: "\x1b[38;5;22m",
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
		accent: "\x1b[38;5;51m", muted: "\x1b[38;5;97m", border: "\x1b[38;5;61m",
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
		accent: "\x1b[38;5;159m", muted: "\x1b[38;5;66m", border: "\x1b[38;5;24m",
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
		accent: "\x1b[38;5;225m", muted: "\x1b[38;5;132m", border: "\x1b[38;5;96m",
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
		accent: "\x1b[38;5;231m", muted: "\x1b[38;5;243m", border: "\x1b[38;5;239m",
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
	activeTheme                                        = &themes[0]
	amber, dim, panel, selected, accent, muted, border string
)

const (
	bold   = "\x1b[1m"
	unbold = "\x1b[22m"
)

func init() { applyTheme(&themes[0]) }

func applyTheme(t *theme) {
	activeTheme = t
	amber, dim, panel, selected = t.text, t.dim, t.panel, t.selected
	accent, muted, border = t.accent, t.muted, t.border
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
// Frames are drawn in the border color and headings in the accent, and each helper hands the
// text color back so callers can keep writing after it.
func boxTop(heading string, columns int) string {
	b := activeTheme.box
	if heading == "" {
		return border + b.tl + strings.Repeat(b.h, max(0, columns-2)) + b.tr + amber
	}
	heading = cell(heading, min(textWidth(heading), max(0, columns-2)))
	lead := ""
	if rest, ok := strings.CutPrefix(heading, b.h); ok {
		lead, heading = b.h, rest
	}
	return border + b.tl + lead + accent + bold + heading + unbold + border +
		strings.Repeat(b.h, max(0, columns-2-textWidth(lead+heading))) + b.tr + amber
}

// paneTop is boxTop with a second, quieter label at the right end, for list panes.
func paneTop(left, right string, columns int, focused bool) string {
	b := activeTheme.box
	frame, label := border, dim
	if focused {
		frame, label = amber, accent+bold
	}
	inner := max(0, columns-2)
	right = cell(right, min(textWidth(right), max(0, inner-textWidth(left)-2)))
	left = cell(left, min(textWidth(left), inner))
	fill := max(0, inner-textWidth(left)-textWidth(right))
	return frame + b.tl + label + left + unbold + frame + strings.Repeat(b.h, fill) + muted + right + frame + b.tr + amber
}

func boxBottom(columns int) string {
	b := activeTheme.box
	return border + b.bl + strings.Repeat(b.h, max(0, columns-2)) + b.br + amber
}

func boxSide(inner string) string {
	return border + activeTheme.box.v + amber + inner + border + activeTheme.box.v + amber
}

// boxDivider is a horizontal rule inside a box.
func boxDivider(columns int) string {
	b := activeTheme.box
	return border + b.v + strings.Repeat("─", max(0, columns-2)) + b.v + amber
}

// ruleRow is the "now playing" divider. MAGI replaces its tail with the three computers' votes:
// MELCHIOR-1 follows playback, BALTHASAR-2 the like on the current track, CASPER-3 the queue.
func (p *player) ruleRow(columns int) string {
	t := activeTheme
	h := t.box.h
	head := " " + h + h + " " + t.nowPlaying + " "
	if !t.magi || columns < textWidth(head)+60 {
		return " " + border + h + h + " " + muted + t.nowPlaying + " " + border +
			strings.Repeat(h, max(0, columns-textWidth(head))) + reset
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
	label, color := verdict(p.state.Playing, current != nil)
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
	dialog = append(dialog, dialogFooter(" ↑↓ Preview  Enter Keep  Esc Cancel", columns), boxBottom(columns))
	x, y := (width-columns)/2+1, max(0, (len(rows)-len(dialog))/2)
	cursor := fmt.Sprintf("\x1b[%dG", x)
	for i, line := range dialog {
		if y+i < len(rows) {
			rows[y+i] += cursor + panel + amber + line + reset
		}
	}
}
