package main

import (
	"fmt"
)

func (p *player) overlayPlaylist(rows []string, width int) {
	d := p.playlistDialog
	columns := min(72, width-4)
	heading := " NEW PLAYLIST "
	footer := " Tab Field  Enter Create  Esc Cancel"
	var body []string
	row := func(text, style string) string {
		return boxSide(style + cell(text, columns-2) + panel + amber)
	}
	if target := d.deleteTarget; target != nil {
		heading = " DELETE PLAYLIST "
		footer = " Enter Delete  Esc Back"
		body = append(body, row(" Delete "+target.Name+"?", amber), row(" The music files stay in your library.", dim))
	} else if d.kind == "np" {
		if d.tapeCard != "" {
			heading = " MIXTAPE "
			footer = " Tab Field  Enter Save tape  Esc Cancel"
			body = append(body, row(" [◉ ═════ ◉]  "+d.tapeCard, amber))
		}
		nameStyle, descriptionStyle := selected, dim
		name, description := d.name, d.description
		if d.descriptionFocused {
			nameStyle, descriptionStyle = dim, selected
			description += "_"
		} else {
			name += "_"
		}
		body = append(body, row("", amber),
			row(" Name                  "+name, nameStyle),
			row(" Description (optional) "+description, descriptionStyle),
			row("", amber))
	} else {
		heading = " ADD TO PLAYLIST "
		footer = " ↑↓ Move  Enter Add  Esc Cancel"
		song := " Select or play a song first"
		if d.target != nil {
			song = " " + d.target.title + " / " + d.target.artist
		}
		if d.kind == "dp" {
			heading = " DELETE PLAYLIST "
			footer = " ↑↓ Move  Enter Select  Esc Cancel"
		} else {
			body = append(body, row(song, dim))
		}
		body = append(body, row(" > "+d.filter+"_", selected))
		matches := p.matchPlaylists(d.filter)
		if len(matches) == 0 {
			hint := " No matching playlists"
			if len(p.playlistRecords) == 0 {
				hint = " No playlists yet. Create one with /np."
			}
			body = append(body, row(hint, dim))
		}
		limit := min(8, len(rows)-7)
		start := max(0, min(d.index-limit/2, len(matches)-limit))
		for i := start; i < min(len(matches), start+limit); i++ {
			list := matches[i]
			text := "   " + list.Name
			style := dim
			if i == d.index {
				text = " › " + list.Name
				style = amber
			}
			if list.Description != "" {
				text += " / " + list.Description
			}
			body = append(body, row(text, style))
		}
	}
	if d.errorText != "" {
		body = append(body, row(" "+d.errorText, amber))
	}
	dialog := append([]string{boxTop(heading, columns)}, body...)
	dialog = append(dialog, row(footer, dim), boxBottom(columns))
	x, y := (width-columns)/2+1, (len(rows)-len(dialog))/2
	cursor := fmt.Sprintf("\x1b[%dG", x)
	for i, line := range dialog {
		if y+i >= 0 && y+i < len(rows) {
			rows[y+i] += cursor + panel + amber + line + reset
		}
	}
}
