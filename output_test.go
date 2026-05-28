package pixelize

import (
	"bytes"
	"context"
	"encoding/csv"
	"image"
	"image/color"
	"strings"
	"testing"
)

func sampleApply(t *testing.T) *Pattern[EntryMeta] {
	t.Helper()
	pal := Palette[EntryMeta]{
		{R: 255, G: 0, B: 0, Meta: EntryMeta{ID: "01", Name: "Red", Hex: "FF0000"}},
		{R: 0, G: 255, B: 0, Meta: EntryMeta{ID: "02", Name: "Green", Hex: "00FF00"}},
		{R: 0, G: 0, B: 255, Meta: EntryMeta{ID: "03", Name: "Blue", Hex: "0000FF"}},
	}
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255}) // Red
	src.SetRGBA(1, 0, color.RGBA{255, 0, 0, 255}) // Red
	src.SetRGBA(0, 1, color.RGBA{0, 255, 0, 255}) // Green
	src.SetRGBA(1, 1, color.RGBA{0, 0, 255, 255}) // Blue
	pat, err := pal.Apply(context.Background(), src, ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return pat
}

func TestStats(t *testing.T) {
	p := sampleApply(t)
	s := p.Stats()
	if s.Width != 2 || s.Height != 2 || s.TotalPixels != 4 {
		t.Fatalf("dims: %+v", s)
	}
	if s.UniqueColors != 3 || s.PaletteSize != 3 {
		t.Fatalf("colors: %+v", s)
	}
	if s.Histogram[0].Count != 2 || s.Histogram[0].Entry.Meta.Name != "Red" {
		t.Fatalf("histogram top: %+v", s.Histogram[0])
	}
}

func TestWriteStatsJSON(t *testing.T) {
	p := sampleApply(t)
	var b bytes.Buffer
	if err := p.WriteStatsJSON(&b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"unique_colors": 3`) {
		t.Fatalf("json missing fields: %s", b.String())
	}
}

func TestWriteBuildMap(t *testing.T) {
	p := sampleApply(t)
	var b bytes.Buffer
	if err := WriteBuildMap(&b, p); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if !strings.Contains(got, "[0][0] = R:255, G:0, B:0\t-Red\n") {
		t.Fatalf("missing first pixel: %s", got)
	}
	if !strings.Contains(got, "[1][1] = R:0, G:0, B:255\t-Blue\n") {
		t.Fatalf("missing last pixel: %s", got)
	}
}

func TestWritePiecesCSV(t *testing.T) {
	p := sampleApply(t)
	var b bytes.Buffer
	if err := WritePiecesCSV(&b, p); err != nil {
		t.Fatal(err)
	}
	r := csv.NewReader(&b)
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if rows[0][0] != "id" || rows[0][3] != "count" {
		t.Fatalf("header: %v", rows[0])
	}
	if rows[1][1] != "Red" || rows[1][3] != "2" {
		t.Fatalf("top row should be Red,2: %v", rows[1])
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4 (header + 3)", len(rows))
	}
}
