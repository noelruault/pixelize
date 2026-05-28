package palettes

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveEmbedded(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	r, err := Resolve("nes")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer r.Reader.Close()
	if r.Source != SourceEmbedded || r.Ext != ".csv" {
		t.Fatalf("got %+v, want embedded csv", r)
	}
	data, _ := io.ReadAll(r.Reader)
	if len(data) == 0 {
		t.Fatal("empty embedded palette")
	}
}

func TestResolveUserOverridesEmbedded(t *testing.T) {
	udir := filepath.Join(t.TempDir(), "pixelize", "palettes")
	t.Setenv("XDG_CONFIG_HOME", filepath.Dir(filepath.Dir(udir)))
	if err := os.MkdirAll(udir, 0o755); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(udir, "nes.csv")
	if err := os.WriteFile(custom, []byte("hex,name\n#000000,Black\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := Resolve("nes")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Reader.Close()
	if r.Source != SourceUser {
		t.Fatalf("source = %v, want user", r.Source)
	}
}

func TestResolveNotFound(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := Resolve("nonexistent-palette-xyz"); err == nil {
		t.Fatal("want error")
	}
}

func TestListIncludesEmbedded(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	listing, err := List()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"nes": false, "gameboy": false, "pico8": false,
		"lego": false, "lego-grayscale": false,
		"wong": false, "tol-bright": false,
	}
	for _, l := range listing {
		if _, ok := want[l.Name]; ok {
			want[l.Name] = true
		}
	}
	for n, found := range want {
		if !found {
			t.Errorf("palette %q missing from List()", n)
		}
	}
}

func TestInitCopiesAllPalettes(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	res, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Copied) == 0 {
		t.Fatal("no palettes copied")
	}
	for _, name := range res.Copied {
		p := filepath.Join(res.Dir, name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing copied file: %s", p)
		}
	}

	res2, err := Init()
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Copied) != 0 || len(res2.Skipped) == 0 {
		t.Fatalf("second init: copied=%v skipped=%v, want skipped-only", res2.Copied, res2.Skipped)
	}
}
