package pixelize

import (
	"context"
	"image"
	"image/jpeg"
	"os"
	"testing"

	"github.com/noelruault/pixelize/palettes"
)

// Matcher microbenchmarks over a real image, for the shipped exact and Fast
// paths. These are "our own" numbers: they isolate the nearest-color matcher
// (no decode/resize/encode), so a regression in applyNearest shows here even
// when end-to-end wall time is dominated by I/O. Pair them with bench/compare.sh
// (pixelize vs ImageMagick, end-to-end) and bench/history.
//
//	go test -run xxx -bench Matcher -benchmem .

func benchPalette(b *testing.B, name string) Palette[EntryMeta] {
	b.Helper()
	r, err := palettes.Resolve(name)
	if err != nil {
		b.Skipf("palette %s unavailable: %v", name, err)
	}
	defer r.Reader.Close()
	pal, err := LoadCSV(r.Reader)
	if err != nil {
		b.Fatalf("load %s: %v", name, err)
	}
	return pal
}

func benchSrcImage(b *testing.B) *image.RGBA {
	b.Helper()
	const path = "docs/demo/inputs/starry.jpg"
	f, err := os.Open(path)
	if err != nil {
		b.Skipf("image %s unavailable: %v", path, err)
	}
	defer f.Close()
	src, err := jpeg.Decode(f)
	if err != nil {
		b.Fatalf("decode: %v", err)
	}
	bnd := src.Bounds()
	img := image.NewRGBA(bnd)
	for y := bnd.Min.Y; y < bnd.Max.Y; y++ {
		for x := bnd.Min.X; x < bnd.Max.X; x++ {
			img.Set(x, y, src.At(x, y))
		}
	}
	return img
}

// BenchmarkMatcherExact times the exact default path. gameboy/pico8/nes are
// small-P opaque (the alpha-dropped linear scan); lego is large-P opaque (kd).
func BenchmarkMatcherExact(b *testing.B) {
	img := benchSrcImage(b)
	px := float64(img.Bounds().Dx() * img.Bounds().Dy())
	ctx := context.Background()
	for _, name := range []string{"gameboy", "pico8", "nes", "lego"} {
		pal := benchPalette(b, name)
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := pal.applyNearest(ctx, img, nil); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/px, "ns/px")
		})
	}
}

// BenchmarkMatcherLUT times the opt-in Fast (lookup-table) path with a prebuilt,
// reused table (the batch/watch usage).
func BenchmarkMatcherLUT(b *testing.B) {
	img := benchSrcImage(b)
	px := float64(img.Bounds().Dx() * img.Bounds().Dy())
	ctx := context.Background()
	for _, name := range []string{"nes", "lego"} {
		pal := benchPalette(b, name)
		lut := pal.NewFastLUT()
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := pal.Apply(ctx, img, ApplyOptions{FastLUT: lut}); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/px, "ns/px")
		})
	}
}
