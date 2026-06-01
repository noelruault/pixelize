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

// randomImage builds a deterministic w×h noise image. Noise defeats
// run-length coherence and forces a real decision at every pixel.
func randomImage(w, h int, seed int64) *image.RGBA {
	r := rand.New(rand.NewSource(seed))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = uint8(r.Intn(256))
		img.Pix[i+1] = uint8(r.Intn(256))
		img.Pix[i+2] = uint8(r.Intn(256))
		img.Pix[i+3] = 255
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
	palSizes := []int{1, 16, 64, 256}
	dists := []struct {
		name string
		fn   DistanceFunc
	}{
		{"stdlib", nil},
		{"euclidean", EuclideanRGBA},
	}

	for _, sz := range sizes {
		for _, n := range palSizes {
			for _, d := range dists {
				name := d.name
				img := randomImage(sz.w, sz.h, 1)
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
