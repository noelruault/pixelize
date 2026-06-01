package pixelize

import (
	"context"
	"image"
	"image/color"
	"math/rand"
	"testing"
)

// serialNearest reproduces the pre-parallel reference implementation
// exactly: a single-threaded linear/stdlib scan. The parallel applyNearest
// must produce bit-identical output to this.
func serialNearest[M any](p Palette[M], src *image.RGBA, dist DistanceFunc) (*image.RGBA, [][]int, map[int]int) {
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
		for x := b.Min.X; x < b.Max.X; x++ {
			c := src.At(x, y)
			var idx int
			if useStdlib {
				idx = cp.Index(c)
			} else {
				idx = nearest(c, cp, dist)
			}
			out.SetRGBA(x, y, p[idx].Color())
			indices[x-b.Min.X][y-b.Min.Y] = idx
			usage[idx]++
		}
	}
	return out, indices, usage
}

// randomImage builds a deterministic w×h opaque noise image. Noise defeats
// run-length coherence and forces a real decision at every pixel.
func randomImage(w, h int, seed int64) *image.RGBA {
	return randomImageOpacity(w, h, seed, true)
}

// randomImageOpacity builds a deterministic w×h noise image. When opaque is
// false, alpha varies too, which exercises the alpha-carrying paths (and
// must disqualify the kd path, since kd drops the alpha term).
func randomImageOpacity(w, h int, seed int64, opaque bool) *image.RGBA {
	r := rand.New(rand.NewSource(seed))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = uint8(r.Intn(256))
		img.Pix[i+1] = uint8(r.Intn(256))
		img.Pix[i+2] = uint8(r.Intn(256))
		if opaque {
			img.Pix[i+3] = 255
		} else {
			img.Pix[i+3] = uint8(r.Intn(256))
		}
	}
	return img
}

func testPalette(n int, seed int64) Palette[struct{}] {
	r := rand.New(rand.NewSource(seed))
	p := make(Palette[struct{}], n)
	for i := range p {
		p[i] = Entry[struct{}]{R: uint8(r.Intn(256)), G: uint8(r.Intn(256)), B: uint8(r.Intn(256))}
	}
	return p
}

// TestParallelMatchesSerial is the exactness gate for the parallel scan.
// It runs sizes that straddle parallelMinPixels (so both the serial and
// the parallel branch are exercised) and palette sizes on both sides of
// the eventual linear/kd crossover, with and without a custom metric, and
// asserts bit-for-bit identical output, indices, and usage versus the
// serial reference. Run under -race to also catch data races in the
// row-band workers.
func TestParallelMatchesSerial(t *testing.T) {
	sizes := []struct{ w, h int }{
		{16, 16},   // tiny, serial branch
		{255, 257}, // just over the 65536 threshold, parallel branch
		{512, 512}, // comfortably parallel
		{300, 1},   // single row: workers must clamp to h
	}
	// Palette sizes straddle kdExactMinPaletteSize (128): below it the linear
	// path runs, at/above it the kd path runs (when the source is opaque).
	palSizes := []int{1, 16, 64, 127, 128, 256}
	dists := []struct {
		name string
		fn   DistanceFunc
	}{
		{"stdlib", nil},
		{"euclidean", EuclideanRGBA},
	}
	// opaque true exercises the kd path at P>=128; opaque false carries
	// alpha and must fall back off kd while staying bit-identical to cp.Index.
	opacities := []bool{true, false}

	for _, sz := range sizes {
		for _, n := range palSizes {
			for _, d := range dists {
				for _, opaque := range opacities {
					name := d.name
					if !opaque {
						name += "/alpha"
					}
					img := randomImageOpacity(sz.w, sz.h, 1, opaque)
					pal := testPalette(n, 2)

					wantImg, wantIdx, wantUsage := serialNearest(pal, img, d.fn)
					got, err := pal.applyNearest(context.Background(), img, d.fn)
					if err != nil {
						t.Fatalf("%dx%d P=%d %s: applyNearest: %v", sz.w, sz.h, n, name, err)
					}

					if !bytesEqual(got.Image.Pix, wantImg.Pix) {
						t.Fatalf("%dx%d P=%d %s: image pixels differ from serial", sz.w, sz.h, n, name)
					}
					for x := range wantIdx {
						for y := range wantIdx[x] {
							if got.Indices[x][y] != wantIdx[x][y] {
								t.Fatalf("%dx%d P=%d %s: index[%d][%d]=%d want %d",
									sz.w, sz.h, n, name, x, y, got.Indices[x][y], wantIdx[x][y])
							}
						}
					}
					if len(got.Usage) != len(wantUsage) {
						t.Fatalf("%dx%d P=%d %s: usage size %d want %d", sz.w, sz.h, n, name, len(got.Usage), len(wantUsage))
					}
					for idx, n2 := range wantUsage {
						if got.Usage[idx] != n2 {
							t.Fatalf("%dx%d P=%d %s: usage[%d]=%d want %d", sz.w, sz.h, n, name, idx, got.Usage[idx], n2)
						}
					}
				}
			}
		}
	}
}

// blockyImage builds a deterministic coherent image: every block×block tile
// is a single random color, so consecutive pixels in a row are usually equal.
// This drives the coherence probe high and exercises the run-length path.
func blockyImage(w, h, block int, seed int64, opaque bool) *image.RGBA {
	r := rand.New(rand.NewSource(seed))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for by := 0; by < h; by += block {
		for bx := 0; bx < w; bx += block {
			cr, cg, cb := uint8(r.Intn(256)), uint8(r.Intn(256)), uint8(r.Intn(256))
			ca := uint8(255)
			if !opaque {
				ca = uint8(r.Intn(256))
			}
			for y := by; y < by+block && y < h; y++ {
				for x := bx; x < bx+block && x < w; x++ {
					o := img.PixOffset(x, y)
					img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = cr, cg, cb, ca
				}
			}
		}
	}
	return img
}

// TestRunLengthMatchesSerial is the exactness gate for the run-length path.
// It uses coherent (blocky) images so the probe enables the run-length
// short-circuit, then asserts bit-for-bit identical output, indices, and
// usage versus the per-pixel serial reference. Run under -race too.
func TestRunLengthMatchesSerial(t *testing.T) {
	palSizes := []int{16, 128, 256} // linear and kd regimes
	dists := []struct {
		name string
		fn   DistanceFunc
	}{
		{"stdlib", nil},
		{"euclidean", EuclideanRGBA},
	}
	for _, opaque := range []bool{true, false} {
		for _, n := range palSizes {
			for _, d := range dists {
				img := blockyImage(300, 300, 7, 3, opaque)
				if got := coherenceProbe(img, runLengthProbeSamples); got < runLengthMinCoherence {
					t.Fatalf("blocky image probe %.3f < %.3f: run-length path would not engage", got, runLengthMinCoherence)
				}
				pal := testPalette(n, 2)
				name := d.name
				if !opaque {
					name += "/alpha"
				}

				wantImg, wantIdx, wantUsage := serialNearest(pal, img, d.fn)
				got, err := pal.applyNearest(context.Background(), img, d.fn)
				if err != nil {
					t.Fatalf("P=%d %s: applyNearest: %v", n, name, err)
				}
				if !bytesEqual(got.Image.Pix, wantImg.Pix) {
					t.Fatalf("P=%d %s: image pixels differ from serial", n, name)
				}
				for x := range wantIdx {
					for y := range wantIdx[x] {
						if got.Indices[x][y] != wantIdx[x][y] {
							t.Fatalf("P=%d %s: index[%d][%d]=%d want %d", n, name, x, y, got.Indices[x][y], wantIdx[x][y])
						}
					}
				}
				if len(got.Usage) != len(wantUsage) {
					t.Fatalf("P=%d %s: usage size %d want %d", n, name, len(got.Usage), len(wantUsage))
				}
				for idx, n2 := range wantUsage {
					if got.Usage[idx] != n2 {
						t.Fatalf("P=%d %s: usage[%d]=%d want %d", n, name, idx, got.Usage[idx], n2)
					}
				}
			}
		}
	}
}

func bytesEqual(a, b []uint8) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ensure color is referenced (kept for parity with other test files).
var _ = color.RGBA{}
