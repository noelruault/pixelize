package pixelize

import (
	"context"
	"image"
	"image/color"
	"testing"
)

func TestApplyNearestRoundtrip(t *testing.T) {
	pal := Palette[string]{
		{R: 255, G: 0, B: 0, Meta: "red"},
		{R: 0, G: 255, B: 0, Meta: "green"},
		{R: 0, G: 0, B: 255, Meta: "blue"},
	}

	src := image.NewRGBA(image.Rect(0, 0, 3, 1))
	src.SetRGBA(0, 0, color.RGBA{250, 5, 5, 255})
	src.SetRGBA(1, 0, color.RGBA{10, 240, 10, 255})
	src.SetRGBA(2, 0, color.RGBA{0, 5, 250, 255})

	pat, err := pal.Apply(context.Background(), src, ApplyOptions{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if pat.Indices[0][0] != 0 || pat.Indices[1][0] != 1 || pat.Indices[2][0] != 2 {
		t.Fatalf("indices: %v", pat.Indices)
	}
	if pat.Usage[0] != 1 || pat.Usage[1] != 1 || pat.Usage[2] != 1 {
		t.Fatalf("usage: %v", pat.Usage)
	}
	r, g, b, _ := pat.Image.At(0, 0).RGBA()
	if r>>8 != 255 || g>>8 != 0 || b>>8 != 0 {
		t.Fatalf("pixel(0,0) = (%d,%d,%d), want (255,0,0)", r>>8, g>>8, b>>8)
	}
}

func TestApplyResize(t *testing.T) {
	pal := Palette[struct{}]{
		{R: 0, G: 0, B: 0},
		{R: 255, G: 255, B: 255},
	}
	src := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			src.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	pat, err := pal.Apply(context.Background(), src, ApplyOptions{
		Width: 10, Height: 10, Resize: NearestNeighbor,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if pat.Image.Bounds().Dx() != 10 || pat.Image.Bounds().Dy() != 10 {
		t.Fatalf("output size: %v", pat.Image.Bounds())
	}
	if pat.Usage[1] != 100 {
		t.Fatalf("usage: %v", pat.Usage)
	}
}

func TestEmptyPaletteRejected(t *testing.T) {
	var pal Palette[struct{}]
	_, err := pal.Apply(context.Background(), image.NewRGBA(image.Rect(0, 0, 1, 1)), ApplyOptions{})
	if err == nil {
		t.Fatal("want error on empty palette")
	}
}

func TestEuclideanRGBA(t *testing.T) {
	a := color.RGBA{255, 0, 0, 255}
	b := color.RGBA{0, 255, 0, 255}
	if d := EuclideanRGBA(a, a); d != 0 {
		t.Fatalf("self-distance = %v, want 0", d)
	}
	if EuclideanRGBA(a, b) <= 0 {
		t.Fatal("cross-distance should be > 0")
	}
}
