package pixelize

import (
	"strings"
	"testing"
)

func TestLoadCSVHexOnly(t *testing.T) {
	in := `hex,name
#FF0000,Red
#00FF00,Green
#0000FF,Blue
`
	pal, err := LoadCSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(pal) != 3 {
		t.Fatalf("len = %d, want 3", len(pal))
	}
	if pal[0].R != 255 || pal[0].G != 0 || pal[0].B != 0 {
		t.Fatalf("entry 0: %+v", pal[0])
	}
	if pal[0].Meta.Name != "Red" || pal[0].Meta.Hex != "FF0000" {
		t.Fatalf("meta: %+v", pal[0].Meta)
	}
}

func TestLoadCSVRGB(t *testing.T) {
	in := `r,g,b,name,id
255,0,0,Red,01
0,255,0,Green,02
`
	pal, err := LoadCSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(pal) != 2 {
		t.Fatalf("len = %d", len(pal))
	}
	if pal[0].Meta.ID != "01" || pal[0].Meta.Hex != "FF0000" {
		t.Fatalf("meta: %+v", pal[0].Meta)
	}
}

func TestLoadCSVExtraColumnsInMore(t *testing.T) {
	in := `hex,name,category
#FF0000,Red,primary
`
	pal, err := LoadCSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if got := pal[0].Meta.More["category"]; got != "primary" {
		t.Fatalf("category = %q", got)
	}
}

func TestLoadCSVRejectsMissingColumns(t *testing.T) {
	in := `name
Red
`
	_, err := LoadCSV(strings.NewReader(in))
	if err == nil {
		t.Fatal("want error")
	}
}

func TestLoadHEX(t *testing.T) {
	in := `# pico-8 first 4
000000
1d2b53
7e2553
008751
`
	pal, err := LoadHEX(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(pal) != 4 {
		t.Fatalf("len = %d", len(pal))
	}
	if pal[1].R != 0x1d || pal[1].G != 0x2b || pal[1].B != 0x53 {
		t.Fatalf("entry 1: %+v", pal[1])
	}
}

func TestLoadGPL(t *testing.T) {
	in := `GIMP Palette
Name: Test
Columns: 4
#
255   0   0   Red
  0 255   0   Green
  0   0 255   Blue
`
	pal, err := LoadGPL(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(pal) != 3 {
		t.Fatalf("len = %d", len(pal))
	}
	if pal[0].Meta.Name != "Red" {
		t.Fatalf("name: %q", pal[0].Meta.Name)
	}
}

func TestLoadJSON(t *testing.T) {
	in := `{"name":"t","entries":[{"r":255,"g":0,"b":0,"name":"Red","id":"01","meta":{"k":"v"}}]}`
	pal, err := LoadJSON(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(pal) != 1 || pal[0].Meta.Name != "Red" || pal[0].Meta.More["k"] != "v" {
		t.Fatalf("%+v", pal[0])
	}
}
