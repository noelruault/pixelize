package preview

import (
	"fmt"
	"image"
	"io"
)

// renderANSI prints two pixels per terminal cell using the foreground
// half block "▀" with foreground = top pixel and background = bottom
// pixel. Truecolor escapes (24-bit).
//
// If maxWidth > 0, downsamples the image so the rendered width fits.
func renderANSI(w io.Writer, img image.Image, maxWidth int) error {
	src := scale(img, maxWidth)
	b := src.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 2 {
		for x := b.Min.X; x < b.Max.X; x++ {
			tr, tg, tb, _ := src.At(x, y).RGBA()
			ty := y + 1
			var br, bg, bb uint32
			if ty < b.Max.Y {
				br, bg, bb, _ = src.At(x, ty).RGBA()
			} else {
				br, bg, bb = tr, tg, tb
			}
			fmt.Fprintf(w, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀",
				tr>>8, tg>>8, tb>>8,
				br>>8, bg>>8, bb>>8,
			)
		}
		fmt.Fprint(w, "\x1b[0m\n")
	}
	return nil
}

// scale returns the image downsampled by integer ratio so rendered
// width stays within maxWidth columns. maxWidth <= 0 returns the
// source unchanged.
func scale(img image.Image, maxWidth int) image.Image {
	if maxWidth <= 0 {
		return img
	}
	b := img.Bounds()
	w := b.Dx()
	if w <= maxWidth {
		return img
	}
	step := (w + maxWidth - 1) / maxWidth
	out := image.NewRGBA(image.Rect(0, 0, w/step, b.Dy()/step))
	for y := 0; y < out.Bounds().Dy(); y++ {
		for x := 0; x < out.Bounds().Dx(); x++ {
			out.Set(x, y, img.At(b.Min.X+x*step, b.Min.Y+y*step))
		}
	}
	return out
}
