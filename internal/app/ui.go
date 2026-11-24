package app

import (
	"regexp"
	"strconv"
	"strings"
)

// seg is a run of text drawn in one style. line lays runs out to an exact width.
type seg struct{ text, style string }

// styled is the style of a seg whose text already carries its own escape codes.
const styled = "\x00"

// raw wraps already-styled text so line can place it without escaping it.
func raw(text string) seg { return seg{text, styled} }

var escapeCode = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// plainText drops escape codes, leaving what the terminal shows.
func plainText(s string) string { return escapeCode.ReplaceAllString(s, "") }

func segWidth(p seg) int {
	if p.style == styled {
		return textWidth(plainText(p.text))
	}
	return textWidth(p.text)
}

// fit shortens text to columns, ending in an ellipsis when something was cut.
func fit(text string, columns int) string {
	if columns <= 0 {
		return ""
	}
	if textWidth(text) <= columns {
		return text
	}
	return strings.TrimRight(cell(text, columns-1), " ") + "…"
}

// line draws the runs left to right, cutting the last one that doesn't fit, and pads with base
// so the visible width is exactly columns. Every run hands back to base afterwards.
func line(columns int, base string, parts ...seg) string {
	var out strings.Builder
	out.WriteString(base)
	left := columns
	for _, p := range parts {
		if left <= 0 {
			break
		}
		if p.style == styled {
			if w := segWidth(p); w <= left {
				out.WriteString(p.text + unbold + base)
				left -= w
			}
			continue
		}
		text := fit(p.text, left)
		left -= textWidth(text)
		out.WriteString(p.style + cell(text, textWidth(text)) + unbold + base)
	}
	out.WriteString(strings.Repeat(" ", max(0, left)))
	return out.String()
}

// spread puts the left runs at the start and the right runs flush against the end, dropping the
// right side when the two would touch.
func spread(columns int, base string, left, right []seg) string {
	width := func(parts []seg) int {
		n := 0
		for _, p := range parts {
			n += segWidth(p)
		}
		return n
	}
	need, have := width(left), width(right)
	if have == 0 || need+have+2 > columns {
		return line(columns, base, left...)
	}
	padding := seg{strings.Repeat(" ", columns-need-have), base}
	return line(columns, base, append(append(append([]seg{}, left...), padding), right...)...)
}

// keyHints renders "key label" pairs with the key in the accent and the label dimmed, keeping as
// many pairs as fit.
func keyHints(columns int, base string, pairs ...string) string {
	var parts []seg
	used := 0
	for i := 0; i+1 < len(pairs); i += 2 {
		key, label := pairs[i], pairs[i+1]
		w := 1 + textWidth(key) + 1 + textWidth(label) + 1
		if used+w > columns {
			break
		}
		parts = append(parts, seg{" " + key, accent + bold}, seg{" " + label + " ", dim})
		used += w
	}
	return line(columns, base, parts...)
}

// progressBar fills the played part in the accent and the rest in the border color, with a knob
// on themes whose bar is a thin line.
func progressBar(elapsed, duration float64, columns int) string {
	if columns <= 0 {
		return ""
	}
	th := activeTheme
	filled := 0
	if duration > 0 {
		filled = int(float64(columns) * min(1, max(0, elapsed/duration)))
	}
	knob := th.barDone == "━" && duration > 0
	done := filled
	if knob && done == columns {
		done--
	}
	var out strings.Builder
	out.WriteString(accent + strings.Repeat(th.barDone, done))
	todo := columns - done
	if knob {
		out.WriteString(bold + "●" + unbold)
		todo--
	}
	out.WriteString(border + strings.Repeat(th.barTodo, max(0, todo)) + amber)
	return out.String()
}

// meter is a small level gauge, used for volume.
func meter(percent, columns int) string {
	filled := (max(0, min(100, percent))*columns + 50) / 100
	return accent + strings.Repeat("▮", filled) + border + strings.Repeat("▯", columns-filled) + amber
}

var colorNumber = regexp.MustCompile(`[34]8;5;(\d+)`)

// colorIndex reads the 256-color index out of a style, or fallback when it has none.
func colorIndex(style string, fallback int) int {
	if m := colorNumber.FindAllStringSubmatch(style, -1); len(m) > 0 {
		if n, err := strconv.Atoi(m[len(m)-1][1]); err == nil {
			return n
		}
	}
	return fallback
}

// dialogFooter turns a hint line such as " ↑↓ Move  Enter Add  Esc Cancel" (pairs separated by
// two spaces or " · ") into a styled box row, keys in the accent and labels dimmed.
func dialogFooter(text string, columns int) string {
	text = strings.ReplaceAll(strings.TrimSpace(text), " · ", "  ")
	var pairs []string
	for _, chunk := range strings.Split(text, "  ") {
		if chunk = strings.TrimSpace(chunk); chunk == "" {
			continue
		}
		key, label, _ := strings.Cut(chunk, " ")
		pairs = append(pairs, key, label)
	}
	return boxSide(keyHints(columns-2, panel+amber, pairs...) + panel + amber)
}
