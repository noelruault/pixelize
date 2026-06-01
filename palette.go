package pixelize

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"runtime"
	"sync"
)

// parallelMinPixels gates the row-banded parallel path in applyNearest.
// Below this, goroutine setup costs more than the scan saves, so we stay
// serial. ~256x256.
const parallelMinPixels = 1 << 16

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

	// match returns the nearest palette index for the pixel at (x, y),
	// using the exact same decision as the serial path: stdlib
	// color.Palette.Index when no custom metric is given, else the
	// linear scan over dist. Output is bit-identical to the prior
	// serial implementation.
	match := func(x, y int) int {
		c := src.At(x, y)
		if useStdlib {
			return cp.Index(c)
		}
		return nearest(c, cp, dist)
	}

	// Serial path for small images: goroutine setup would cost more than
	// it saves. Also the only path when a cancellable ctx must be checked
	// at fine granularity.
	if w*h < parallelMinPixels {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			for x := b.Min.X; x < b.Max.X; x++ {
				idx := match(x, y)
				e := p[idx]
				out.SetRGBA(x, y, e.Color())
				indices[x-b.Min.X][y-b.Min.Y] = idx
				usage[idx]++
			}
		}
		return &Pattern[M]{Image: out, Palette: p, Indices: indices, Usage: usage}, nil
	}

	// Parallel path: split rows into contiguous bands, one worker per band.
	// Workers share only read-only state (src, cp) plus disjoint output
	// regions, so no locking is needed. Each worker writes its own slice of
	// the output image, the per-column index buffers it owns, and a private
	// usage map that is merged at the end. The per-pixel decision is
	// identical to the serial path, so the result is bit-for-bit the same.
	workers := runtime.GOMAXPROCS(0)
	if workers > h {
		workers = h
	}
	band := (h + workers - 1) / workers

	partials := make([]map[int]int, workers)
	var cancelled bool
	var mu sync.Mutex
	var wg sync.WaitGroup

	for wkr := 0; wkr < workers; wkr++ {
		y0 := b.Min.Y + wkr*band
		if y0 >= b.Max.Y {
			break
		}
		y1 := y0 + band
		if y1 > b.Max.Y {
			y1 = b.Max.Y
		}
		wg.Add(1)
		go func(y0, y1 int) {
			defer wg.Done()
			local := make(map[int]int)
			for y := y0; y < y1; y++ {
				// Check cancellation once per row band boundary, not per
				// pixel: cheap and still responsive.
				if y == y0 {
					if err := ctx.Err(); err != nil {
						mu.Lock()
						cancelled = true
						mu.Unlock()
						return
					}
				}
				for x := b.Min.X; x < b.Max.X; x++ {
					idx := match(x, y)
					e := p[idx]
					out.SetRGBA(x, y, e.Color())
					indices[x-b.Min.X][y-b.Min.Y] = idx
					local[idx]++
				}
			}
			partials[wkr] = local
		}(y0, y1)
	}
	wg.Wait()

	if cancelled {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}

	for _, local := range partials {
		for idx, n := range local {
			usage[idx] += n
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
