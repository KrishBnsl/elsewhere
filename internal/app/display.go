package app

import (
	"elsewhere/internal/audio"
	"elsewhere/internal/kitty"
	"elsewhere/internal/lyrics"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

func cell(text string, columns int) string {
	var line strings.Builder
	for _, r := range text {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			r = '?'
		}
		w := runeWidth(r)
		if w > columns {
			break
		}
		line.WriteRune(r)
		columns -= w
	}
	line.WriteString(strings.Repeat(" ", columns))
	return line.String()
}

func clock(seconds float64) string {
	whole := int(seconds)
	return fmt.Sprintf("%d:%02d", whole/60, whole%60)
}

// homePath shortens a path under the home folder to ~/….
func homePath(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
			if rel == "." {
				return "~"
			}
			return "~" + string(filepath.Separator) + rel
		}
	}
	return path
}

// trackTitle drops a leading track number from titles that came from file names like
// "03 Roygbiv", since the list already numbers its rows.
func trackTitle(t track, number int) string {
	title := t.title
	digits := 0
	for digits < len(title) && digits < 3 && title[digits] >= '0' && title[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits >= len(title) {
		return title
	}
	n, _ := strconv.Atoi(title[:digits])
	if n != number && (t.number == 0 || n != t.number) {
		return title
	}
	rest := strings.TrimLeft(title[digits:], " .-_")
	if rest == "" || len(rest) == len(title)-digits {
		return title
	}
	return rest
}

// engineBadge splits the engine status into a colored state glyph and its text.
func engineBadge(status string) []seg {
	for _, glyph := range []string{"● ", "▶ "} {
		if rest, ok := strings.CutPrefix(status, glyph); ok {
			return []seg{{"MELISMA ", muted}, {glyph, accent}, {rest, dim}}
		}
	}
	return []seg{{"MELISMA ", muted}, {"○ ", muted}, {status, dim}}
}

func (p *player) frame(width, height int) []string {
	p.coverPlacement = kitty.Placement{}
	row := func(text, style string) string { return activeTheme.screen + style + cell(text, width) + reset }
	if width < 45 || height < 13 {
		return []string{row(" Enlarge terminal (45 x 13). :q quits", amber)}
	}
	th := activeTheme
	screen := th.screen
	if screen == "" {
		screen = "\x1b[49m"
	}
	base := screen + amber
	bar := th.panel + amber
	state := th.stop
	current := p.current()
	hidden := p.hiddenTrack()
	if current != nil {
		state = th.pause
	}
	if p.state.Playing {
		state = th.play
	}

	// Header: the theme's name on the left, Melisma and the theme's tag on the right.
	var right []seg
	if status := p.engineStatus(); width >= 70 && status != "off" {
		right = append(engineBadge(status), seg{"   ", dim})
	}
	right = append(right, seg{strings.TrimSpace(th.titleRight) + " ", muted})
	header := spread(width, bar, []seg{{" ◆", accent}, {th.title, accent + bold}}, right)

	// What's playing, then the transport.
	var now []seg
	switch {
	case hidden:
		now = []seg{{"  ◌  ", muted}, {"Mystery track", accent + bold}, {"   R to reveal", dim}}
	case current != nil:
		glyph := "‖"
		if p.state.Playing {
			glyph = "▶"
		}
		now = []seg{{"  " + glyph + "  ", accent}, {trackTitle(*current, p.playingIndex+1), accent + bold}, {"  " + current.artist, amber}}
		if current.album != "" {
			now = append(now, seg{"  ·  " + current.album, dim})
		}
	default:
		now = []seg{{"  ○  ", muted}, {"Choose an album or track and press Enter", dim}}
	}
	volume := []seg{{"VOL ", muted}, raw(meter(p.volume, 10)), {fmt.Sprintf(" %3d%%  ", p.volume), amber}}
	var transport []seg
	if p.live != nil {
		transport = []seg{{"  " + p.liveStatus() + "  ", accent + bold}, {p.live.station.name, amber}}
	} else {
		elapsed, duration := p.state.Elapsed, p.state.Duration
		barWidth := max(8, min(56, width-textWidth(state)-42))
		transport = []seg{{"  " + state + "  ", dim}, {clock(elapsed) + " ", amber},
			raw(progressBar(elapsed, duration, barWidth)), {" " + clock(duration), dim}}
	}

	leftWidth := max(17, width*34/100)
	rightWidth := width - leftWidth
	inner := func(n int) int { return max(0, n-2) }
	bodyHeight := height - 9
	tracks := p.tracks()
	albumStart := max(0, min(p.albumIndex-bodyHeight/2, len(p.results)-bodyHeight))
	trackStart := max(0, min(p.trackIndex-bodyHeight/2, len(tracks)-bodyHeight))
	albumCount := 0
	for _, a := range p.results {
		if !a.likedSongs && !a.isPlaylist {
			albumCount++
		}
	}

	var entry *album
	if len(p.results) > 0 {
		entry = p.results[p.albumIndex]
	}
	rightTitle, rightCount := " Tracks ", ""
	if entry != nil {
		rightTitle = " " + entry.title + " "
		rightCount = fmt.Sprintf(" %d ", len(tracks))
		if len(tracks) == 1 {
			rightCount = " 1 "
		}
	}
	frameColor := func(focused bool) string {
		if focused {
			return amber
		}
		return border
	}
	paneBottom := func(columns int, focused bool) string {
		b := th.box
		return frameColor(focused) + b.bl + strings.Repeat(b.h, inner(columns)) + b.br
	}

	rows := []string{
		header + reset,
		screen + p.ruleRow(width),
		line(width, base, now...) + reset,
		spread(width, base, transport, volume) + reset,
		screen + paneTop(" Library ", fmt.Sprintf(" %d ", len(p.results)), leftWidth, !p.trackPane) +
			paneTop(rightTitle, rightCount, rightWidth, p.trackPane) + reset,
	}
	lw, rw := inner(leftWidth), inner(rightWidth)
	for n := range bodyHeight {
		a, t := albumStart+n, trackStart+n
		left, right := line(lw, base), line(rw, base)
		if a < len(p.results) {
			album := p.results[a]
			chosen := a == p.albumIndex
			icon, count := "  ", ""
			if album.likedSongs {
				icon, count = "♥ ", fmt.Sprint(len(album.tracks))
			} else if album.isPlaylist {
				icon, count = "≡ ", fmt.Sprint(len(album.tracks))
			}
			name, extra := album.title, ""
			if album.artist != "" {
				extra = " · " + album.artist
			}
			switch {
			case chosen && !p.trackPane:
				left = spread(lw, selected, []seg{{" " + icon, ""}, {name, bold}, {extra, ""}}, []seg{{count + " ", ""}})
			case chosen:
				left = spread(lw, base, []seg{{"▌", accent}, {icon, accent}, {name, accent + bold}, {extra, muted}}, []seg{{count + " ", muted}})
			default:
				left = spread(lw, base, []seg{{" " + icon, muted}, {name, amber}, {extra, muted}}, []seg{{count + " ", muted}})
			}
		}
		if t < len(tracks) {
			tr := tracks[t]
			playing := !hidden && current != nil && current.path == tr.path
			marker, mark := "  ", "  "
			if playing {
				marker = "▶ "
				if !p.state.Playing {
					marker = "‖ "
				}
			}
			markStyle := accent
			if p.likes.contains(tr.likeID) {
				mark = "♥ "
			} else if p.unknown(tr) {
				mark, markStyle = "· ", muted // Melisma hasn't analyzed it yet
			}
			number := fmt.Sprintf("%2d  ", t+1)
			title := trackTitle(tr, t+1)
			tag := format(tr.path)
			if entry != nil && (entry.isPlaylist || entry.likedSongs) && tr.artist != "" {
				tag = tr.artist
			}
			titleStyle := amber
			if playing {
				titleStyle = accent + bold
			}
			switch {
			case t == p.trackIndex && p.trackPane:
				right = spread(rw, selected, []seg{{" " + marker, ""}, {mark, ""}, {number, ""}, {title, bold}}, []seg{{tag + " ", ""}})
			case t == p.trackIndex:
				right = spread(rw, base, []seg{{"▌" + marker, accent}, {mark, markStyle}, {number, muted}, {title, titleStyle}}, []seg{{tag + " ", muted}})
			default:
				right = spread(rw, base, []seg{{" " + marker, accent}, {mark, markStyle}, {number, muted}, {title, titleStyle}}, []seg{{tag + " ", muted}})
			}
		}
		lf, rf := frameColor(!p.trackPane), frameColor(p.trackPane)
		rows = append(rows, screen+lf+th.box.v+left+screen+lf+th.box.v+rf+th.box.v+right+screen+rf+th.box.v+reset)
	}
	rows = append(rows, screen+paneBottom(leftWidth, !p.trackPane)+paneBottom(rightWidth, p.trackPane)+reset)

	detail := []seg{{" " + homePath(p.path), dim}}
	if len(tracks) > 0 {
		t := tracks[p.trackIndex]
		detail = []seg{{" " + t.artist, amber}, {"  ·  " + format(t.path), dim}}
		if p.unknown(t) {
			detail = append(detail, seg{"  ·  Melisma hasn't heard this yet: /analyze", muted})
		}
	}
	if entry != nil {
		switch {
		case entry.isPlaylist && entry.description != "":
			detail = []seg{{" " + entry.description, dim}}
		case entry.isPlaylist && len(tracks) == 0:
			detail = []seg{{" Select a song and use /ap to add it here", dim}}
		case entry.likedSongs && len(tracks) == 0:
			detail = []seg{{" Press * on a track to add it here", dim}}
		}
	}
	if p.notice != "" {
		detail = []seg{{" » ", accent + bold}, {p.notice, amber}}
	}

	var modes []seg
	if p.repeat != "" && p.repeat != "off" {
		modes = append(modes, seg{"repeat " + p.repeat + "  ", muted})
	}
	if p.shuffle {
		modes = append(modes, seg{"shuffle  ", muted})
	}
	if len(p.queue) > 0 {
		modes = append(modes, seg{fmt.Sprintf("queue %d  ", len(p.queue)), muted})
	}
	if p.melisma && p.engine != nil {
		modes = append(modes, seg{"melisma  ", accent})
	}
	status := spread(width, base, []seg{{fmt.Sprintf(" %d albums", albumCount), dim}, {"  ·  " + homePath(p.path), muted}}, modes)
	if p.searching || p.query != "" {
		query, cursor := p.query, ""
		if p.searching {
			query, cursor = p.searchPrompt.input, "█"
		}
		status = line(width, base, seg{" / ", accent + bold}, seg{query, amber}, seg{cursor, accent}, seg{fmt.Sprintf("   ·  %d albums", albumCount), dim})
	}
	if p.command != nil {
		status = line(width, base, seg{" : ", accent + bold}, seg{p.command.input, amber}, seg{"█", accent})
	}
	hints := []string{"↑↓", "Move", "Tab", "Pane", "Enter", "Play", "Space", "Pause", "/", "Find",
		"</>", "Skip", "*", "Like", "M", "Player", "Q", "Queue", ":q", "Quit"}
	if width >= 120 {
		hints = append(hints, "/radio", "Radio")
		if p.engine != nil {
			hints = append(hints, "/melisma", "Melisma")
		}
	}
	rows = append(rows, line(width, base, detail...)+reset, status+reset, keyHints(width, bar, hints...)+reset)
	if p.popup && current != nil {
		if hidden {
			for i := 4; i < len(rows)-1; i++ {
				rows[i] = row("", dim)
			}
		}
		p.overlay(rows, width, *current, state)
		if p.searching || p.command != nil {
			rows[height-2] += "\x1b[1G" + status + reset
		}
	}
	if p.searching || p.command != nil {
		rows[len(rows)-1] = keyHints(width, bar, "Tab/↑↓", "Choose a command", "Enter", "Run", "Esc", "Cancel") + reset
		p.overlayPalette(rows, width)
	}
	if p.social.panel != nil {
		p.overlayFriends(rows, width)
	} else if p.finder != nil {
		p.overlayFinder(rows, width)
	} else if p.tasteView != nil {
		p.overlayTaste(rows, width)
	} else if p.themePicker != nil {
		p.overlayThemePicker(rows, width)
	} else if p.queueView != nil {
		p.overlayQueue(rows, width)
	} else if p.radioPicker != nil {
		p.overlayRadioPicker(rows, width)
	} else if p.playlistDialog != nil {
		p.overlayPlaylist(rows, width)
	} else if p.coverPrompt != nil {
		p.overlayCoverPrompt(rows, width)
	}
	return rows
}

func format(path string) string {
	return strings.ToUpper(strings.TrimPrefix(filepath.Ext(path), "."))
}

// vinylRow draws one row of a record, the stand-in when a song has no cover. Each cell is a half
// block, so the record is size*2 pixels square; the label takes the theme's accent.
func vinylRow(row, size int) string {
	background := colorIndex(activeTheme.panel, 235)
	label := colorIndex(activeTheme.accent, 214)
	labelEdge := colorIndex(activeTheme.dim, 179)
	pixels := size * 2
	color := func(x, y int) int {
		center := float64(pixels) / 2
		dx, dy := (float64(x)+0.5-center)/center, (float64(y)+0.5-center)/center
		r := math.Hypot(dx, dy)
		switch {
		case r > 0.97 || r < 0.06:
			return background
		case r < 0.30:
			return label
		case r < 0.34:
			return labelEdge
		}
		shade := 233
		if int(r*40)%4 == 0 {
			shade = 234
		}
		// A soft highlight across two opposite quarters, like light on vinyl.
		if sheen := math.Abs(math.Sin(2*math.Atan2(dy, dx) + 0.9)); sheen > 0.985 {
			shade += 3
		} else if sheen > 0.94 {
			shade += 2
		}
		return shade
	}
	var line strings.Builder
	lastTop, lastBottom := -1, -1
	for x := range pixels {
		upper, lower := color(x, row*2), color(x, row*2+1)
		if upper != lastTop {
			fmt.Fprintf(&line, "\x1b[38;5;%dm", upper)
			lastTop = upper
		}
		if lower != lastBottom {
			fmt.Fprintf(&line, "\x1b[48;5;%dm", lower)
			lastBottom = lower
		}
		line.WriteRune('▀')
	}
	line.WriteString(panel + amber)
	return line.String()
}

func artworkRow(cover *audio.Cover, row, size int) string {
	if cover == nil {
		return vinylRow(row, size)
	}
	var line strings.Builder
	// A half block gives each terminal cell independent top and bottom pixels.
	pixels := size * 2
	sample := func(pixel int) int { return (2*pixel + 1) * audio.CoverSize / (2 * pixels) }
	top, bottom := cover.Pixels[sample(row*2)], cover.Pixels[sample(row*2+1)]
	var lastTop, lastBottom [4]byte
	for x := range pixels {
		upper, lower := top[sample(x)], bottom[sample(x)]
		if x == 0 || upper != lastTop {
			fmt.Fprintf(&line, "\x1b[38;2;%d;%d;%dm", upper[0], upper[1], upper[2])
			lastTop = upper
		}
		if x == 0 || lower != lastBottom {
			fmt.Fprintf(&line, "\x1b[48;2;%d;%d;%dm", lower[0], lower[1], lower[2])
			lastBottom = lower
		}
		line.WriteRune('▀')
	}
	line.WriteString(panel + amber)
	return line.String()
}

func (p *player) overlay(rows []string, width int, t track, state string) {
	height := len(rows)
	hidden := p.hiddenTrack()
	showLyrics := p.lyricsMode && !hidden
	columns := min(112, width-max(2, width/10))
	size := min(audio.CoverSize/2, max(8, (columns-36)/3), height-5)
	infoWidth := columns - size*2 - 6
	x := (width-columns)/2 + 1
	padding := 0
	if height >= size+7 {
		padding = 1
	}
	y := (height - size - 4 - padding*2) / 2
	base := panel + amber
	l := func(parts ...seg) string { return line(infoWidth, base, parts...) }
	volume := fmt.Sprintf("APP %d%%", p.volume)
	if p.output >= 0 {
		volume += fmt.Sprintf("  OUT %d%%", p.output)
	}
	trackInfo := fmt.Sprintf("TRACK %d/%d  ·  %s", p.playingIndex+1, len(p.active.tracks), format(t.path))
	if t.radioFormat != "" {
		trackInfo = "ON AIR  " + t.radioFormat
	} else if p.likes.contains(t.likeID) {
		trackInfo += "  ·  ♥"
	}
	if p.notice != "" {
		trackInfo = p.notice
	}
	// Why Melisma chose this song: up to two reasons.
	reasons := []string{l()}
	if len(p.why) > 0 {
		reasons = nil
		for _, why := range p.why[:min(2, len(p.why))] {
			reasons = append(reasons, l(seg{"↳ ", accent}, seg{why, dim}))
		}
	} else if p.handover && p.melisma && p.engine != nil {
		reasons = []string{l(seg{"↳ ", accent}, seg{"Melisma takes over after this song", dim})}
	} else if p.unknown(t) {
		reasons = []string{l(seg{"· ", muted}, seg{"Melisma hasn't heard this yet: /analyze", muted})}
	}
	album := l(seg{t.album, dim})
	if p.melisma && p.tripDestination != "" && !hidden {
		album = l(seg{tripRoute(p.tripLeg), dim})
	}
	info := []string{l(seg{trackTitle(t, p.playingIndex+1), accent + bold}), l(seg{t.artist, amber}), album}
	if hidden {
		info = []string{l(seg{"Mystery track", accent + bold}), l(seg{"R to reveal", dim}), l()}
		reasons = []string{l()}
		trackInfo = "Blind listening"
	}
	info = append(info, reasons...)
	if size-len(info) >= 9 {
		info = append(info, l()) // room to breathe between the song and the transport
	}
	progressAt := len(info)
	info = append(info,
		l(raw(progressBar(p.state.Elapsed, p.state.Duration, infoWidth-2))),
		l(seg{state + "  " + clock(p.state.Elapsed) + " / " + clock(p.state.Duration), accent + bold}),
		l(seg{volume + "  ", dim}, raw(meter(p.volume, 10))),
		l(seg{trackInfo, muted}))
	if p.live != nil {
		info[progressAt] = l(seg{strings.Repeat(activeTheme.box.h, infoWidth-2), border})
		info[progressAt+1] = l(seg{p.liveStatus(), accent + bold}, seg{"  /  " + p.live.station.provider, dim})
		if p.notice == "" {
			info[progressAt+3] = l(seg{"Live broadcast", muted})
		}
	}
	titleRow := len(info)
	featureStart := titleRow + 1
	if showLyrics && size-featureStart >= 4 {
		titleRow++
		featureStart += 2
	}
	chartHeight := max(1, size-featureStart)
	var feature []string
	featureTitle := activeTheme.box.h + " " + activeTheme.visualTitle
	if showLyrics {
		featureTitle = activeTheme.box.h + " SYNCED LYRICS"
		feature = lyricRows(p.lyrics, p.lyricsStatus, p.state.Elapsed, infoWidth, chartHeight)
	} else if (p.visual || hidden) && p.analysisAvailable {
		feature = spectrumRows(p.levels, infoWidth, chartHeight, p.phase)
	}
	heading := " " + activeTheme.overlayTitle + " "
	if hidden {
		heading = " BLIND LISTENING "
	} else if p.inJam() && p.social.jam.path == t.path {
		heading = " JAM · " + p.social.reply.Jam.Host.Name + " + " + p.social.reply.Jam.Guest.Name + " "
	} else if p.melisma && p.tripDestination != "" && p.live == nil && t.radioFormat == "" {
		heading = " TRIP · " + strings.ToUpper(p.tripDestination) + " "
	} else if p.live != nil {
		heading = " " + strings.ToUpper(p.live.station.name) + " "
	} else if p.melisma && p.engine != nil {
		heading = " " + strings.ToUpper(p.melismaHeading()) + " "
	} else if t.radioFormat != "" {
		heading = " CHIPTUNE RADIO "
	}
	place := func(line string) {
		rows[y] += fmt.Sprintf("\x1b[%dG", x) + panel + amber + line + reset
		y++
	}
	blank := boxSide(strings.Repeat(" ", columns-2))
	place(boxTop(activeTheme.box.h+heading, columns))
	if padding > 0 {
		place(blank)
	}
	source := t.source
	if !hidden && (source.artworkSize != size || source.artworkTheme != activeTheme) {
		source.artworkSize, source.artworkTheme = size, activeTheme
		source.artworkRows = make([]string, size)
		art := source.artwork(t)
		for i := range size {
			source.artworkRows[i] = artworkRow(art, i, size)
		}
	}
	nativeCover := !hidden && p.kittyGraphics && p.social.panel == nil && p.finder == nil && p.playlistDialog == nil && p.radioPicker == nil && p.queueView == nil && p.themePicker == nil && p.coverPrompt == nil && source.cover != nil && len(source.cover.PNG) > 0
	if nativeCover {
		p.coverPlacement = kitty.Placement{PNG: source.cover.PNG, Row: y + 1, Column: x + 2, Columns: size * 2, Rows: size}
	}
	emptyArt := strings.Repeat(" ", size*2)
	for i := range size {
		text := ""
		switch {
		case len(feature) == 1 && i == 3:
			text = feature[0]
		case len(feature) > 1 && i == titleRow:
			text = l(seg{featureTitle, muted})
		case len(feature) > 1 && i >= featureStart:
			text = feature[i-featureStart]
		case i < len(info):
			text = info[i]
		default:
			text = l()
		}
		art := emptyArt
		if hidden {
			art = artworkRow(nil, i, size)
		} else if !nativeCover {
			art = source.artworkRows[i]
		}
		place(boxSide(" " + art + "  " + text + base + " "))
	}
	if padding > 0 {
		place(boxDivider(columns))
	}
	visual := "off"
	if p.visual {
		visual = "on"
	}
	like := "Like"
	if !hidden && p.likes.contains(t.likeID) {
		like = "Unlike"
	}
	controls := []string{"Space", "Pause", "</>", "Skip", "←/→", "10s", "*", like}
	if p.live != nil {
		controls = []string{"Space", "Pause/Resume", "/radio", "Stations"}
	} else if t.radioFormat != "" {
		controls = []string{"Space", "Pause", "</>", "Track", "←/→", "10s", "/radio", "Stations"}
	}
	if p.melisma && p.engine != nil && p.live == nil && t.radioFormat == "" {
		controls = append(controls, "F", "Further", "B", "Back", "S", "Surprise", "D", "Never")
	}
	place(boxSide(keyHints(columns-2, base, controls...)))
	lyricsHint := "Lyrics"
	if p.lyricsMode {
		lyricsHint = "Back"
	}
	footer := []string{"Esc", "Library", "L", lyricsHint, "V", "Visuals " + visual}
	if p.live != nil {
		footer = []string{"Esc", "Library"}
	}
	footer = append(footer, "+/-", "Vol")
	if hidden {
		footer = []string{"Esc", "Library", "R", "Reveal song", "+/-", "Vol"}
	}
	if !hidden && source.cover == nil && source.artist != "" {
		footer = append(footer, "C", "Cover")
	}
	place(boxSide(keyHints(columns-2, base, footer...)))
	place(boxBottom(columns))
}

func lyricRows(lines []lyrics.Line, status string, elapsed float64, width, height int) []string {
	rows := make([]string, height)
	for i := range rows {
		rows[i] = cell("", width)
	}
	if len(lines) == 0 {
		rows[0] = cell(status, width)
		return rows
	}

	current := lyrics.Current(lines, elapsed)
	for row := range rows {
		index := current + row
		if height >= 3 {
			index--
		}
		if index < 0 || index >= len(lines) || lines[index].Text == "" {
			continue
		}
		switch {
		case index == current:
			rows[row] = accent + "\x1b[1m" + cell("› "+lines[index].Text, width) + unbold + amber
		case index == current+1:
			rows[row] = amber + cell("  "+lines[index].Text, width)
		default:
			rows[row] = muted + cell("  "+lines[index].Text, width) + amber
		}
	}
	if current < 0 && height == 1 {
		rows[0] = dim + cell("  "+lines[0].Text, width) + amber
	}
	return rows
}

func spectrumRows(levels [audio.BandCount]float32, width, height, phase int) []string {
	th := activeTheme
	glyphs := []rune(th.rainGlyphs)
	blocks := []rune(" ▁▂▃▄▅▆▇█")
	count := min(len(levels), max(1, width/2))
	steps := make([]int, count)
	for band := range count {
		var level float32
		for _, sample := range levels[band*len(levels)/count : (band+1)*len(levels)/count] {
			level = max(level, sample)
		}
		steps[band] = int(level*float32(height*8) + 0.5)
	}
	rows := make([]string, height)
	for row := range height {
		var line strings.Builder
		lastColor := -1
		for x := range width {
			band := x * count / width
			fill := min(8, max(0, steps[band]-(height-row-1)*8))
			period := height + 9 + x%5
			head := (phase/(2+x%3) + x*13) % period
			trail := (head - row + period) % period
			glyph, color := ' ', th.rain
			if fill > 0 && (x == 0 || band != (x-1)*count/width) {
				glyph, color = blocks[fill], th.bar
				if steps[band] <= (height-row)*8 {
					color = th.peak
				}
			} else if trail < 5 {
				glyph = glyphs[(x*7+row*3+head)%len(glyphs)]
				if trail == 0 {
					color = th.head
				}
			}
			if glyph != ' ' && color != lastColor {
				fmt.Fprintf(&line, "\x1b[38;5;%dm", color)
				lastColor = color
			}
			line.WriteRune(glyph)
		}
		rows[row] = line.String() + amber
	}
	return rows
}
