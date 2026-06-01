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
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	nColors := len(p)

	out := image.NewRGBA(b)
	indices := make([][]int, w)
	for x := range indices {
		indices[x] = make([]int, h)
	}

	// Two exact paths, chosen once (not per pixel):
	//   - useStdlib: replicate color.Palette.Index bit-for-bit against a
	//     precomputed 16-bit palette table, reading raw bytes from src.Pix.
	//     This is the flat-Pix16 form (research report 08): ~1.65x faster
	//     than the prior At()/cp.Index() scan with near-zero hot-loop
	//     allocation, and verified bit-identical (image, indices, usage).
	//   - custom metric: linear scan over dist, reading the pixel straight
	//     from src.Pix into a color.RGBA (same value At() would yield).
	useStdlib := dist == nil
	var tab []pal16
	var cp color.Palette
	if useStdlib {
		tab = make([]pal16, nColors)
		for i, e := range p {
			tab[i] = pal16FromRGBA(e.R, e.G, e.B, 255)
		}
	} else {
		cp = p.ColorPalette()
	}

	// scanBand quantizes rows [y0, y1), writing out.Pix and indices and
	// tallying into counts (len == nColors). The useStdlib branch is hoisted
	// out of the inner loop so the hot path is a tight per-pixel scan.
	scanBand := func(y0, y1 int, counts []int) {
		for y := y0; y < y1; y++ {
			so := src.PixOffset(b.Min.X, y)
			oo := out.PixOffset(b.Min.X, y)
			yi := y - b.Min.Y
			if useStdlib {
				for xi := 0; xi < w; xi++ {
					idx := indexRaw(src.Pix[so], src.Pix[so+1], src.Pix[so+2], src.Pix[so+3], tab)
					e := p[idx]
					out.Pix[oo], out.Pix[oo+1], out.Pix[oo+2], out.Pix[oo+3] = e.R, e.G, e.B, 255
					indices[xi][yi] = idx
					counts[idx]++
					so += 4
					oo += 4
				}
			} else {
				for xi := 0; xi < w; xi++ {
					c := color.RGBA{R: src.Pix[so], G: src.Pix[so+1], B: src.Pix[so+2], A: src.Pix[so+3]}
					idx := nearest(c, cp, dist)
					e := p[idx]
					out.Pix[oo], out.Pix[oo+1], out.Pix[oo+2], out.Pix[oo+3] = e.R, e.G, e.B, 255
					indices[xi][yi] = idx
					counts[idx]++
					so += 4
					oo += 4
				}
			}
		}
	}

	// Serial path for small images: goroutine setup would cost more than it
	// saves, and a cancellable ctx can be checked per row.
	if w*h < parallelMinPixels {
		counts := make([]int, nColors)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			scanBand(y, y+1, counts)
		}
		return &Pattern[M]{Image: out, Palette: p, Indices: indices, Usage: countsToUsage(counts)}, nil
	}

	// Parallel path: split rows into contiguous bands, one worker per band
	// (band = h/GOMAXPROCS is optimal per report 08; finer bands lose to
	// goroutine churn). Workers share only read-only state (src, tab, cp)
	// plus disjoint output regions, index ranges, and a private []int count
	// array each, so no locking is needed. The per-pixel decision is
	// identical to the serial path, so the result is bit-for-bit the same.
	workers := runtime.GOMAXPROCS(0)
	if workers > h {
		workers = h
	}
	band := (h + workers - 1) / workers

	partials := make([][]int, workers)
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
		go func(wkr, y0, y1 int) {
			defer wg.Done()
			// One cancellation check per band; bands are short enough that
			// this is responsive without per-pixel overhead.
			if err := ctx.Err(); err != nil {
				mu.Lock()
				cancelled = true
				mu.Unlock()
				return
			}
			counts := make([]int, nColors)
			scanBand(y0, y1, counts)
			partials[wkr] = counts
		}(wkr, y0, y1)
	}
	wg.Wait()

	if cancelled {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}

	total := make([]int, nColors)
	for _, counts := range partials {
		for idx, n := range counts {
			total[idx] += n
		}
	}

	return &Pattern[M]{
		Image:   out,
		Palette: p,
		Indices: indices,
		Usage:   countsToUsage(total),
	}, nil
}

// pal16 holds one palette color promoted to the 16-bit channel values that
// color.Color.RGBA() yields (v|v<<8), so the nearest-color scan can run as
// plain integer math with no per-pixel interface calls.
type pal16 struct{ r, g, b, a uint32 }

func pal16FromRGBA(r, g, b, a uint8) pal16 {
	rr := uint32(r)
	rr |= rr << 8
	gg := uint32(g)
	gg |= gg << 8
	bb := uint32(b)
	bb |= bb << 8
	aa := uint32(a)
	aa |= aa << 8
	return pal16{rr, gg, bb, aa}
}

// sqDiff replicates image/color.sqDiff exactly: the squared difference of two
// 16-bit channel values, scaled by >>2.
func sqDiff(x, y uint32) uint32 {
	var d uint32
	if x > y {
		d = x - y
	} else {
		d = y - x
	}
	return (d * d) >> 2
}

// indexRaw returns the nearest palette index for raw 8-bit channel bytes,
// matching color.Palette.Index bit-for-bit: 8->16-bit promotion before
// squaring, the alpha term, the strict-< first-match tie-break (lowest index
// wins ties), and the early return on an exact match.
func indexRaw(pr, pg, pb, pa uint8, tab []pal16) int {
	cr := uint32(pr)
	cr |= cr << 8
	cg := uint32(pg)
	cg |= cg << 8
	cb := uint32(pb)
	cb |= cb << 8
	ca := uint32(pa)
	ca |= ca << 8
	ret := 0
	bestSum := uint32(1<<32 - 1)
	for i := range tab {
		t := tab[i]
		sum := sqDiff(cr, t.r) + sqDiff(cg, t.g) + sqDiff(cb, t.b) + sqDiff(ca, t.a)
		if sum < bestSum {
			if sum == 0 {
				return i
			}
			ret, bestSum = i, sum
		}
	}
	return ret
}

// countsToUsage converts a per-index count array to the sparse Usage map,
// including only indices that were actually assigned (matching the prior
// map-accumulation behavior, where unused indices have no entry).
func countsToUsage(counts []int) map[int]int {
	usage := make(map[int]int)
	for idx, n := range counts {
		if n > 0 {
			usage[idx] = n
		}
	}
	return usage
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
