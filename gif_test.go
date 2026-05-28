package pixelize

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"
)

func newFrame(c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestEncodeGIFLoopNone(t *testing.T) {
	frames := []*image.RGBA{
		newFrame(color.RGBA{255, 0, 0, 255}),
		newFrame(color.RGBA{0, 255, 0, 255}),
		newFrame(color.RGBA{0, 0, 255, 255}),
	}
	var b bytes.Buffer
	if err := EncodeGIF(&b, frames, GIFOptions{DelayMS: 100}); err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(&b)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 3 {
		t.Fatalf("frames = %d, want 3", len(g.Image))
	}
}

func TestEncodeGIFLoopFull(t *testing.T) {
	frames := []*image.RGBA{
		newFrame(color.RGBA{1, 0, 0, 255}),
		newFrame(color.RGBA{2, 0, 0, 255}),
		newFrame(color.RGBA{3, 0, 0, 255}),
	}
	var b bytes.Buffer
	if err := EncodeGIF(&b, frames, GIFOptions{Loop: LoopFull}); err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(&b)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 5 {
		t.Fatalf("frames = %d, want 5 (3 forward + 2 reverse-tail)", len(g.Image))
	}
}

func TestEncodeGIFRejectsEmpty(t *testing.T) {
	var b bytes.Buffer
	if err := EncodeGIF(&b, nil, GIFOptions{}); err == nil {
		t.Fatal("want error on empty frames")
	}
}

func TestEncodeAnimatedGIFPreservesDelays(t *testing.T) {
	frames := []*image.RGBA{
		newFrame(color.RGBA{1, 0, 0, 255}),
		newFrame(color.RGBA{2, 0, 0, 255}),
		newFrame(color.RGBA{3, 0, 0, 255}),
	}
	delays := []int{7, 4, 12}
	var b bytes.Buffer
	if err := EncodeAnimatedGIF(&b, frames, delays, 0); err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(&b)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 3 {
		t.Fatalf("frames = %d, want 3", len(g.Image))
	}
	for i, want := range delays {
		if g.Delay[i] != want {
			t.Fatalf("delay[%d] = %d, want %d", i, g.Delay[i], want)
		}
	}
}

func TestEncodeAnimatedGIFRejectsMismatchedDelays(t *testing.T) {
	frames := []*image.RGBA{newFrame(color.RGBA{1, 2, 3, 255})}
	var b bytes.Buffer
	if err := EncodeAnimatedGIF(&b, frames, []int{5, 10}, 0); err == nil {
		t.Fatal("want error on delay/frame length mismatch")
	}
}
