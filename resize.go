package pixelize

import (
	"fmt"
	"image"
	"image/color"

	xdraw "golang.org/x/image/draw"
)

// ResizeMode selects the resize algorithm used by Apply.
type ResizeMode int

const (
	// NearestNeighbor preserves blocky edges. Default for pixel art.
	NearestNeighbor ResizeMode = iota

	// BlockAverage averages every NxN source pixel block into one
	// destination pixel. Useful as a smoothing pre-pass before
	// quantizing photographic input. Only works when source dimensions
	// are integer multiples of the destination dimensions; otherwise
	// falls back to NearestNeighbor for the remainder.
	BlockAverage

	// BiLinear is golang.org/x/image/draw.BiLinear. Smooth.
	BiLinear

	// CatmullRom is golang.org/x/image/draw.CatmullRom. Sharpest of the
	// smooth resamplers.
	CatmullRom
)

// resize returns an *image.RGBA scaled to (w, h). If both w and h are
// zero the source is returned (copied into RGBA). If one is zero the
// missing dimension is derived to preserve aspect ratio.
func resize(src image.Image, w, h int, mode ResizeMode) (*image.RGBA, error) {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw == 0 || sh == 0 {
		return nil, fmt.Errorf("source image has zero dimension")
	}

	if w == 0 && h == 0 {
		dst := image.NewRGBA(image.Rect(0, 0, sw, sh))
		xdraw.Copy(dst, image.Point{}, src, sb, xdraw.Src, nil)
		return dst, nil
	}
	if w == 0 {
		w = sw * h / sh
		if w == 0 {
			w = 1
		}
	}
	if h == 0 {
		h = sh * w / sw
		if h == 0 {
			h = 1
		}
	}

	dst := image.NewRGBA(image.Rect(0, 0, w, h))

	switch mode {
	case NearestNeighbor:
		xdraw.NearestNeighbor.Scale(dst, dst.Bounds(), src, sb, xdraw.Over, nil)
	case BiLinear:
		xdraw.BiLinear.Scale(dst, dst.Bounds(), src, sb, xdraw.Over, nil)
	case CatmullRom:
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, sb, xdraw.Over, nil)
	case BlockAverage:
		blockAverage(dst, src, sb)
	default:
		return nil, fmt.Errorf("unknown resize mode: %d", mode)
	}
	return dst, nil
}

// blockAverage maps each destination pixel to the mean of the source
// pixels that fall inside its corresponding block. Block size is
// computed per axis; remainder pixels at the edge are averaged into
// the last column / row.
func blockAverage(dst *image.RGBA, src image.Image, sb image.Rectangle) {
	db := dst.Bounds()
	dw, dh := db.Dx(), db.Dy()
	sw, sh := sb.Dx(), sb.Dy()

	for dy := 0; dy < dh; dy++ {
		y0 := sb.Min.Y + dy*sh/dh
		y1 := sb.Min.Y + (dy+1)*sh/dh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for dx := 0; dx < dw; dx++ {
			x0 := sb.Min.X + dx*sw/dw
			x1 := sb.Min.X + (dx+1)*sw/dw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var sumR, sumG, sumB, sumA, count uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					r, g, b, a := src.At(sx, sy).RGBA()
					sumR += uint64(r)
					sumG += uint64(g)
					sumB += uint64(b)
					sumA += uint64(a)
					count++
				}
			}
			if count == 0 {
				count = 1
			}
			dst.SetRGBA(dx+db.Min.X, dy+db.Min.Y, color.RGBA{
				R: uint8((sumR / count) >> 8),
				G: uint8((sumG / count) >> 8),
				B: uint8((sumB / count) >> 8),
				A: uint8((sumA / count) >> 8),
			})
		}
	}
}
