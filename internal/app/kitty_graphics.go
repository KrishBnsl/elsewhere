package app

import (
	"encoding/base64"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
)

type kittyPlacement struct {
	art                        *cover
	row, column, columns, rows int
}

func supportsKittyGraphics() bool {
	term := os.Getenv("TERM")
	if os.Getenv("TMUX") != "" || os.Getenv("STY") != "" ||
		strings.HasPrefix(term, "tmux") || strings.HasPrefix(term, "screen") {
		return false
	}
	return term == "xterm-kitty" || term == "xterm-ghostty" || os.Getenv("TERM_PROGRAM") == "ghostty"
}

func newKittyImageID() uint32 {
	return 1 + rand.Uint32N(1<<31-1)
}

func deleteKittyImage(id uint32) string {
	return fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)
}

func (placement kittyPlacement) write(out *strings.Builder, id uint32) {
	fmt.Fprintf(out, "\x1b[%d;%dH", placement.row, placement.column)
	encoded := base64.StdEncoding.EncodeToString(placement.art.png)
	const chunkSize = 4096
	for i := 0; i < len(encoded); i += chunkSize {
		end := min(i+chunkSize, len(encoded))
		more := 0
		if end < len(encoded) {
			more = 1
		}
		out.WriteString("\x1b_G")
		if i == 0 {
			fmt.Fprintf(out, "a=T,f=100,t=d,q=2,C=1,i=%d,c=%d,r=%d,z=1,", id, placement.columns, placement.rows)
		}
		fmt.Fprintf(out, "m=%d;%s\x1b\\", more, encoded[i:end])
	}
}
