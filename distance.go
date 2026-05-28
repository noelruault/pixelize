package pixelize

import "image/color"

// DistanceFunc returns a distance metric between two colors.
// Smaller means more similar. Absolute scale does not matter,
// only relative ordering.
//
// The default implementation is stdlib unweighted Euclidean
// (via color.Palette.Index). Callers that need perceptual accuracy
// can plug in CIEDE2000 over CIE Lab.
type DistanceFunc func(a, b color.Color) float64

// EuclideanRGBA is the stdlib-equivalent unweighted Euclidean
// distance in 16-bit RGBA space.
func EuclideanRGBA(a, b color.Color) float64 {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	dr := float64(ar) - float64(br)
	dg := float64(ag) - float64(bg)
	db := float64(ab) - float64(bb)
	da := float64(aa) - float64(ba)
	return dr*dr + dg*dg + db*db + da*da
}
