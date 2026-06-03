package quantize

import (
	"image/color"
	"math"

	"github.com/noelruault/pixelize"
)

// space.go — the color spaces palette selection runs in. Research found the
// right space depends on palette density: RGB for small palettes (entries far
// apart, large color differences), OKLab for large ones (entries close, small
// differences — OKLab's perceptually-uniform regime). Euclidean distance in
// OKLab is still Euclidean, so it composes with pixelize's exact matcher; the
// only change is the coordinates.
//
// See noelruault/research/quantization reports 02, 05, 09, 11.

type vec3 [3]float64

func (a vec3) add(b vec3) vec3      { return vec3{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func (a vec3) scale(s float64) vec3 { return vec3{a[0] * s, a[1] * s, a[2] * s} }
func (a vec3) dist2(b vec3) float64 {
	d0, d1, d2 := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return d0*d0 + d1*d1 + d2*d2
}

// space maps 8-bit sRGB to a working vector space and back.
type space interface {
	fromRGB(r, g, b uint8) vec3
	toRGBA(v vec3) color.RGBA
}

func clamp8(f float64) uint8 {
	i := int(math.Round(f))
	switch {
	case i < 0:
		return 0
	case i > 255:
		return 255
	default:
		return uint8(i)
	}
}

// rgbSpace: plain 8-bit RGB as floats.
type rgbSpace struct{}

func (rgbSpace) fromRGB(r, g, b uint8) vec3 { return vec3{float64(r), float64(g), float64(b)} }
func (rgbSpace) toRGBA(v vec3) color.RGBA {
	return color.RGBA{R: clamp8(v[0]), G: clamp8(v[1]), B: clamp8(v[2]), A: 255}
}

func srgbToLinear(c uint8) float64 {
	v := float64(c) / 255.0
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func linearToSRGB(c float64) float64 {
	if c <= 0.0031308 {
		return 12.92 * c
	}
	return 1.055*math.Pow(c, 1.0/2.4) - 0.055
}

// oklabSpace: Björn Ottosson's OKLab (2020). Cheap, perceptually uniform for
// small color differences, which is why it wins at large palettes.
type oklabSpace struct{}

func (oklabSpace) fromRGB(r, g, b uint8) vec3 {
	rl, gl, bl := srgbToLinear(r), srgbToLinear(g), srgbToLinear(b)
	l := 0.4122214708*rl + 0.5363325363*gl + 0.0514459929*bl
	m := 0.2119034982*rl + 0.6806995451*gl + 0.1073969566*bl
	s := 0.0883024619*rl + 0.2817188376*gl + 0.6299787005*bl
	l_, m_, s_ := math.Cbrt(l), math.Cbrt(m), math.Cbrt(s)
	return vec3{
		0.2104542553*l_ + 0.7936177850*m_ - 0.0040720468*s_,
		1.9779984951*l_ - 2.4285922050*m_ + 0.4505937099*s_,
		0.0259040371*l_ + 0.7827717662*m_ - 0.8086757660*s_,
	}
}

func (oklabSpace) toRGBA(v vec3) color.RGBA {
	L, A, B := v[0], v[1], v[2]
	l_ := L + 0.3963377774*A + 0.2158037573*B
	m_ := L - 0.1055613458*A - 0.0638541728*B
	s_ := L - 0.0894841775*A - 1.2914855480*B
	l, m, s := l_*l_*l_, m_*m_*m_, s_*s_*s_
	rl := +4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	gl := -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	bl := -0.0041960863*l - 0.7034186147*m + 1.7076147010*s
	return color.RGBA{
		R: clamp8(255 * linearToSRGB(rl)),
		G: clamp8(255 * linearToSRGB(gl)),
		B: clamp8(255 * linearToSRGB(bl)),
		A: 255,
	}
}

// OKLabDistance is a pixelize.DistanceFunc that measures squared Euclidean
// distance in OKLab. Pass it as ApplyOptions.Distance to assign pixels in the
// same space an OKLab palette was built in (the "matched assignment" that makes
// the perceptual quality hold through to the output). Generate returns the
// matching DistanceFunc in its Result, so callers normally don't construct this
// by hand.
func OKLabDistance(a, b color.Color) float64 {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	var sp oklabSpace
	av := sp.fromRGB(uint8(ar>>8), uint8(ag>>8), uint8(ab>>8))
	bv := sp.fromRGB(uint8(br>>8), uint8(bg>>8), uint8(bb>>8))
	return av.dist2(bv)
}

// ensure the package's distance matches the engine's type.
var _ pixelize.DistanceFunc = OKLabDistance
