package app

import (
	"fmt"
	"sort"
	"strings"
)

type radioPicker struct {
	filter string
	index  int
}

func (p *player) matchingStations() []radioStation {
	filter := strings.ToLower(strings.TrimSpace(p.radioPicker.filter))
	var options []radioStation
	for _, station := range radioStations {
		// Chiptune Radio needs Game Music Emu, which this build can't load.
		if station.stream == "" && !chiptuneSupported {
			continue
		}
		options = append(options, station)
	}
	if p.radio != nil || p.live != nil {
		options = append(options, stopRadioStation)
	}
	if filter == "" {
		return options
	}
	type match struct {
		station radioStation
		cost    int
	}
	var matches []match
	for _, station := range options {
		text := strings.ToLower(station.name + " " + station.description + " " + station.provider)
		cost := 0
		for _, word := range strings.Fields(filter) {
			part := fuzzyCost(text, word)
			if part < 0 {
				cost = -1
				break
			}
			cost += part
		}
		if cost >= 0 {
			matches = append(matches, match{station, cost})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].cost < matches[j].cost })
	result := make([]radioStation, len(matches))
	for i, m := range matches {
		result[i] = m.station
	}
	return result
}

func (p *player) handleRadioPicker(key string) {
	picker := p.radioPicker
	switch key {
	case "\x1b":
		p.radioPicker = nil
	case "\x1b[A":
		picker.index = max(0, picker.index-1)
	case "\x1b[B":
		picker.index = min(max(0, len(p.matchingStations())-1), picker.index+1)
	case "\r", "\n":
		stations := p.matchingStations()
		if picker.index < len(stations) {
			p.radioPicker = nil
			p.tuneStation(stations[picker.index])
		}
	default:
		if editText(&picker.filter, key, 120) {
			picker.index = 0
		}
	}
}

func (p *player) overlayRadioPicker(rows []string, width int) {
	columns := min(75, width-4)
	const heading = " RADIO STATIONS "
	row := func(text, style string) string {
		return boxSide(style + cell(text, columns-2) + panel + amber)
	}
	dialog := []string{boxTop(heading, columns),
		row(" > "+p.radioPicker.filter+"_", selected)}
	stations := p.matchingStations()
	if len(stations) == 0 {
		dialog = append(dialog, row(" No matching stations", dim))
	}
	for i, station := range stations {
		text, style := "   ", dim
		if i == p.radioPicker.index {
			text, style = " › ", amber
		}
		text += station.name + " / " + station.description
		dialog = append(dialog, row(text, style))
	}
	dialog = append(dialog, row(" ↑↓ Move  Enter Tune in  Esc Cancel", dim),
		boxBottom(columns))
	x, y := (width-columns)/2+1, (len(rows)-len(dialog))/2
	cursor := fmt.Sprintf("\x1b[%dG", x)
	for i, line := range dialog {
		rows[y+i] += cursor + panel + amber + line + reset
	}
}
