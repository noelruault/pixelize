package pixelize

import (
	"image/color"
	"testing"
)

func TestSwatch(t *testing.T) {
	pal := Palette[struct{}]{
		{R: 255, G: 0, B: 0},
		{R: 0, G: 255, B: 0},
		{R: 0, G: 0, B: 255},
	}
	out := pal.Swatch(SwatchOptions{CellSize: 16, Columns: 3})
	if out.Bounds().Dx() != 48 || out.Bounds().Dy() != 16 {
		t.Fatalf("bounds: %v", out.Bounds())
	}
	got := color.RGBAModel.Convert(out.At(0, 0)).(color.RGBA)
	if got.R != 255 {
		t.Fatalf("top-left should be red: %+v", got)
	}
}

func TestSwatchWrapsRows(t *testing.T) {
	pal := make(Palette[struct{}], 9)
	out := pal.Swatch(SwatchOptions{CellSize: 8, Columns: 4})
	if out.Bounds().Dx() != 32 || out.Bounds().Dy() != 24 {
		t.Fatalf("bounds: %v want 32x24 (4 cols, ceil(9/4)=3 rows)", out.Bounds())
	}
}
