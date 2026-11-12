//go:build !darwin || dhunn_audio

package audio

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestRenderCover(t *testing.T) {
	// 1000×500: red on the left half, blue on the right.
	img := image.NewRGBA(image.Rect(0, 0, 1000, 500))
	for y := range 500 {
		for x := range 1000 {
			c := color.RGBA{255, 0, 0, 255}
			if x >= 500 {
				c = color.RGBA{0, 0, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	var data bytes.Buffer
	png.Encode(&data, img)
	art := renderCover(data.Bytes())
	if art == nil {
		t.Fatal("no cover")
	}
	// The whole picture is squeezed into the square.
	if left := art.Pixels[10][5]; left != [4]byte{255, 0, 0, 255} {
		t.Fatalf("left %v", left)
	}
	if right := art.Pixels[10][CoverSize-5]; right != [4]byte{0, 0, 255, 255} {
		t.Fatalf("right %v", right)
	}
	thumb, err := png.Decode(bytes.NewReader(art.PNG))
	if err != nil {
		t.Fatal(err)
	}
	if b := thumb.Bounds(); b.Dx() != coverMaxSide || b.Dy() != coverMaxSide/2 {
		t.Fatalf("thumbnail %v", b)
	}
	if renderCover([]byte("not an image")) != nil {
		t.Fatal("garbage decoded")
	}
}
