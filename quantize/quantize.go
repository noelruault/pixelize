// Package quantize derives a palette from an image — "turn any image into N
// colors" — to feed pixelize's Apply pipeline. It is the engine side of
// workflow B; once you have a Result you can dither, build mosaics, count
// pieces, or render GIFs exactly as with a loaded palette.
//
// # The pipeline
//
// One pipeline, parameterized by color space: a PCA-divisive initializer
// (variance-selected box, split along its principal axis) followed by a
// weighted Lloyd (k-means) refine. Both run in the same space, and the palette
// is paired with the matching assignment metric so the perceptual result holds
// through to the output.
//
// # Which space
//
// The right space depends on palette density, so Generate picks it from n by
// default (SpaceAuto):
//
//   - n <= 48  -> RGB.   Small palettes: entries are far apart (large color
//     differences), the regime where plain RGB does best.
//   - n  > 48  -> OKLab. Large palettes: entries are close (small differences),
//     OKLab's perceptually-uniform regime.
//
// Measured against libimagequant (pngquant) and ImageMagick on the Kodak suite,
// this beats ImageMagick's octree at every size and matches-or-edges
// libimagequant. See noelruault/research/quantization (reports 02, 05, 09, 11).
//
// # Determinism
//
// Output is deterministic: the histogram is sorted into a canonical order, so
// every downstream step (including the k-means summation order) is fixed.
package quantize

import (
	"fmt"
	"image"

	"github.com/noelruault/pixelize"
)

// AutoThreshold is the palette size at/below which SpaceAuto chooses RGB and
// above which it chooses OKLab. The RGB<->OKLab crossover is empirically
// ~32-48; only the n>=64 OKLab side is sharp, so the exact value below ~48 is
// not consequential.
const AutoThreshold = 48

// SpaceMode selects the working color space.
type SpaceMode int

const (
	SpaceAuto  SpaceMode = iota // RGB for n<=AutoThreshold, else OKLab (default)
	SpaceRGB                    // force RGB
	SpaceOKLab                  // force OKLab
)

// Options configures Generate. The zero value is valid (SpaceAuto, 10
// iterations, PCA-divisive init).
type Options struct {
	// Space is the working color space. SpaceAuto (default) picks by n.
	Space SpaceMode

	// Iterations is the number of weighted Lloyd refine passes. 0 means the
	// default (10). The init choice is second-order once refined.
	Iterations int

	// CurveInit replaces the PCA-divisive initializer with a space-filling
	// (Morton Z-order) curve seed. Off by default. It only helps at large
	// palettes — hint: enable it for n >= 256, where the uniform coverage it
	// gives measurably lowers error; at smaller n it is neutral-to-worse.
	CurveInit bool
}

const defaultIterations = 10

// Result is a derived palette paired with the assignment metric it was built
// for. Pass Distance to ApplyOptions so pixels are matched in the same space
// the palette was optimized in.
type Result struct {
	// Palette is the derived palette, ready for pixelize.Palette.Apply. Each
	// entry's Meta.Hex is filled; Name is "auto_<i>".
	Palette pixelize.Palette[pixelize.EntryMeta]

	// Distance is the matched assignment metric: nil for RGB (use the default
	// exact Euclidean / kd-tree path) or OKLabDistance for OKLab.
	Distance pixelize.DistanceFunc

	// Space is "rgb" or "oklab", for logging.
	Space string
}

// Generate derives a palette of at most n colors from img.
func Generate(img image.Image, n int, opts *Options) (Result, error) {
	if n < 1 {
		return Result{}, fmt.Errorf("quantize: n must be >= 1, got %d", n)
	}
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return Result{}, fmt.Errorf("quantize: image has zero dimension")
	}
	var o Options
	if opts != nil {
		o = *opts
	}
	iters := o.Iterations
	if iters <= 0 {
		iters = defaultIterations
	}

	useOKLab := o.Space == SpaceOKLab || (o.Space == SpaceAuto && n > AutoThreshold)
	var sp space = rgbSpace{}
	res := Result{Space: "rgb"}
	if useOKLab {
		sp = oklabSpace{}
		res.Distance = OKLabDistance
		res.Space = "oklab"
	}

	h := histogram(img)
	if len(h) <= n {
		// Fewer distinct colors than requested: the palette is the image's
		// exact colors; no clustering, no assignment loss.
		res.Palette = toPalette(colorsToVecs(h, sp), sp)
		return res, nil
	}

	pts := points(h, sp)
	var cents []vec3
	if o.CurveInit {
		cents = curveInit(pts, n)
	} else {
		cents = divisiveInit(pts, n)
	}
	cents = refine(pts, cents, iters)

	res.Palette = toPalette(cents, sp)
	return res, nil
}

func colorsToVecs(h []wcolor, sp space) []vec3 {
	out := make([]vec3, len(h))
	for i, c := range h {
		out[i] = sp.fromRGB(c.r, c.g, c.b)
	}
	return out
}

func toPalette(cents []vec3, sp space) pixelize.Palette[pixelize.EntryMeta] {
	pal := make(pixelize.Palette[pixelize.EntryMeta], len(cents))
	for i, c := range cents {
		rgba := sp.toRGBA(c)
		hex := fmt.Sprintf("%02X%02X%02X", rgba.R, rgba.G, rgba.B)
		pal[i] = pixelize.Entry[pixelize.EntryMeta]{
			R: rgba.R, G: rgba.G, B: rgba.B,
			Meta: pixelize.EntryMeta{Name: fmt.Sprintf("auto_%d", i+1), Hex: hex},
		}
	}
	return pal
}
