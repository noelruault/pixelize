package pixelize

import (
	"image"
)

// SwatchOptions configures palette swatch rendering.
type SwatchOptions struct {
	// CellSize is the side length of one color square in pixels.
	// Default 32.
	CellSize int

	// Columns sets how many cells per row. Default 8.
	// If 0 or larger than len(palette), all entries fit on one row.
	Columns int
}

// Swatch returns an image displaying the palette as a grid of color
// squares. Useful for inspecting a palette before applying it.
func (p Palette[M]) Swatch(opts SwatchOptions) *image.RGBA {
	cs := opts.CellSize
	if cs <= 0 {
		cs = 32
	}
	cols := opts.Columns
	if cols <= 0 || cols > len(p) {
		cols = len(p)
	}
	rows := (len(p) + cols - 1) / cols
	w := cols * cs
	h := rows * cs

	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for i, e := range p {
		c := e.Color()
		r := i / cols
		cIdx := i % cols
		x0 := cIdx * cs
		y0 := r * cs
		for y := y0; y < y0+cs; y++ {
			for x := x0; x < x0+cs; x++ {
				out.SetRGBA(x, y, c)
			}
		}
	}
	return out
}
