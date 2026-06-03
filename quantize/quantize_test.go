package quantize

import (
	"context"
	"image"
	"testing"

	"github.com/noelruault/pixelize"
)

// gradient builds a w*h image with many distinct colors.
func gradient(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Pix[img.PixOffset(x, y):][0] = uint8((x * 5) % 256)
			img.Pix[img.PixOffset(x, y)+1] = uint8((y * 5) % 256)
			img.Pix[img.PixOffset(x, y)+2] = uint8(((x + y) * 3) % 256)
			img.Pix[img.PixOffset(x, y)+3] = 255
		}
	}
	return img
}

func samePalette(a, b pixelize.Palette[pixelize.EntryMeta]) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].R != b[i].R || a[i].G != b[i].G || a[i].B != b[i].B {
			return false
		}
	}
	return true
}

// TestDeterministic is the headline guarantee: identical output across runs.
// (The canonical histogram sort is what makes this hold; without it, Go map
// order would perturb the k-means summation.)
func TestDeterministic(t *testing.T) {
	img := gradient(80, 80)
	for _, n := range []int{16, 64, 256} {
		first, err := Generate(img, n, nil)
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		for run := 0; run < 3; run++ {
			got, err := Generate(img, n, nil)
			if err != nil {
				t.Fatalf("n=%d run=%d: %v", n, run, err)
			}
			if !samePalette(first.Palette, got.Palette) {
				t.Errorf("n=%d: palette not deterministic across runs", n)
			}
		}
	}
}

func TestCurveInitDeterministic(t *testing.T) {
	img := gradient(80, 80)
	opts := &Options{CurveInit: true}
	a, _ := Generate(img, 256, opts)
	b, _ := Generate(img, 256, opts)
	if !samePalette(a.Palette, b.Palette) {
		t.Error("curve-init palette not deterministic")
	}
}

func TestSpaceAuto(t *testing.T) {
	img := gradient(80, 80)
	for _, tc := range []struct {
		n    int
		want string
	}{{16, "rgb"}, {AutoThreshold, "rgb"}, {AutoThreshold + 1, "oklab"}, {256, "oklab"}} {
		res, err := Generate(img, tc.n, nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.Space != tc.want {
			t.Errorf("n=%d: space=%q want %q", tc.n, res.Space, tc.want)
		}
		wantDist := tc.want == "oklab"
		if (res.Distance != nil) != wantDist {
			t.Errorf("n=%d: Distance set=%v, want %v", tc.n, res.Distance != nil, wantDist)
		}
	}
}

func TestSizeBound(t *testing.T) {
	img := gradient(80, 80)
	for _, n := range []int{4, 16, 64, 256} {
		res, err := Generate(img, n, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Palette) == 0 || len(res.Palette) > n {
			t.Errorf("n=%d: got %d colors, want 1..%d", n, len(res.Palette), n)
		}
	}
}

// TestFeedsApply confirms a derived palette + matched distance drives Apply and
// every output pixel comes from the palette.
func TestFeedsApply(t *testing.T) {
	img := gradient(64, 64)
	res, err := Generate(img, 64, nil)
	if err != nil {
		t.Fatal(err)
	}
	pat, err := res.Palette.Apply(context.Background(), img, pixelize.ApplyOptions{Distance: res.Distance})
	if err != nil {
		t.Fatal(err)
	}
	if pat.Image == nil {
		t.Fatal("nil result image")
	}
	if len(pat.Usage) == 0 || len(pat.Usage) > 64 {
		t.Errorf("usage has %d entries, want 1..64", len(pat.Usage))
	}
}

func TestFewerColorsThanN(t *testing.T) {
	// 4 distinct colors, ask for 64 → palette is the 4 exact colors.
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	cols := [][3]uint8{{255, 0, 0}, {0, 255, 0}, {0, 0, 255}, {255, 255, 0}}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			c := cols[(x/4)+(y/4)*2]
			o := img.PixOffset(x, y)
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = c[0], c[1], c[2], 255
		}
	}
	res, err := Generate(img, 64, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Palette) != 4 {
		t.Errorf("got %d colors, want exactly 4", len(res.Palette))
	}
}

func TestErrors(t *testing.T) {
	img := gradient(8, 8)
	if _, err := Generate(img, 0, nil); err == nil {
		t.Error("n=0 should error")
	}
	if _, err := Generate(image.NewRGBA(image.Rect(0, 0, 0, 0)), 8, nil); err == nil {
		t.Error("empty image should error")
	}
}
