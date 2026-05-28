package pixelize

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
)

// Entry is one palette color plus arbitrary typed metadata.
//
// M is the metadata type chosen by the consumer: a string name, a
// rich struct (e.g. LegoColor with brick id), or struct{} when none
// is needed.
type Entry[M any] struct {
	R, G, B uint8
	Meta    M
}

// Color returns the entry as a stdlib color.RGBA with full opacity.
func (e Entry[M]) Color() color.RGBA {
	return color.RGBA{R: e.R, G: e.G, B: e.B, A: 255}
}

// Palette is an ordered set of entries. Indices into the slice are
// the canonical reference for results: Pattern.Indices stores them.
type Palette[M any] []Entry[M]

// ColorPalette converts to stdlib color.Palette for use with stdlib
// drawing routines (color.Palette.Index, image.NewPaletted, etc.).
func (p Palette[M]) ColorPalette() color.Palette {
	cp := make(color.Palette, len(p))
	for i, e := range p {
		cp[i] = e.Color()
	}
	return cp
}

// ApplyOptions configures palette application.
type ApplyOptions struct {
	// Width and Height set the target dimensions. Zero means no resize.
	// If only one is zero, it is derived from the other to preserve the
	// aspect ratio.
	Width, Height int

	// Resize selects the resize algorithm. Default NearestNeighbor.
	// Ignored when Width and Height are both zero.
	Resize ResizeMode

	// Dither enables Floyd-Steinberg dithering when quantizing.
	Dither bool

	// Distance overrides the nearest-color metric. nil means stdlib
	// unweighted Euclidean (color.Palette.Index).
	Distance DistanceFunc
}

// Pattern is the result of Apply.
type Pattern[M any] struct {
	// Image is the quantized output. Every pixel matches an entry in Palette.
	Image *image.RGBA

	// Palette is the palette used to produce Image. Held for downstream
	// helpers (Histogram, Dominant, build-map writers).
	Palette Palette[M]

	// Indices[x][y] is the palette index assigned to pixel (x, y).
	Indices [][]int

	// Usage maps palette index to pixel count.
	Usage map[int]int
}

// Apply resizes the image (if requested) and quantizes it against p.
// Returns a Pattern indexed against p.
func (p Palette[M]) Apply(ctx context.Context, img image.Image, opts ApplyOptions) (*Pattern[M], error) {
	if len(p) == 0 {
		return nil, fmt.Errorf("empty palette")
	}

	resized, err := resize(img, opts.Width, opts.Height, opts.Resize)
	if err != nil {
		return nil, fmt.Errorf("resize: %w", err)
	}

	if opts.Dither {
		return p.applyDither(ctx, resized)
	}
	return p.applyNearest(ctx, resized, opts.Distance)
}

func (p Palette[M]) applyNearest(ctx context.Context, src *image.RGBA, dist DistanceFunc) (*Pattern[M], error) {
	cp := p.ColorPalette()
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	out := image.NewRGBA(b)
	indices := make([][]int, w)
	for x := range indices {
		indices[x] = make([]int, h)
	}
	usage := make(map[int]int, len(p))

	useStdlib := dist == nil

	for y := b.Min.Y; y < b.Max.Y; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := b.Min.X; x < b.Max.X; x++ {
			c := src.At(x, y)
			var idx int
			if useStdlib {
				idx = cp.Index(c)
			} else {
				idx = nearest(c, cp, dist)
			}
			e := p[idx]
			out.SetRGBA(x, y, e.Color())
			indices[x-b.Min.X][y-b.Min.Y] = idx
			usage[idx]++
		}
	}

	return &Pattern[M]{
		Image:   out,
		Palette: p,
		Indices: indices,
		Usage:   usage,
	}, nil
}

func (p Palette[M]) applyDither(ctx context.Context, src *image.RGBA) (*Pattern[M], error) {
	cp := p.ColorPalette()
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	paletted := image.NewPaletted(b, cp)
	draw.FloydSteinberg.Draw(paletted, b, src, b.Min)

	indices := make([][]int, w)
	for x := range indices {
		indices[x] = make([]int, h)
	}
	usage := make(map[int]int, len(p))
	out := image.NewRGBA(b)

	for y := b.Min.Y; y < b.Max.Y; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := b.Min.X; x < b.Max.X; x++ {
			idx := int(paletted.ColorIndexAt(x, y))
			e := p[idx]
			out.SetRGBA(x, y, e.Color())
			indices[x-b.Min.X][y-b.Min.Y] = idx
			usage[idx]++
		}
	}

	return &Pattern[M]{
		Image:   out,
		Palette: p,
		Indices: indices,
		Usage:   usage,
	}, nil
}

func nearest(c color.Color, cp color.Palette, dist DistanceFunc) int {
	best, bestD := 0, dist(c, cp[0])
	for i := 1; i < len(cp); i++ {
		d := dist(c, cp[i])
		if d < bestD {
			bestD = d
			best = i
		}
	}
	return best
}
