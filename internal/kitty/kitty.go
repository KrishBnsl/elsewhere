// Package kitty draws images with the Kitty terminal graphics protocol (Kitty, Ghostty).
package kitty

import (
	"encoding/base64"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
)

// Placement is a PNG shown at a terminal cell. The zero value means nothing is shown.
type Placement struct {
	PNG                        []byte
	Row, Column, Columns, Rows int
}

// Visible reports whether the placement shows an image.
func (p Placement) Visible() bool { return len(p.PNG) > 0 }

// Equal reports whether two placements show the same image data in the same place.
func (p Placement) Equal(o Placement) bool {
	if p.Row != o.Row || p.Column != o.Column || p.Columns != o.Columns || p.Rows != o.Rows || len(p.PNG) != len(o.PNG) {
		return false
	}
	return len(p.PNG) == 0 || &p.PNG[0] == &o.PNG[0]
}

// Supported reports whether the terminal can show images directly. Multiplexers cannot.
func Supported() bool {
	term := os.Getenv("TERM")
	if os.Getenv("TMUX") != "" || os.Getenv("STY") != "" ||
		strings.HasPrefix(term, "tmux") || strings.HasPrefix(term, "screen") {
		return false
	}
	return term == "xterm-kitty" || term == "xterm-ghostty" || os.Getenv("TERM_PROGRAM") == "ghostty"
}

// NewImageID picks an identifier for the image this player draws.
func NewImageID() uint32 {
	return 1 + rand.Uint32N(1<<31-1)
}

// Delete returns the escape sequence that removes the image with the given id.
func Delete(id uint32) string {
	return fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)
}

// Write appends the escape sequences that draw the placement.
func (p Placement) Write(out *strings.Builder, id uint32) {
	fmt.Fprintf(out, "\x1b[%d;%dH", p.Row, p.Column)
	encoded := base64.StdEncoding.EncodeToString(p.PNG)
	const chunkSize = 4096
	for i := 0; i < len(encoded); i += chunkSize {
		end := min(i+chunkSize, len(encoded))
		more := 0
		if end < len(encoded) {
			more = 1
		}
		out.WriteString("\x1b_G")
		if i == 0 {
			fmt.Fprintf(out, "a=T,f=100,t=d,q=2,C=1,i=%d,c=%d,r=%d,z=1,", id, p.Columns, p.Rows)
		}
		fmt.Fprintf(out, "m=%d;%s\x1b\\", more, encoded[i:end])
	}
}
