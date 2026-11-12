package app

import (
	"elsewhere/internal/audio"
	"elsewhere/internal/kitty"
	"elsewhere/internal/lyrics"
	"fmt"
	"path/filepath"
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

func progress(elapsed, duration float64, width int) string {
	filled := 0
	if duration > 0 {
		filled = int(float64(width) * min(1, max(0, elapsed/duration)))
	}
	return strings.Repeat(activeTheme.barDone, filled) + strings.Repeat(activeTheme.barTodo, width-filled)
}

func (p *player) frame(width, height int) []string {
	p.coverPlacement = kitty.Placement{}
	row := func(text, style string) string { return activeTheme.screen + style + cell(text, width) + reset }
	radioShortcut := ""
	if width >= 50 {
		radioShortcut = "  /radio"
	}
	if width >= 62 && p.engine != nil {
		radioShortcut += "  /melisma"
	}
	if width < 45 || height < 13 {
		return []string{row(" Enlarge terminal (45 x 13). :q quits", amber)}
	}
	th := activeTheme
	state := th.stop
	playing := " Choose an album or track and press Enter"
	current := p.current()
	hidden := p.hiddenTrack()
	if current != nil {
		state = th.pause
		playing = fmt.Sprintf(" %s  /  %s  /  %s", current.title, current.artist, current.album)
	}
	if hidden {
		playing = " Mystery track · R to reveal"
	}
	if p.state.Playing {
		state = th.play
	}
	elapsed, duration := p.state.Elapsed, p.state.Duration
	bar := progress(elapsed, duration, max(8, min(42, width-35)))
	transport := fmt.Sprintf(" %s  %s %s %s   VOL %d%%", state, clock(elapsed), bar, clock(duration), p.volume)
	if p.live != nil {
		transport = fmt.Sprintf(" %s  %s  VOL %d%%", p.liveStatus(), p.live.station.name, p.volume)
	}
	leftWidth := max(15, width*34/100)
	rightWidth := width - leftWidth - 1
	bodyHeight := height - 9
	tracks := p.tracks()
	albumStart := max(0, min(p.albumIndex-bodyHeight/2, len(p.results)-bodyHeight))
	trackStart := max(0, min(p.trackIndex-bodyHeight/2, len(tracks)-bodyHeight))
	leftTitle, rightTitle := " › ALBUMS", "  TRACKS"
	if p.trackPane {
		leftTitle, rightTitle = "  ALBUMS", " › TRACKS"
	}
	title := th.title
	if status := p.engineStatus(); width >= 70 && status != "off" {
		title += "   ·   MELISMA " + status
	}
	rows := []string{
		row(cell(title, width-textWidth(th.titleRight))+th.titleRight, selected),
		th.screen + p.ruleRow(width),
		row(playing, amber),
		row(transport, amber),
		th.screen + dim + cell(leftTitle, leftWidth) + th.box.v + cell(rightTitle, rightWidth) + reset,
	}
	for line := range bodyHeight {
		a, t := albumStart+line, trackStart+line
		left, right, leftStyle, rightStyle := "", "", dim, amber
		if a < len(p.results) {
			album := p.results[a]
			left = " " + album.title
			if album.likedSongs {
				left = fmt.Sprintf(" ♥ %s (%d)", album.title, len(album.tracks))
			} else if album.isPlaylist {
				left = fmt.Sprintf(" ≡ %s (%d)", album.title, len(album.tracks))
			}
			if album.artist != "" {
				left += " / " + album.artist
			}
			if a == p.albumIndex {
				leftStyle = amber
				if !p.trackPane {
					leftStyle = selected
				}
			}
		}
		if t < len(tracks) {
			marker := " "
			if !hidden && current != nil && current.path == tracks[t].path {
				marker = "▶"
			}
			liked := " "
			if p.likes.contains(tracks[t].likeID) {
				liked = "♥"
			} else if p.unknown(tracks[t]) {
				liked = "·" // Melisma hasn't analyzed it yet
			}
			right = fmt.Sprintf(" %s%s %02d  %s", marker, liked, t+1, tracks[t].title)
			if t == p.trackIndex && p.trackPane {
				rightStyle = selected
			}
		}
		rows = append(rows, th.screen+leftStyle+cell(left, leftWidth)+black+dim+th.box.v+rightStyle+cell(right, rightWidth)+reset)
	}
	detail := " " + p.path
	if len(tracks) > 0 {
		t := tracks[p.trackIndex]
		detail = " " + t.artist + "  /  " + format(t.path)
		if p.unknown(t) {
			detail += "  ·  Melisma hasn't heard this yet: /analyze"
		}
	}
	if len(p.results) > 0 {
		entry := p.results[p.albumIndex]
		switch {
		case entry.isPlaylist && entry.description != "":
			detail = " " + entry.description
		case entry.isPlaylist && len(tracks) == 0:
			detail = " Select a song and use /ap to add it here"
		case entry.likedSongs && len(tracks) == 0:
			detail = " Press * on a track to add it here"
		}
	}
	if p.notice != "" {
		detail = " " + p.notice
	}
	albumCount := 0
	for _, a := range p.results {
		if !a.likedSongs && !a.isPlaylist {
			albumCount++
		}
	}
	status := fmt.Sprintf(" %d albums  ·  %s", albumCount, p.path)
	if p.searching || p.query != "" {
		query := p.query
		if p.searching {
			query = p.searchPrompt.input + "_"
		}
		status = fmt.Sprintf(" / %s  ·  %d albums", query, albumCount)
	}
	if p.command != nil {
		status = " :" + p.command.input + "_"
	}
	rows = append(rows, row(detail, dim), row(status, amber),
		row(" ↑↓ Move  Tab Pane  Enter Play  Space Pause  / Find", selected),
		row(" </> Skip  * Like  M Player  Q Queue  :q Quit"+radioShortcut, selected))
	if p.popup && current != nil {
		if hidden {
			for i := 4; i < len(rows)-2; i++ {
				rows[i] = row("", dim)
			}
		}
		p.overlay(rows, width, *current, state)
		if p.searching {
			y := height - 3
			rows[y] += "\x1b[1G" + row(" /"+p.searchPrompt.input+"_", selected)
		}
		if p.command != nil {
			rows[height-3] += "\x1b[1G" + row(" :"+p.command.input+"_", selected)
		}
	}
	if p.searching || p.command != nil {
		rows[len(rows)-2] = row(" Tab/↑↓ Choose a command  Enter Run  Esc Cancel", selected)
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

var placeholder = []string{
	"                ", "      ████      ", "    ██░░░░██    ", "   ██░█▓▓█░██   ",
	"   ██░█▓▓█░██   ", "    ██░░░░██    ", "      ████      ", "                ",
}

func artworkRow(cover *audio.Cover, row, size int) string {
	var line strings.Builder
	if cover == nil {
		pixels := []rune(placeholder[row*len(placeholder)/size])
		for x := range size * 2 {
			line.WriteRune(pixels[x*len(pixels)/(size*2)])
		}
		return line.String()
	}
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
	volume := fmt.Sprintf("APP %d%%", p.volume)
	if p.output >= 0 {
		volume += fmt.Sprintf("  OUT %d%%", p.output)
	}
	trackInfo := fmt.Sprintf("TRACK %d/%d  %s", p.playingIndex+1, len(p.active.tracks), format(t.path))
	if t.radioFormat != "" {
		trackInfo = "ON AIR  " + t.radioFormat
	} else if p.likes.contains(t.likeID) {
		trackInfo += "  ♥"
	}
	if p.notice != "" {
		trackInfo = p.notice
	}
	// Why Melisma chose this song: up to two reasons.
	reasons := []string{""}
	if len(p.why) > 0 {
		reasons = nil
		for _, why := range p.why[:min(2, len(p.why))] {
			reasons = append(reasons, "↳ "+why)
		}
	} else if p.handover && p.melisma && p.engine != nil {
		reasons = []string{"↳ Melisma takes over after this song"}
	} else if p.unknown(t) {
		reasons = []string{"· Melisma hasn't heard this yet: /analyze"}
	}
	info := []string{t.title, t.artist, t.album}
	if hidden {
		info = []string{"Mystery track", "R to reveal", ""}
		reasons = []string{""}
		trackInfo = "Blind listening"
	} else if p.melisma && p.tripDestination != "" {
		info[2] = tripRoute(p.tripLeg)
	}
	info = append(info, reasons...)
	info = append(info,
		progress(p.state.Elapsed, p.state.Duration, infoWidth-2),
		state+"  "+clock(p.state.Elapsed)+" / "+clock(p.state.Duration),
		volume, trackInfo)
	if p.live != nil {
		info[4] = strings.Repeat(activeTheme.box.h, infoWidth-2)
		info[5] = p.liveStatus() + "  /  " + p.live.station.provider
		if p.notice == "" {
			info[7] = "Live broadcast"
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
	heading := activeTheme.box.h + " " + activeTheme.overlayTitle + " "
	if hidden {
		heading = activeTheme.box.h + " BLIND LISTENING "
	} else if p.inJam() && p.social.jam.path == t.path {
		heading = activeTheme.box.h + " JAM · " + p.social.reply.Jam.Host.Name + " + " + p.social.reply.Jam.Guest.Name + " "
	} else if p.melisma && p.tripDestination != "" && p.live == nil && t.radioFormat == "" {
		heading = activeTheme.box.h + " TRIP · " + strings.ToUpper(p.tripDestination) + " "
	} else if p.live != nil {
		heading = activeTheme.box.h + " " + strings.ToUpper(p.live.station.name) + " "
	} else if p.melisma && p.engine != nil {
		heading = activeTheme.box.h + " " + strings.ToUpper(p.melismaHeading()) + " "
	} else if t.radioFormat != "" {
		heading = activeTheme.box.h + " CHIPTUNE RADIO "
	}
	place := func(line string) {
		rows[y] += fmt.Sprintf("\x1b[%dG", x) + panel + amber + line + reset
		y++
	}
	blank := boxSide(strings.Repeat(" ", columns-2))
	place(boxTop(heading, columns))
	if padding > 0 {
		place(blank)
	}
	source := t.source
	if !hidden && source.artworkSize != size {
		source.artworkSize = size
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
			text = cell(featureTitle, infoWidth)
		case len(feature) > 1 && i >= featureStart:
			text = feature[i-featureStart]
		case i < len(info):
			text = cell(info[i], infoWidth)
		default:
			text = cell("", infoWidth)
		}
		art := emptyArt
		if hidden {
			art = artworkRow(nil, i, size)
		} else if !nativeCover {
			art = source.artworkRows[i]
		}
		place(boxSide(" " + art + "  " + text + " "))
	}
	if padding > 0 {
		place(blank)
	}
	visual := "off"
	if p.visual {
		visual = "on"
	}
	controls := " Space Pause  </> Skip  ←/→ 10s  * Like"
	if p.live != nil {
		controls = " Space Pause/Resume  /radio Stations"
	} else if t.radioFormat != "" {
		controls = " Space Pause  </> Track  ←/→ 10s  /radio Stations"
	} else if !hidden && p.likes.contains(t.likeID) {
		controls = " Space Pause  </> Skip  ←/→ 10s  * Unlike"
	}
	if p.melisma && p.engine != nil && p.live == nil && t.radioFormat == "" {
		controls += "  F Further  B Back  S Surprise  D Never"
	}
	place(boxSide(cell(controls, columns-2)))
	lyricsHint := "L Lyrics"
	if p.lyricsMode {
		lyricsHint = "L Back"
	}
	footer := " Esc Library " + lyricsHint + " V Visuals " + visual
	if p.live != nil {
		footer = " Esc Library  +/- Vol"
	}
	if columns >= 49 && p.live == nil {
		footer += "  +/- Vol"
	}
	if hidden {
		footer = " Esc Library  R Reveal song  +/- Vol"
	}
	if !hidden && columns >= 60 && source.cover == nil && source.artist != "" {
		footer += "  C Cover"
	}
	place(boxSide(cell(footer, columns-2)))
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
		if index == current {
			rows[row] = "\x1b[1m" + cell("› "+lines[index].Text, width) + "\x1b[22m"
		} else {
			rows[row] = dim + cell("  "+lines[index].Text, width) + amber
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
