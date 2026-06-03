package quantize

import (
	"fmt"
	"testing"

	"github.com/noelruault/pixelize"
)

// BenchmarkGenerate is the in-engine speed benchmark: it measures palette
// derivation at the palette sizes that matter, in both spaces (n<=48 RGB,
// n>48 OKLab under SpaceAuto), on a 256x256 image. Run with:
//
//	go test ./quantize -bench=Generate -benchmem
func BenchmarkGenerate(b *testing.B) {
	img := gradient(256, 256)
	for _, n := range []int{4, 16, 64, 256} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := Generate(img, n, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
	// Curve-init variant at the size where it's hinted.
	b.Run("n=256/curve", func(b *testing.B) {
		opts := &Options{CurveInit: true}
		for i := 0; i < b.N; i++ {
			if _, err := Generate(img, 256, opts); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestMerge(t *testing.T) {
	// Three colors: two near-identical (dist 3) and one far. Merge at 8 should
	// collapse the near pair to 2 colors; at 1 it should leave all 3.
	pal := pixelize.Palette[pixelize.EntryMeta]{
		{R: 10, G: 10, B: 10, Meta: pixelize.EntryMeta{Name: "a"}},
		{R: 12, G: 11, B: 11, Meta: pixelize.EntryMeta{Name: "b"}},
		{R: 200, G: 200, B: 200, Meta: pixelize.EntryMeta{Name: "c"}},
	}
	if got := len(Merge(pal, 8)); got != 2 {
		t.Errorf("Merge(8): got %d colors, want 2", got)
	}
	if got := len(Merge(pal, 1)); got != 3 {
		t.Errorf("Merge(1): got %d colors, want 3 (nothing within threshold)", got)
	}
	// Determinism + metadata of the kept (lower-index) entry.
	m := Merge(pal, 8)
	if m[0].Meta.Name != "a" {
		t.Errorf("merged entry should keep lower-index metadata, got %q", m[0].Meta.Name)
	}
	if len(Merge(pal, 0)) != 3 {
		t.Error("Merge(0) should be a no-op")
	}
}
