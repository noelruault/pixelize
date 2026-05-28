package pixelize

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"io"
)

// LoopMode controls how progressive GIF frames repeat.
type LoopMode int

const (
	// LoopNone plays the frames once in order.
	LoopNone LoopMode = iota

	// LoopReverse reverses the frame order (no forward play).
	LoopReverse

	// LoopFull plays forward then reverse (smooth back-and-forth).
	LoopFull
)

// GIFOptions configures EncodeGIF.
type GIFOptions struct {
	// DelayMS is the per-frame delay in milliseconds. GIF resolution
	// is 10ms, so values are floored to the nearest centisecond.
	// Default 200ms.
	DelayMS int

	// Loop controls frame ordering.
	Loop LoopMode

	// LoopCount sets the GIF loop count (0 = infinite).
	LoopCount int
}

// EncodeAnimatedGIF writes frames as an animated GIF preserving per-
// frame delays and a loop count. Use this when source frames carry
// their own timing (e.g. converted from another animated GIF).
// DelaysCS values are centiseconds, matching gif.GIF.Delay.
func EncodeAnimatedGIF(w io.Writer, frames []*image.RGBA, delaysCS []int, loopCount int) error {
	if len(frames) == 0 {
		return fmt.Errorf("no frames")
	}
	if len(delaysCS) != len(frames) {
		return fmt.Errorf("delays length %d != frames length %d", len(delaysCS), len(frames))
	}
	bounds := frames[0].Bounds()
	g := &gif.GIF{LoopCount: loopCount}
	for i, f := range frames {
		pal := framePalette(f)
		paletted := image.NewPaletted(bounds, pal)
		draw.Draw(paletted, bounds, f, bounds.Min, draw.Src)
		d := delaysCS[i]
		if d < 1 {
			d = 1
		}
		g.Image = append(g.Image, paletted)
		g.Delay = append(g.Delay, d)
	}
	return gif.EncodeAll(w, g)
}

// EncodeGIF writes the frames as an animated GIF. Each frame is
// re-paletted against its own colors via image.NewPaletted so the
// caller does not need to pre-quantize. Output size matches the first
// frame's bounds; subsequent frames are clipped to that rectangle.
func EncodeGIF(w io.Writer, frames []*image.RGBA, opts GIFOptions) error {
	if len(frames) == 0 {
		return fmt.Errorf("no frames")
	}
	if opts.DelayMS <= 0 {
		opts.DelayMS = 200
	}

	switch opts.Loop {
	case LoopReverse:
		frames = reversed(frames)
	case LoopFull:
		frames = append(frames, reversed(frames[:len(frames)-1])...)
	}

	bounds := frames[0].Bounds()
	g := &gif.GIF{LoopCount: opts.LoopCount}
	delay := opts.DelayMS / 10
	if delay < 1 {
		delay = 1
	}

	for _, f := range frames {
		pal := framePalette(f)
		paletted := image.NewPaletted(bounds, pal)
		draw.Draw(paletted, bounds, f, bounds.Min, draw.Src)
		g.Image = append(g.Image, paletted)
		g.Delay = append(g.Delay, delay)
	}

	return gif.EncodeAll(w, g)
}

// framePalette samples up to 256 distinct colors from the frame.
// Sufficient for already-quantized output. For non-quantized input
// the caller should use Pattern frames or stdlib palette.Plan9.
func framePalette(img *image.RGBA) color.Palette {
	seen := map[color.RGBA]struct{}{}
	pal := make(color.Palette, 0, 256)
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if _, ok := seen[c]; ok {
				continue
			}
			seen[c] = struct{}{}
			pal = append(pal, c)
			if len(pal) == 256 {
				return pal
			}
		}
	}
	if len(pal) == 0 {
		pal = append(pal, color.RGBA{})
	}
	return pal
}

func reversed[T any](in []T) []T {
	out := make([]T, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}
