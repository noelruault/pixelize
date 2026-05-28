// Package pixelize resizes images and quantizes their colors to a palette.
//
// Two layers:
//
//   - Palette[M] models an ordered set of color entries with arbitrary
//     typed metadata M (name, ID, hex, whatever a domain needs).
//   - Apply takes an image and returns a Pattern[M]: the quantized image
//     plus the palette index per pixel and a usage histogram.
//
// Distance defaults to stdlib unweighted Euclidean (image/color.Palette.Index).
// Callers can plug in a custom DistanceFunc.
package pixelize

// Version is set at build time via -ldflags.
var Version = "dev"
