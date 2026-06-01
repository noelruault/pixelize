package pixelize

import "sort"

// kdExactMinPaletteSize is the palette size at or above which the exact
// kd-tree beats the parallel-linear scan for the default (stdlib) metric.
// Below it, the linear scan's tight branch-free inner loop wins; at or above
// it the kd-tree's sub-linear node visits win and the margin grows with P.
// Pinned at 128 by research report 09 (the first P where kd wins on every
// image size in every run; 96 is the noisy borderline).
const kdExactMinPaletteSize = 128

// kdNode is a 3-D (RGB) kd-tree node. The color is stored inline (not an
// index into the palette) so the hot distance computation stays cache-local.
// idx is the original palette index, carried so the search can break
// equidistant ties toward the lowest index, exactly as color.Palette.Index
// does.
type kdNode struct {
	r, g, b     uint8
	idx, axis   int
	left, right *kdNode
}

// kdTree is a 3-D RGB kd-tree over a palette. It reproduces
// color.Palette.Index bit-for-bit for OPAQUE 8-bit colors: the 8-bit and
// 16-bit squared-distance orderings coincide (16-bit is the 8-bit value times
// a constant), the alpha term cancels when every alpha is 0xff, and the
// lowest-index tie-break matches the stdlib first-match argmin. Callers must
// gate on an opaque source; with non-opaque alpha the alpha term no longer
// cancels and this is not bit-identical.
type kdTree struct {
	root  *kdNode
	nodes []kdNode // contiguous backing; see buildKDTree for the cap invariant
}

type kdPt struct {
	r, g, b uint8
	idx     int
}

func kdAxisVal(p kdPt, axis int) int32 {
	switch axis {
	case 0:
		return int32(p.r)
	case 1:
		return int32(p.g)
	default:
		return int32(p.b)
	}
}

// buildKDTree builds the median-split, axis=depth%3 tree that report 09
// found robustly fastest. Build shape affects only speed, never the answer:
// correctness is re-derived in the search from geometry plus the index
// tie-break.
//
// Invariant: nodes is allocated with cap == len(p) and exactly len(p) nodes
// are appended (one per palette entry), so the backing array never reallocates
// and the &t.nodes[i] child pointers stay valid for the tree's lifetime.
func buildKDTree[M any](p Palette[M]) *kdTree {
	pts := make([]kdPt, len(p))
	for i, e := range p {
		pts[i] = kdPt{e.R, e.G, e.B, i}
	}
	t := &kdTree{nodes: make([]kdNode, 0, len(p))}

	var build func(items []kdPt, depth int) *kdNode
	build = func(items []kdPt, depth int) *kdNode {
		if len(items) == 0 {
			return nil
		}
		axis := depth % 3
		sort.Slice(items, func(a, b int) bool {
			va, vb := kdAxisVal(items[a], axis), kdAxisVal(items[b], axis)
			if va != vb {
				return va < vb
			}
			return items[a].idx < items[b].idx // stable on index
		})
		mid := len(items) / 2
		it := items[mid]
		t.nodes = append(t.nodes, kdNode{r: it.r, g: it.g, b: it.b, idx: it.idx, axis: axis})
		n := &t.nodes[len(t.nodes)-1]
		n.left = build(items[:mid], depth+1)
		n.right = build(items[mid+1:], depth+1)
		return n
	}
	t.root = build(pts, 0)
	return t
}

// kdSearch is per-query (and per-worker) scratch for a nearest-color lookup.
type kdSearch struct {
	tr, tg, tb int32
	bestD      int32
	bestIdx    int
}

func (s *kdSearch) search(n *kdNode) {
	if n == nil {
		return
	}
	dr := s.tr - int32(n.r)
	dg := s.tg - int32(n.g)
	db := s.tb - int32(n.b)
	d := dr*dr + dg*dg + db*db
	// Lowest-index tie-break: on an equidistant tie keep the smaller palette
	// index, matching color.Palette.Index's first-match argmin.
	if d < s.bestD || (d == s.bestD && n.idx < s.bestIdx) {
		s.bestD = d
		s.bestIdx = n.idx
	}
	var diff int32
	switch n.axis {
	case 0:
		diff = dr
	case 1:
		diff = dg
	default:
		diff = db
	}
	var near, far *kdNode
	if diff < 0 {
		near, far = n.left, n.right
	} else {
		near, far = n.right, n.left
	}
	s.search(near)
	// `<=` prune: when the splitting plane is exactly bestD away, the far side
	// can still hold an EQUIDISTANT point with a smaller palette index.
	// Visiting it is mandatory for bit-identical tie-breaking.
	if diff*diff <= s.bestD {
		s.search(far)
	}
}

// match returns the nearest palette index for the opaque RGB color (r,g,b),
// bit-identical to color.Palette.Index. s is reused across calls.
func (t *kdTree) match(s *kdSearch, r, g, b uint8) int {
	s.tr, s.tg, s.tb = int32(r), int32(g), int32(b)
	s.bestD = 1 << 30
	s.bestIdx = 1 << 30 // sentinel: first visited node always wins
	s.search(t.root)
	return s.bestIdx
}
