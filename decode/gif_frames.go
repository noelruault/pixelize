package decode

import (
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"os"
)

// AnimatedGIF holds the result of decoding every frame of an animated
// GIF with disposal methods applied, so each frame is the full
// composed image you'd see at that moment.
type AnimatedGIF struct {
	Frames    []*image.RGBA // composed, one per source frame
	DelaysCS  []int         // per-frame delay in centiseconds
	LoopCount int           // gif.GIF.LoopCount (0 = forever)
}

// AllFramesGIF decodes every frame of an animated GIF, composing
// disposal methods. Use this when you want to process each visible
// frame, not just the raw delta layers stdlib returns.
func AllFramesGIF(path string) (*AnimatedGIF, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	g, err := gif.DecodeAll(f)
	if err != nil {
		return nil, err
	}

	bounds := image.Rect(0, 0, g.Config.Width, g.Config.Height)
	canvas := image.NewRGBA(bounds)

	var (
		frames []*image.RGBA
		prev   *image.RGBA // for DisposalPrevious
	)

	for i, frame := range g.Image {
		// Apply the PREVIOUS frame's disposal before drawing the current.
		if i > 0 {
			switch g.Disposal[i-1] {
			case gif.DisposalBackground:
				clearRect(canvas, g.Image[i-1].Bounds())
			case gif.DisposalPrevious:
				if prev != nil {
					draw.Draw(canvas, bounds, prev, image.Point{}, draw.Src)
				}
			}
		}
		if g.Disposal[i] == gif.DisposalPrevious {
			snap := image.NewRGBA(bounds)
			draw.Draw(snap, bounds, canvas, image.Point{}, draw.Src)
			prev = snap
		}

		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)

		snapshot := image.NewRGBA(bounds)
		draw.Draw(snapshot, bounds, canvas, image.Point{}, draw.Src)
		frames = append(frames, snapshot)
	}

	return &AnimatedGIF{
		Frames:    frames,
		DelaysCS:  append([]int(nil), g.Delay...),
		LoopCount: g.LoopCount,
	}, nil
}

func clearRect(dst *image.RGBA, r image.Rectangle) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			dst.SetRGBA(x, y, color.RGBA{})
		}
	}
}
