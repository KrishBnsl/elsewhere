package kitty

import (
	"strings"
	"testing"
)

func TestPlacementEqualityFollowsImageIdentity(t *testing.T) {
	png := []byte{1, 2, 3}
	a := Placement{PNG: png, Row: 2, Column: 3, Columns: 8, Rows: 4}
	if !a.Equal(a) || !(Placement{}).Equal(Placement{}) {
		t.Fatal("a placement should equal itself")
	}
	moved := a
	moved.Row++
	if a.Equal(moved) {
		t.Fatal("moving the image changes the placement")
	}
	copied := a
	copied.PNG = append([]byte(nil), png...)
	if a.Equal(copied) {
		t.Fatal("a different image buffer is a different placement even with equal bytes")
	}
	if (Placement{}).Visible() || !a.Visible() {
		t.Fatal("Visible should follow the PNG data")
	}
}

func TestWriteChunksLargeImages(t *testing.T) {
	var out strings.Builder
	Placement{PNG: make([]byte, 10000), Row: 1, Column: 1, Columns: 4, Rows: 2}.Write(&out, 7)
	text := out.String()
	if !strings.HasPrefix(text, "\x1b[1;1H\x1b_Ga=T,f=100,t=d,q=2,C=1,i=7,c=4,r=2,z=1,m=1;") {
		t.Fatalf("unexpected start: %q", text[:60])
	}
	if strings.Count(text, "\x1b_G") < 3 || !strings.Contains(text, "m=0;") {
		t.Fatal("a 10 KB image should be sent in several chunks, the last marked m=0")
	}
}

func TestSupportedRejectsMultiplexers(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("TMUX", "")
	t.Setenv("STY", "")
	if !Supported() {
		t.Fatal("kitty should be supported")
	}
	t.Setenv("TMUX", "/tmp/tmux")
	if Supported() {
		t.Fatal("tmux cannot pass images through")
	}
}
