package decode

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestReaderPNG(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			src.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, src); err != nil {
		t.Fatal(err)
	}
	img, format, err := Reader(&b)
	if err != nil {
		t.Fatal(err)
	}
	if format != "png" {
		t.Fatalf("format = %q", format)
	}
	if img.Bounds().Dx() != 4 {
		t.Fatalf("bounds: %v", img.Bounds())
	}
}

func TestFilePNG(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.SetRGBA(0, 0, color.RGBA{1, 2, 3, 255})

	p := filepath.Join(t.TempDir(), "test.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, src); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	img, format, err := File(p)
	if err != nil {
		t.Fatal(err)
	}
	if format != "png" || img.Bounds().Dx() != 2 {
		t.Fatalf("got format=%q bounds=%v", format, img.Bounds())
	}
}
