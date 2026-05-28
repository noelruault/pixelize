package pixelize

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// goldenUpdate regenerates testdata/golden hashes when -update is passed.
var goldenUpdate = flag.Bool("update", false, "regenerate golden hashes")

// generateInput returns a 32x32 deterministic gradient image suitable
// as a regression fixture. Each pixel = (x*8, y*8, (x+y)*4).
func generateInput() *image.RGBA {
	const side = 32
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 8),
				G: uint8(y * 8),
				B: uint8((x + y) * 4),
				A: 255,
			})
		}
	}
	return img
}

// pngHash encodes img to PNG and returns the sha256 hex digest.
func pngHash(img image.Image) (string, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

// applyBuiltinPalette loads a builtin palette by name and applies
// it to img with default options.
func applyBuiltinPalette(t *testing.T, name string, img image.Image) *Pattern[EntryMeta] {
	t.Helper()
	data, err := loadGoldenPalette(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	pal, err := LoadCSV(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	pat, err := pal.Apply(context.Background(), img, ApplyOptions{})
	if err != nil {
		t.Fatalf("apply %s: %v", name, err)
	}
	return pat
}

// loadGoldenPalette reads palettes/<name>.csv from disk. Cannot use
// the palettes package directly because of import cycle in tests.
func loadGoldenPalette(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join("palettes", name+".csv"))
}

const goldenFile = "testdata/golden_hashes.json"

func TestGoldenPalettes(t *testing.T) {
	names := []string{
		"nes", "gameboy", "pico8", "lego", "lego-grayscale", "wong", "tol-bright",
	}
	sort.Strings(names)
	img := generateInput()

	got := map[string]string{}
	for _, n := range names {
		pat := applyBuiltinPalette(t, n, img)
		h, err := pngHash(pat.Image)
		if err != nil {
			t.Fatal(err)
		}
		got[n] = h
	}

	if *goldenUpdate {
		if err := os.MkdirAll(filepath.Dir(goldenFile), 0o755); err != nil {
			t.Fatal(err)
		}
		data, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenFile, data, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", goldenFile)
		return
	}

	want := map[string]string{}
	data, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatalf("read %s: %v (run with -update to create)", goldenFile, err)
	}
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if got[n] != want[n] {
			t.Errorf("palette %q hash mismatch: got %s, want %s (run go test -update if intentional)",
				n, got[n], want[n])
		}
	}
}

// TestPipelineRoundTrip verifies that the full Apply -> WriteBuildMap
// -> WritePiecesCSV chain produces consistent output on the gradient.
func TestPipelineRoundTrip(t *testing.T) {
	img := generateInput()
	pat := applyBuiltinPalette(t, "pico8", img)

	if pat.Image.Bounds() != img.Bounds() {
		t.Fatalf("bounds changed: %v vs %v", pat.Image.Bounds(), img.Bounds())
	}
	if pat.UniqueColors() < 2 {
		t.Fatalf("only %d unique colors", pat.UniqueColors())
	}
	if pat.UniqueColors() > len(pat.Palette) {
		t.Fatalf("unique > palette size")
	}

	var bm bytes.Buffer
	if err := WriteBuildMap(&bm, pat); err != nil {
		t.Fatal(err)
	}
	if bm.Len() == 0 {
		t.Fatal("empty build map")
	}
	var pc bytes.Buffer
	if err := WritePiecesCSV(&pc, pat); err != nil {
		t.Fatal(err)
	}
	if pc.Len() == 0 {
		t.Fatal("empty pieces csv")
	}
}
