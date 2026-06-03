package quantize

import (
	"image"
	"math"
	"sort"
)

// cluster.go — the validated pipeline: a deterministic weighted histogram, a
// PCA-divisive initializer (or the optional space-filling-curve initializer),
// and a weighted Lloyd (k-means) refine. All operate in a chosen space.

// wcolor is one distinct opaque color and its pixel weight.
type wcolor struct {
	r, g, b uint8
	w       float64
}

// histogram returns the image's distinct opaque colors with weights, sorted
// into a canonical order. The sort is load-bearing: it makes every downstream
// step deterministic. (A Go map's iteration order is randomized per run, so
// summation order — and thus seeded/iterative results — would otherwise drift;
// research report 05.)
func histogram(img image.Image) []wcolor {
	b := img.Bounds()
	counts := make(map[uint32]int, 1<<14)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bb, _ := img.At(x, y).RGBA()
			counts[uint32(r>>8)<<16|uint32(g>>8)<<8|uint32(bb>>8)]++
		}
	}
	out := make([]wcolor, 0, len(counts))
	for k, c := range counts {
		out = append(out, wcolor{uint8(k >> 16), uint8(k >> 8), uint8(k), float64(c)})
	}
	sort.Slice(out, func(i, j int) bool {
		ki := uint32(out[i].r)<<16 | uint32(out[i].g)<<8 | uint32(out[i].b)
		kj := uint32(out[j].r)<<16 | uint32(out[j].g)<<8 | uint32(out[j].b)
		return ki < kj
	})
	return out
}

type wpoint struct {
	p vec3
	w float64
}

func points(h []wcolor, sp space) []wpoint {
	out := make([]wpoint, len(h))
	for i, c := range h {
		out[i] = wpoint{p: sp.fromRGB(c.r, c.g, c.b), w: c.w}
	}
	return out
}

// --- PCA-divisive initializer ------------------------------------------------

type pbox struct {
	pts  []wpoint
	w    float64
	mean vec3
	sse  float64
}

func newPBox(pts []wpoint) pbox {
	var w float64
	var sum vec3
	for _, p := range pts {
		w += p.w
		sum = sum.add(p.p.scale(p.w))
	}
	b := pbox{pts: pts, w: w}
	if w > 0 {
		b.mean = sum.scale(1.0 / w)
	}
	for _, p := range pts {
		b.sse += p.w * p.p.dist2(b.mean)
	}
	return b
}

// divisiveInit splits the largest-SSE box along its principal axis at the
// weighted median, until n boxes exist; each box's weighted mean is a centroid.
func divisiveInit(pts []wpoint, n int) []vec3 {
	boxes := []pbox{newPBox(pts)}
	for len(boxes) < n {
		best := -1
		for i := range boxes {
			if len(boxes[i].pts) < 2 {
				continue
			}
			if best < 0 || boxes[i].sse > boxes[best].sse {
				best = i
			}
		}
		if best < 0 {
			break
		}
		a, b := splitBox(boxes[best])
		if len(a.pts) == 0 || len(b.pts) == 0 {
			boxes[best].pts = boxes[best].pts[:1]
			continue
		}
		boxes[best] = a
		boxes = append(boxes, b)
	}
	cents := make([]vec3, len(boxes))
	for i, bx := range boxes {
		cents[i] = bx.mean
	}
	return cents
}

func splitBox(b pbox) (pbox, pbox) {
	axis := principalAxis(b.pts, b.mean)
	proj := func(p vec3) float64 {
		return (p[0]-b.mean[0])*axis[0] + (p[1]-b.mean[1])*axis[1] + (p[2]-b.mean[2])*axis[2]
	}
	sort.Slice(b.pts, func(i, j int) bool { return proj(b.pts[i].p) < proj(b.pts[j].p) })
	half := b.w / 2
	acc, cut := 0.0, 1
	for i, p := range b.pts {
		acc += p.w
		if acc >= half {
			cut = i + 1
			break
		}
	}
	if cut < 1 {
		cut = 1
	}
	if cut >= len(b.pts) {
		cut = len(b.pts) - 1
	}
	return newPBox(b.pts[:cut]), newPBox(b.pts[cut:])
}

// principalAxis is the top eigenvector of the weighted covariance (power
// iteration) — the direction of greatest spread.
func principalAxis(pts []wpoint, mean vec3) vec3 {
	var cxx, cxy, cxz, cyy, cyz, czz, w float64
	for _, p := range pts {
		dx, dy, dz := p.p[0]-mean[0], p.p[1]-mean[1], p.p[2]-mean[2]
		cxx += p.w * dx * dx
		cxy += p.w * dx * dy
		cxz += p.w * dx * dz
		cyy += p.w * dy * dy
		cyz += p.w * dy * dz
		czz += p.w * dz * dz
		w += p.w
	}
	if w > 0 {
		cxx, cxy, cxz, cyy, cyz, czz = cxx/w, cxy/w, cxz/w, cyy/w, cyz/w, czz/w
	}
	v := vec3{1, 1, 1}
	for it := 0; it < 32; it++ {
		nv := vec3{
			cxx*v[0] + cxy*v[1] + cxz*v[2],
			cxy*v[0] + cyy*v[1] + cyz*v[2],
			cxz*v[0] + cyz*v[1] + czz*v[2],
		}
		norm := math.Sqrt(nv[0]*nv[0] + nv[1]*nv[1] + nv[2]*nv[2])
		if norm == 0 {
			return vec3{1, 0, 0}
		}
		v = nv.scale(1.0 / norm)
	}
	return v
}

// --- space-filling-curve initializer (optional) ------------------------------

// curveInit sorts colors along a Morton (Z-order) space-filling curve and cuts
// the run into n equal-weight segments, giving a spatially uniform centroid
// seed. Research (report 09) found this beats the divisive seed only at large
// palettes (N>=256); below that it is neutral-to-worse. Opt-in via Options.
func curveInit(pts []wpoint, n int) []vec3 {
	type item struct {
		key uint32
		p   vec3
		w   float64
	}
	items := make([]item, len(pts))
	var total float64
	for i, wp := range pts {
		items[i] = item{key: morton3(wp.p), p: wp.p, w: wp.w}
		total += wp.w
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].key != items[j].key {
			return items[i].key < items[j].key
		}
		return false
	})
	cents := make([]vec3, 0, n)
	seg := total / float64(n)
	thresh := seg
	var sum vec3
	var segW, cum float64
	for _, it := range items {
		sum = sum.add(it.p.scale(it.w))
		segW += it.w
		cum += it.w
		if cum >= thresh && len(cents) < n-1 {
			cents = append(cents, sum.scale(1.0/segW))
			sum = vec3{}
			segW = 0
			thresh += seg
		}
	}
	if segW > 0 {
		cents = append(cents, sum.scale(1.0/segW))
	}
	return cents
}

// morton3 interleaves the three channels (quantized to 8 bits each in the
// working space) into a 24-bit Z-order key.
func morton3(v vec3) uint32 {
	q := func(f float64) uint8 { return clamp8(f) }
	// Map working-space coordinates to 0..255. RGB is already in range; OKLab
	// L is ~[0,1] and a,b ~[-0.4,0.4], so normalize generously.
	var r, g, b uint8
	if v[0] >= 0 && v[0] <= 255 && v[1] >= 0 && v[1] <= 255 && v[2] >= 0 && v[2] <= 255 {
		r, g, b = q(v[0]), q(v[1]), q(v[2]) // RGB space
	} else {
		r = clamp8(v[0] * 255)
		g = clamp8((v[1] + 0.4) / 0.8 * 255)
		b = clamp8((v[2] + 0.4) / 0.8 * 255)
	}
	var d uint32
	for i := uint(0); i < 8; i++ {
		d |= uint32((r>>i)&1) << (3*i + 0)
		d |= uint32((g>>i)&1) << (3*i + 1)
		d |= uint32((b>>i)&1) << (3*i + 2)
	}
	return d
}

// --- weighted Lloyd refine ---------------------------------------------------

// refine runs weighted k-means for iters passes. Assignment is nearest-centroid
// (the same primitive pixelize's matcher provides); over a sorted histogram the
// summation order is fixed, so the result is deterministic.
func refine(pts []wpoint, cents []vec3, iters int) []vec3 {
	for it := 0; it < iters; it++ {
		sums := make([]vec3, len(cents))
		wts := make([]float64, len(cents))
		for _, wp := range pts {
			j := nearest(wp.p, cents)
			sums[j] = sums[j].add(wp.p.scale(wp.w))
			wts[j] += wp.w
		}
		for j := range cents {
			if wts[j] > 0 {
				cents[j] = sums[j].scale(1.0 / wts[j])
			}
		}
	}
	return cents
}

func nearest(p vec3, cents []vec3) int {
	best, bestD := 0, p.dist2(cents[0])
	for j := 1; j < len(cents); j++ {
		if d := p.dist2(cents[j]); d < bestD {
			best, bestD = j, d
		}
	}
	return best
}
