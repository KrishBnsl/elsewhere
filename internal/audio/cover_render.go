//go:build !darwin || dhunn_audio

package audio

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
)

// coverMaxSide bounds the PNG sent to terminals that show real images.
const coverMaxSide = 512

// renderCover turns an image file's bytes into a cover: the whole picture squeezed into the
// small square, and a PNG of it at most coverMaxSide pixels across. JPEG, PNG and GIF.
func renderCover(data []byte) *Cover {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil
	}
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)

	var art Cover
	shrinkInto(rgba, CoverSize, CoverSize, func(x, y int, c [4]byte) { art.Pixels[y][x] = c })

	w, h := b.Dx(), b.Dy()
	if side := max(w, h); side > coverMaxSide {
		w, h = max(1, w*coverMaxSide/side), max(1, h*coverMaxSide/side)
	}
	thumb := image.NewRGBA(image.Rect(0, 0, w, h))
	shrinkInto(rgba, w, h, func(x, y int, c [4]byte) {
		copy(thumb.Pix[y*thumb.Stride+x*4:], c[:])
	})
	var out bytes.Buffer
	if png.Encode(&out, thumb) == nil {
		art.PNG = out.Bytes()
	}
	return &art
}

// shrinkInto averages src over a w×h grid (each output pixel is the mean of its area).
func shrinkInto(src *image.RGBA, w, h int, set func(x, y int, c [4]byte)) {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	for y := range h {
		y0, y1 := y*sh/h, max(y*sh/h+1, (y+1)*sh/h)
		for x := range w {
			x0, x1 := x*sw/w, max(x*sw/w+1, (x+1)*sw/w)
			var sum [4]int
			n := 0
			for sy := y0; sy < y1; sy++ {
				row := src.Pix[sy*src.Stride:]
				for sx := x0; sx < x1; sx++ {
					for i := range 4 {
						sum[i] += int(row[sx*4+i])
					}
					n++
				}
			}
			var c [4]byte
			for i := range 4 {
				c[i] = byte(sum[i] / max(1, n))
			}
			set(x, y, c)
		}
	}
}
