package main

import (
	"fmt"
	"sort"
	"strings"
)

// paletteCommand is one entry in the fuzzy command list shown above the prompt.
type paletteCommand struct{ name, description, alias string }

var slashCommands = []paletteCommand{
	{"np", "New playlist", ""},
	{"ap", "Add the selected or playing song to a playlist", ""},
	{"radio", "Pick a radio station", ""},
	{"melisma", "Let Melisma choose what plays next", "engine"},
	{"drift", "Melisma: wander gradually from here", ""},
	{"usual", "Melisma: stay with what you play most", ""},
	{"new", "Melisma: head for music you've never played", ""},
	{"toward", "Melisma: travel toward an artist, album or song (/toward name)", "to"},
	{"vibe", "Melisma: travel toward a described sound (/vibe rainy night)", ""},
	{"friend", "Melisma: into a friend's taste (/friend name)", ""},
	{"further", "Melisma: wander further out", ""},
	{"back", "Melisma: come back toward home", ""},
	{"surprise", "Melisma: a surprise next", ""},
	{"never", "Melisma: never play this again", "dislike"},
	{"taste", "What Melisma has learned about you", "profile"},
	{"analyze", "Let Melisma listen to new songs in this library", ""},
	{"q", "Show the queue", "queue"},
	{"themes", "Pick a color theme", "theme"},
}

var colonCommands = []paletteCommand{
	{"q", "Quit Elsewhere", "quit"},
	{"themes", "Pick a color theme", "theme"},
	{"radio", "Pick a radio station", ""},
	{"melisma", "Let Melisma choose what plays next", "engine"},
	{"taste", "What Melisma has learned about you", "profile"},
	{"analyze", "Let Melisma listen to new songs in this library", ""},
	{"queue", "Show the queue", ""},
}

// matchCommands filters a command list with the text typed after the "/" or ":" prefix.
// Slash prompts double as music search, so only a single word that fuzzy-matches a command
// name produces suggestions; colon prompts also match descriptions.
func matchCommands(commands []paletteCommand, input string, describe bool) []paletteCommand {
	words := strings.Fields(strings.ToLower(input))
	if len(words) > 1 || (len(words) == 1 && !describe && strings.ContainsAny(words[0], "/\\")) {
		return nil
	}
	type match struct {
		command paletteCommand
		cost    int
	}
	var matches []match
	for _, c := range commands {
		cost := 0
		if len(words) == 1 {
			text := c.name + " " + c.alias
			if describe {
				text += " " + strings.ToLower(c.description)
			}
			if cost = fuzzyCost(text, words[0]); cost < 0 {
				continue
			}
			// Prefer names that start with what was typed.
			if !strings.HasPrefix(c.name, words[0]) && !strings.HasPrefix(c.alias, words[0]) {
				cost += 100
			}
		}
		matches = append(matches, match{c, cost})
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].cost < matches[j].cost })
	result := make([]paletteCommand, len(matches))
	for i, m := range matches {
		result[i] = m.command
	}
	return result
}

// palette returns the suggestions for whichever prompt is open.
func (p *player) palette() []paletteCommand {
	switch {
	case p.command != nil:
		return matchCommands(colonCommands, p.command.input, true)
	case p.searching:
		return matchCommands(slashCommands, p.searchPrompt.input, false)
	}
	return nil
}

// movePalette handles Up, Down, Tab and Shift-Tab. It reports whether the key was used.
func (p *player) movePalette(key string) bool {
	count := len(p.palette())
	switch key {
	case "\x1b[B":
		p.paletteIndex = min(count-1, p.paletteIndex+1)
	case "\x1b[A":
		p.paletteIndex = max(-1, min(count-1, p.paletteIndex-1))
	case "\t":
		if count > 0 {
			p.paletteIndex = (p.paletteIndex + 1) % count
		}
	case "\x1b[Z":
		if count > 0 {
			if p.paletteIndex--; p.paletteIndex < 0 {
				p.paletteIndex = count - 1
			}
		}
	default:
		return false
	}
	return true
}

// selectedCommand is the highlighted suggestion, or "" when the typed text should be used.
func (p *player) selectedCommand() string {
	items := p.palette()
	if p.paletteIndex >= 0 && p.paletteIndex < len(items) {
		return items[p.paletteIndex].name
	}
	return ""
}

// runCommand opens the screen behind a slash command. prompt is the search state to restore.
func (p *player) runSlashCommand(command string, prompt searchPrompt) bool {
	// Commands that take words: /toward the beatles, /vibe rainy night, /friend bea.
	words := strings.Fields(command)
	if len(words) > 0 {
		switch words[0] {
		case "toward", "to", "vibe", "friend":
			rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(command, words[0]), " "))
			p.query = prompt.previousQuery
			p.search()
			p.albumIndex, p.trackIndex = prompt.albumIndex, prompt.trackIndex
			if rest == "" {
				p.notice = "Type /" + words[0] + " and what to head for, e.g. /" + words[0] + map[string]string{
					"toward": " the beatles", "to": " the beatles", "vibe": " rainy night drive", "friend": " bea"}[words[0]]
				return true
			}
			switch words[0] {
			case "toward", "to":
				p.setMelismaMode(map[string]any{"to": rest}, "heading toward "+rest)
			case "vibe":
				p.setMelismaMode(map[string]any{"vibe": rest}, "heading for \""+rest+"\"")
			case "friend":
				p.setMelismaMode(map[string]any{"friend": rest}, "into "+rest+"'s taste")
			}
			return true
		}
	}
	switch command {
	case "np", "ap", "radio", "q", "queue", "themes", "theme":
	case "melisma", "engine", "further", "back", "surprise", "never", "dislike":
	case "drift", "usual", "new", "taste", "profile", "analyze":
	default:
		return false
	}
	p.query = prompt.previousQuery
	p.search()
	p.albumIndex, p.trackIndex = prompt.albumIndex, prompt.trackIndex
	switch command {
	case "melisma", "engine":
		p.startMelisma()
	case "further", "back", "surprise":
		p.steerMelisma(command)
	case "never", "dislike":
		p.steerMelisma("dislike")
	case "drift", "usual", "new":
		what := map[string]string{"drift": "wandering from here", "usual": "staying with what you play most",
			"new": "heading for music you've never played"}[command]
		p.setMelismaMode(map[string]any{"mode": command}, what)
	case "taste", "profile":
		p.openTaste()
	case "analyze":
		p.startAnalysis()
	case "radio":
		p.radioPicker = &radioPicker{}
	case "q", "queue":
		p.openQueue()
	case "themes", "theme":
		p.themePicker = &themePicker{index: themeIndex(activeTheme), original: activeTheme}
	default:
		p.playlistDialog = &playlistDialog{kind: command, target: prompt.track}
	}
	return true
}

func (p *player) overlayPalette(rows []string, width int) {
	items := p.palette()
	if len(items) == 0 {
		return
	}
	columns := min(width, 62)
	visible := min(len(items), 8, len(rows)-8)
	if visible < 1 {
		return
	}
	first := max(0, min(p.paletteIndex-visible/2, len(items)-visible))
	label := " COMMANDS "
	line := func(text, style string) string {
		return boxSide(style + cell(text, columns-2) + panel + amber)
	}
	lines := []string{boxTop(label, columns)}
	for i := first; i < first+visible; i++ {
		text, style := fmt.Sprintf("   %-8s %s", items[i].name, items[i].description), dim
		if i == p.paletteIndex {
			text, style = fmt.Sprintf(" › %-8s %s", items[i].name, items[i].description), selected
		}
		lines = append(lines, line(text, style))
	}
	lines = append(lines, boxBottom(columns))
	// The list sits directly above the prompt row, like an fzf window growing upward.
	top := len(rows) - 3 - len(lines)
	for i, text := range lines {
		rows[top+i] += "\x1b[1G" + panel + amber + text + reset
	}
}
