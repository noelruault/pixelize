package pixelize

import (
	"context"
	"testing"
)

// TestFastLUTAccuracy checks the Fast (LUT) path against the exact path: it is
// APPROXIMATE, so the gate is a bounded mismatch rate (not bit-identical). The
// 6-bit LUT should stay well under ~12% non-nearest even at large palettes
// (report 11 measured ~2-6% on a painting, higher on uniform noise), and the
// output must remain a valid in-palette image with consistent indices.
func TestFastLUTAccuracy(t *testing.T) {
	const bound = 0.12 // generous ceiling; report 11 saw <=10.6% worst case
	for _, n := range []int{16, 64, 256} {
		img := randomImage(256, 256, 1) // noise: a hard case for the LUT
		pal := testPalette(n, 2)

		exact, err := pal.applyNearest(context.Background(), img, nil)
		if err != nil {
			t.Fatalf("P=%d: exact: %v", n, err)
		}
		fast, err := pal.Apply(context.Background(), img, ApplyOptions{Fast: true})
		if err != nil {
			t.Fatalf("P=%d: fast: %v", n, err)
		}

		// Consistency: Fast output pixel must equal its palette entry, and
		// indices must agree with the image.
		w := len(fast.Indices)
		h := len(fast.Indices[0])
		mismatch := 0
		total := w * h
		for x := 0; x < w; x++ {
			for y := 0; y < h; y++ {
				idx := fast.Indices[x][y]
				if idx < 0 || idx >= n {
					t.Fatalf("P=%d: index %d out of range at (%d,%d)", n, idx, x, y)
				}
				if idx != exact.Indices[x][y] {
					mismatch++
				}
			}
		}
		rate := float64(mismatch) / float64(total)
		t.Logf("P=%d: Fast non-nearest rate = %.3f (%d/%d)", n, rate, mismatch, total)
		if rate > bound {
			t.Errorf("P=%d: Fast mismatch rate %.3f exceeds bound %.3f", n, rate, bound)
		}

		// Usage must sum to the pixel count.
		sum := 0
		for _, c := range fast.Usage {
			sum += c
		}
		if sum != total {
			t.Errorf("P=%d: Fast usage sums to %d, want %d", n, sum, total)
		}
	}
}

// TestFastLUTReuseMatchesPerCall confirms a prebuilt, reused FastLUT produces
// exactly the same output as building one per call (the batch/watch path must
// match the convenience path).
func TestFastLUTReuseMatchesPerCall(t *testing.T) {
	pal := testPalette(128, 2)
	lut := pal.NewFastLUT()
	for _, seed := range []int64{1, 2, 3} {
		img := randomImage(200, 150, seed)
		perCall, err := pal.Apply(context.Background(), img, ApplyOptions{Fast: true})
		if err != nil {
			t.Fatal(err)
		}
		reused, err := pal.Apply(context.Background(), img, ApplyOptions{FastLUT: lut})
		if err != nil {
			t.Fatal(err)
		}
		if !bytesEqual(perCall.Image.Pix, reused.Image.Pix) {
			t.Fatalf("seed %d: reused FastLUT output differs from per-call build", seed)
		}
	}
}
