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

// Run-length gating (research report 10). A horizontal "same as the previous
// pixel" short-circuit computes the nearest index once per run of identical
// pixels and fills the span, which is a large win on coherent content (flat
// regions, pixel art: up to ~100x) and bit-identical to the per-pixel scan.
// On incoherent content (noise, photos) it is neutral, so it is gated on a
// cheap coherence probe: enable only when the sampled fraction of pixels
// equal to their left neighbor is at least runLengthMinCoherence.
const (
	runLengthProbeSamples = 4096
	runLengthMinCoherence = 0.10
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

	// Fast selects the approximate Fast mode (a 6-bit color LUT) for the
	// default metric. It trades a small accuracy loss (~2-6% of pixels get a
	// non-nearest color) for a per-pixel cost of one table lookup. Building
	// the LUT is only worthwhile when reused across many images against a
	// fixed palette; for a single image, set FastLUT instead or leave Fast
	// off (the exact path is already fast and more accurate). Ignored when
	// Dither is set or a custom Distance is given.
	Fast bool

	// FastLUT supplies a prebuilt Fast-mode table (see Palette.NewFastLUT) to
	// reuse across calls, which is where Fast mode actually pays off (batch,
	// watch): build once, pass it to every Apply. When set it takes
	// precedence over Fast and skips the per-call build. Ignored when Dither
	// is set or a custom Distance is given.
	FastLUT *FastLUT
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
	// Fast mode applies only to the default metric (the LUT is built on it)
	// and not under dithering.
	if opts.Distance == nil {
		if opts.FastLUT != nil {
			return p.applyLUT(ctx, resized, opts.FastLUT)
		}
		if opts.Fast {
			return p.applyLUT(ctx, resized, p.NewFastLUT())
		}
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

	// Three exact paths, chosen once (not per pixel):
	//   - useKD: default metric, large palette (P >= kdExactMinPaletteSize),
	//     opaque source. The bit-identical kd-tree (report 09): ~2.2-2.6x
	//     faster than the linear scan at P=256, growing with P. kd drops the
	//     alpha term, so it is only valid when the source is opaque (then the
	//     alpha term cancels and 8-bit RGB ordering matches stdlib exactly).
	//   - useStdlib (linear): replicate color.Palette.Index bit-for-bit
	//     against a precomputed 16-bit palette table, reading raw bytes from
	//     src.Pix. The flat-Pix16 form (report 08). Used for small palettes
	//     and as the opaque-alpha-independent default.
	//   - custom metric: linear scan over dist, reading the pixel straight
	//     from src.Pix into a color.RGBA (same value At() would yield).
	useStdlib := dist == nil
	useKD := useStdlib && nColors >= kdExactMinPaletteSize && src.Opaque()
	var tree *kdTree
	var tab []pal16
	var cp color.Palette
	switch {
	case useKD:
		tree = buildKDTree(p)
	case useStdlib:
		tab = make([]pal16, nColors)
		for i, e := range p {
			tab[i] = pal16FromRGBA(e.R, e.G, e.B, 255)
		}
	default:
		cp = p.ColorPalette()
	}

	// Enable the run-length short-circuit only when the source is coherent
	// enough for it to pay off (report 10). The probe is O(samples), so its
	// cost is negligible and independent of image size.
	useRunLength := coherenceProbe(src, runLengthProbeSamples) >= runLengthMinCoherence

	// scanBand quantizes rows [y0, y1), writing out.Pix and indices and
	// tallying into counts (len == nColors). The path branch is hoisted out
	// of the inner loop so the hot path is a tight per-pixel scan. The kd
	// path keeps its own per-band search scratch (no shared mutable state).
	scanBand := func(y0, y1 int, counts []int) {
		var ks kdSearch

		// Run-length path: extend each horizontal run of identical pixels,
		// resolve the nearest index once for the run via the active matcher,
		// and fill the span. The matcher is called once per run (not per
		// pixel), so the per-run path switch is free. Bit-identical to the
		// per-pixel scan: identical pixels resolve to the identical index.
		if useRunLength {
			matchOne := func(pr, pg, pb, pa uint8) int {
				switch {
				case useKD:
					return tree.match(&ks, pr, pg, pb)
				case useStdlib:
					return indexRaw(pr, pg, pb, pa, tab)
				default:
					return nearest(color.RGBA{R: pr, G: pg, B: pb, A: pa}, cp, dist)
				}
			}
			for y := y0; y < y1; y++ {
				so := src.PixOffset(b.Min.X, y)
				oo := out.PixOffset(b.Min.X, y)
				yi := y - b.Min.Y
				xi := 0
				for xi < w {
					pr, pg, pb, pa := src.Pix[so], src.Pix[so+1], src.Pix[so+2], src.Pix[so+3]
					runEnd := xi + 1
					sp := so + 4
					for runEnd < w && src.Pix[sp] == pr && src.Pix[sp+1] == pg && src.Pix[sp+2] == pb && src.Pix[sp+3] == pa {
						runEnd++
						sp += 4
					}
					idx := matchOne(pr, pg, pb, pa)
					e := p[idx]
					for x := xi; x < runEnd; x++ {
						out.Pix[oo], out.Pix[oo+1], out.Pix[oo+2], out.Pix[oo+3] = e.R, e.G, e.B, 255
						indices[x][yi] = idx
						oo += 4
					}
					counts[idx] += runEnd - xi
					so = sp
					xi = runEnd
				}
			}
			return
		}

		for y := y0; y < y1; y++ {
			so := src.PixOffset(b.Min.X, y)
			oo := out.PixOffset(b.Min.X, y)
			yi := y - b.Min.Y
			switch {
			case useKD:
				for xi := 0; xi < w; xi++ {
					idx := tree.match(&ks, src.Pix[so], src.Pix[so+1], src.Pix[so+2])
					e := p[idx]
					out.Pix[oo], out.Pix[oo+1], out.Pix[oo+2], out.Pix[oo+3] = e.R, e.G, e.B, 255
					indices[xi][yi] = idx
					counts[idx]++
					so += 4
					oo += 4
				}
			case useStdlib:
				for xi := 0; xi < w; xi++ {
					idx := indexRaw(src.Pix[so], src.Pix[so+1], src.Pix[so+2], src.Pix[so+3], tab)
					e := p[idx]
					out.Pix[oo], out.Pix[oo+1], out.Pix[oo+2], out.Pix[oo+3] = e.R, e.G, e.B, 255
					indices[xi][yi] = idx
					counts[idx]++
					so += 4
					oo += 4
				}
			default:
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

	usage, err := runScan(ctx, src, nColors, scanBand)
	if err != nil {
		return nil, err
	}
	return &Pattern[M]{Image: out, Palette: p, Indices: indices, Usage: usage}, nil
}

// runScan executes scanBand over src, serially for small images and row-band
// parallel otherwise, and returns the merged sparse usage map. scanBand must
// tally into the per-call counts slice (len == nColors) it is given; workers
// each get a private counts slice, merged here, so scanBand needs no locking.
// Band = h/GOMAXPROCS contiguous (report 08: finer bands lose to goroutine
// churn). ctx is checked per row (serial) or once per band (parallel).
func runScan(ctx context.Context, src *image.RGBA, nColors int, scanBand func(y0, y1 int, counts []int)) (map[int]int, error) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	if w*h < parallelMinPixels {
		counts := make([]int, nColors)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			scanBand(y, y+1, counts)
		}
		return countsToUsage(counts), nil
	}

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
	return countsToUsage(total), nil
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

// coherenceProbe samples ~sampleN pixels across evenly spaced rows and
// returns the fraction whose RGBA equals the immediately-preceding pixel in
// the row. Cost is O(sampleN), independent of image size, so it is cheap
// enough to run on every Apply to decide whether the run-length path pays off.
func coherenceProbe(src *image.RGBA, sampleN int) float64 {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 2 || h < 1 {
		return 0
	}
	rows := sampleN / (w - 1)
	if rows < 1 {
		rows = 1
	}
	if rows > 16 {
		rows = 16
	}
	rowStep := h / rows
	if rowStep < 1 {
		rowStep = 1
	}
	perRow := sampleN / rows
	if perRow > w-1 {
		perRow = w - 1
	}
	var seen, eq int
	for ri := 0; ri < rows; ri++ {
		yoff := ri * rowStep
		if yoff >= h {
			break
		}
		base := src.PixOffset(b.Min.X, b.Min.Y+yoff)
		for x := 1; x <= perRow; x++ {
			o := base + x*4
			if src.Pix[o] == src.Pix[o-4] && src.Pix[o+1] == src.Pix[o-3] &&
				src.Pix[o+2] == src.Pix[o-2] && src.Pix[o+3] == src.Pix[o-1] {
				eq++
			}
			seen++
		}
	}
	if seen == 0 {
		return 0
	}
	return float64(eq) / float64(seen)
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
