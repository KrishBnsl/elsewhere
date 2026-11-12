package app

import (
	"fmt"
	"path/filepath"
)

func (p *player) overlayCoverPrompt(rows []string, width int) {
	columns := min(66, width-4)
	const heading = " MISSING COVER "
	lines := []string{
		boxTop(heading, columns),
		boxSide(cell(" "+p.coverPrompt.title, columns-2)),
		boxSide(cell(" Folder: "+filepath.Base(filepath.Dir(p.current().path)), columns-2)),
		boxSide(cell(" Download cover into the album folder?", columns-2)),
		boxSide(cell(" [Y/n]  Enter Download  Esc Skip", columns-2)),
		boxBottom(columns),
	}
	x, y := (width-columns)/2+1, (len(rows)-len(lines))/2
	cursor := fmt.Sprintf("\x1b[%dG", x)
	for i, line := range lines {
		rows[y+i] += cursor + panel + amber + line + reset
	}
}
