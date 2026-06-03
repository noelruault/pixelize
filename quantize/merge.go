package quantize

import (
	"math"

	"github.com/noelruault/pixelize"
)

// Merge collapses palette entries that are closer than threshold, repeatedly
// merging the single closest pair until no pair is within threshold — the
// "merge similar colors" operation. Distance is 8-bit RGB Euclidean (so a
// threshold of 8 means "within 8 levels"), which is intuitive and palette-
// agnostic; it works on a derived palette or a loaded one (e.g. trimming
// near-duplicate bricks so a parts list is buildable).
//
// A merged entry takes the midpoint color and keeps the lower-indexed entry's
// metadata. Order is deterministic. It is generic over the metadata type, so it
// composes with any pixelize.Palette.
func Merge[M any](pal pixelize.Palette[M], threshold float64) pixelize.Palette[M] {
	if threshold <= 0 || len(pal) < 2 {
		return pal
	}
	// Work on a mutable copy.
	entries := make([]pixelize.Entry[M], len(pal))
	copy(entries, pal)
	alive := make([]bool, len(entries))
	for i := range alive {
		alive[i] = true
	}
	count := len(entries)
	thr2 := threshold * threshold

	for count > 1 {
		// Find the closest alive pair (lowest indices break ties).
		bi, bj, best := -1, -1, math.Inf(1)
		for i := 0; i < len(entries); i++ {
			if !alive[i] {
				continue
			}
			for j := i + 1; j < len(entries); j++ {
				if !alive[j] {
					continue
				}
				if d := rgbDist2(entries[i], entries[j]); d < best {
					best, bi, bj = d, i, j
				}
			}
		}
		if bi < 0 || best >= thr2 {
			break // nothing left within threshold
		}
		// Merge bj into bi: midpoint color, keep bi's metadata.
		entries[bi].R = uint8((int(entries[bi].R) + int(entries[bj].R) + 1) / 2)
		entries[bi].G = uint8((int(entries[bi].G) + int(entries[bj].G) + 1) / 2)
		entries[bi].B = uint8((int(entries[bi].B) + int(entries[bj].B) + 1) / 2)
		alive[bj] = false
		count--
	}

	out := make(pixelize.Palette[M], 0, count)
	for i, ok := range alive {
		if ok {
			out = append(out, entries[i])
		}
	}
	return out
}

func rgbDist2[M any](a, b pixelize.Entry[M]) float64 {
	dr := float64(a.R) - float64(b.R)
	dg := float64(a.G) - float64(b.G)
	db := float64(a.B) - float64(b.B)
	return dr*dr + dg*dg + db*db
}
