package pixelize

import (
	"context"
	"image"
	"runtime"
	"sync"
)

// fastLUTBits is the per-channel precision of the Fast-mode LUT. 6 bits
// (2^18 = 262144 cells) is the speed/accuracy point report 11 chose: ~2-6%
// non-nearest pixels, while 5-bit is less accurate and 7-bit's 8 MiB table is
// both slower to build and slower to query (cache thrash).
const fastLUTBits = 6

// FastLUT is a precomputed nearest-palette-index table for the default
// (stdlib) metric, keyed on the high fastLUTBits of each RGB channel. It
// trades a small accuracy loss for a per-pixel cost of a single table lookup,
// independent of palette size.
//
// Building costs O(2^(3*bits) * P), so a FastLUT pays off only when reused
// across many images against a fixed palette (batch, watch): build it once
// with Palette.NewFastLUT, then pass it to Apply via ApplyOptions.FastLUT for
// every image. For a single one-shot image the exact path is already fast
// enough and more accurate. The table matches as if the source were opaque
// (it ignores the source alpha channel).
type FastLUT struct {
	bits  int
	table []int32 // nearest palette index per quantized (r,g,b) cell
}

// NewFastLUT builds a Fast-mode lookup table for p against the default metric.
// The result is read-only and safe to share across goroutines and Apply calls.
func (p Palette[M]) NewFastLUT() *FastLUT {
	bits := fastLUTBits
	tab := make([]pal16, len(p))
	for i, e := range p {
		tab[i] = pal16FromRGBA(e.R, e.G, e.B, 255)
	}
	n := 1 << bits    // cells per channel
	shift := 8 - bits // r>>shift selects the cell
	var half uint8
	if shift > 0 {
		half = uint8(1 << (shift - 1)) // cell-center representative color
	}
	table := make([]int32, n*n*n)

	// Build is parallelized across the outer (r) cell index.
	workers := runtime.GOMAXPROCS(0)
	if workers > n {
		workers = n
	}
	band := (n + workers - 1) / workers
	var wg sync.WaitGroup
	for wkr := 0; wkr < workers; wkr++ {
		r0 := wkr * band
		r1 := r0 + band
		if r1 > n {
			r1 = n
		}
		if r0 >= r1 {
			break
		}
		wg.Add(1)
		go func(r0, r1 int) {
			defer wg.Done()
			for ri := r0; ri < r1; ri++ {
				rc := uint8(ri<<shift) + half
				for gi := 0; gi < n; gi++ {
					gc := uint8(gi<<shift) + half
					base := (ri*n + gi) * n
					for bi := 0; bi < n; bi++ {
						bc := uint8(bi<<shift) + half
						table[base+bi] = int32(indexRaw(rc, gc, bc, 255, tab))
					}
				}
			}
		}(r0, r1)
	}
	wg.Wait()
	return &FastLUT{bits: bits, table: table}
}

// applyLUT quantizes src with a precomputed FastLUT: one table lookup per
// pixel, row-band parallel. Approximate (see FastLUT).
func (p Palette[M]) applyLUT(ctx context.Context, src *image.RGBA, lut *FastLUT) (*Pattern[M], error) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	nColors := len(p)

	out := image.NewRGBA(b)
	indices := make([][]int, w)
	for x := range indices {
		indices[x] = make([]int, h)
	}

	shift := 8 - lut.bits
	mask := (1 << lut.bits) - 1
	nn := 1 << lut.bits

	scanBand := func(y0, y1 int, counts []int) {
		for y := y0; y < y1; y++ {
			so := src.PixOffset(b.Min.X, y)
			oo := out.PixOffset(b.Min.X, y)
			yi := y - b.Min.Y
			for xi := 0; xi < w; xi++ {
				ri := (int(src.Pix[so]) >> shift) & mask
				gi := (int(src.Pix[so+1]) >> shift) & mask
				bi := (int(src.Pix[so+2]) >> shift) & mask
				idx := int(lut.table[(ri*nn+gi)*nn+bi])
				e := p[idx]
				out.Pix[oo], out.Pix[oo+1], out.Pix[oo+2], out.Pix[oo+3] = e.R, e.G, e.B, 255
				indices[xi][yi] = idx
				counts[idx]++
				so += 4
				oo += 4
			}
		}
	}

	usage, err := runScan(ctx, src, nColors, scanBand)
	if err != nil {
		return nil, err
	}
	return &Pattern[M]{Image: out, Palette: p, Indices: indices, Usage: usage}, nil
}
