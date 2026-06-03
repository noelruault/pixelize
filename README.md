# pixelize

Turn any image into pixel art for a palette you choose. pixelize resizes images and reduces their colors to a built-in retro palette, a palette file of your own, or a build map for a physical lego, perler, or cross-stitch mosaic. Run it as a command, or import it as a Go package.

<p align="center">
  <img src="docs/demo/inputs/starry.jpg" width="46%" alt="Original: Van Gogh, The Starry Night">
  &nbsp;&nbsp;
  <img src="docs/examples/all_colors-starry_night-vincent_van-gogh.png" width="46%" alt="The same image reduced to the lego brick palette">
</p>

> Every pixel gets its **exact** nearest palette color — and pixelize does it **faster, on less memory, and more accurately than ImageMagick**, which assigns a *non-nearest* color to ~13% of pixels on a 162-color palette. See the [benchmark](#benchmark-vs-imagemagick).

## Why this exists

Plenty of tools turn images into lego mosaics or retro-palette pixel art. Almost all of them are web pages you upload to or apps you click through. pixelize does the same work from the command line, offline, and as a Go package you can call from your own code.

## What you can do

- Resize an image to exact pixel dimensions.
- Reduce an image's colors to a built-in palette (NES, Game Boy, PICO-8, lego, and more) or your own CSV, HEX, GPL, or JSON file.
- Or **derive a palette from the image itself** with `-palette auto:N` — turn any photo into clean N-color pixel art (beats ImageMagick's octree at every size and matches/edges pngquant; see the [`quantize`](quantize) package).
- Build a map and a per-color piece count for a physical lego, perler, or cross-stitch mosaic.
- Convert an animated GIF frame by frame, keeping its delays and loop count.
- Render the result in your terminal (iTerm2, Kitty, or plain ANSI).
- Process a whole folder in one command, or rerun automatically whenever a watched file changes.
- Dither with Floyd-Steinberg, or snap each pixel to the nearest color, and print color counts as JSON.

## Install

```sh
go install github.com/noelruault/pixelize/cmd/pixelize@latest
```

Or build from a clone:

```sh
go build -o pixelize ./cmd/pixelize
```

## Quick start

Reduce a photo to the NES palette at 64x64:

```sh
pixelize photo.jpg -size 64x64 -palette nes -o photo_nes.png
```

Reduce a sprite to your own palette file:

```sh
pixelize sprite.png -palette ./my-palette.csv -o sprite_reduced.png
```

Build a lego mosaic with a build map and a parts list:

```sh
pixelize portrait.jpg -size 48x48 -palette lego -build-map mosaic.txt -pieces parts.csv -o mosaic.png
```

## Make a physical mosaic

Reducing an image to a brick or bead palette is only half the job — to actually *build* the thing you need two more outputs, and pixelize emits both: a per-cell **build map** (which color goes in every position) and a **piece-count** shopping list.

```sh
pixelize liberty.jpg -size 64x64 -palette lego \
  -o mosaic.png -build-map build.txt -pieces parts.csv
```

`build.txt` places every cell by row and column:

```
[0][0] = R:5, G:19, B:29   -Speckle Black-Silver
[0][5] = R:27, G:42, B:52  -Chrome Black
...
```

`parts.csv` is how many of each color to buy, most-used first:

```csv
id,name,hex,count
132,Speckle Black-Silver,05131D,1016
64,Chrome Black,1B2A34,520
308,Dark Brown,352100,142
```

The built-in `lego` palette carries real rebrickable brick IDs, so that `id` column maps straight to what you order.

## Turn any image into N colors

When you don't want to snap to a *fixed* palette but to derive a good one *from the
image*, use `-palette auto:N`. It clusters the image's colors into N representatives
and reduces to them — the "any photo into clean pixel art" path.

```sh
pixelize photo.jpg -size 96x96 -palette auto:16 -o art.png   # 16-color pixel art
pixelize photo.jpg -palette auto:64 -o art.png               # bare `auto` = 16
```

One pipeline does it — a PCA-divisive initializer followed by a weighted k-means
refine — and it runs in the color space that's best for the palette size, chosen
automatically:

- **small palettes (N ≤ 48) → RGB.** Entries are far apart (large color differences),
  where plain RGB does best — the pixel-art regime (Game Boy, PICO-8, NES).
- **large palettes (N > 48) → OKLab.** Entries are close (small differences), OKLab's
  perceptually-uniform regime. The result is matched: pixels are *assigned* in the
  same space the palette was built in.

Controls (all only apply with `-palette auto`):

```sh
pixelize photo.jpg -palette auto:64 -quantize rgb      # force the color space (auto|rgb|oklab)
pixelize photo.jpg -palette auto:256 -curve-init       # space-filling-curve init; helps at N>=256
```

And a palette-agnostic post-pass, **merge similar colors**, which works on a derived
*or* a loaded palette (e.g. trimming near-duplicate bricks so a parts list is
buildable):

```sh
pixelize photo.jpg -palette auto:64 -merge 12          # derive 64, then merge colors within 12 (8-bit RGB)
pixelize photo.jpg -palette lego -merge 25             # collapse near-duplicate lego bricks
```

Output is **deterministic** (same image, same palette, every run). The engine lives
in the [`quantize`](quantize) package and feeds the rest of the pipeline (dither,
build map, pieces, GIF) unchanged.

### Benchmark vs pngquant and ImageMagick

Same task, scored identically: derive an N-color palette, reduce the image, measure
the result's perceptual error (mean **CIEDE2000**, lower is better) against the
original — no dithering, so only palette quality is compared. Run on the 18-image
**Kodak** suite. pixelize is `-palette auto:N`; pngquant is `--nofs`; ImageMagick is
`-colors N -dither None`.

| N | ImageMagick (octree) | pngquant (libimagequant) | **pixelize auto** |
| --- | --- | --- | --- |
| 4 | 8.89 | 8.40 | **8.09** |
| 16 | 4.98 | 4.27 | **4.21** |
| 64 | 2.91 | 2.51 | **2.48** |
| 256 | 1.81 | 1.59 | **1.56** |

pixelize beats ImageMagick's octree at **every** size (decisively — it wins 18/18
images at N≥16) and **matches-or-edges libimagequant** at every size (it wins the
mean everywhere and 10–14 of 18 images; at N=16 it is roughly a tie). pngquant is a
strong, well-tuned bar — this is a genuine edge, not a rout, and we say so. (pngquant
also caps at 256 colors; pixelize scales past it.)

The full method — the pipeline decomposed into pieces, every piece (and many
cross-disciplinary ideas: vector quantization, space-filling curves, deterministic
annealing, …) benchmarked and **kept or discarded with a number** — and the measured
evidence behind the table live in the
[quantization research record](https://github.com/noelruault/research/tree/89ce229f966ae63af5ec68185d55cd2a499ed117/quantization)
in `noelruault/research` (pinned), which also reports the authoritative CIEDE2000
numbers above. The validation used Kodak + the demo paintings; the 100-image CQ100
set was unreachable in the build environment.

To reproduce locally against the shipped binary, `bench/compare-quant.sh` runs the
three tools and reports **RGB RMSE** — a dependency-free proxy (needs only pixelize,
pngquant, ImageMagick). Note RMSE and CIEDE2000 diverge: at N>48 pixelize optimizes
*perceptual* error in OKLab, so it can trail on RGB-RMSE at large palettes while
winning on CIEDE2000 — the metric that tracks what the eye sees.

## Palettes

A palette is a CSV, HEX, GPL, or JSON file. pixelize ships a few examples embedded in the binary (gameboy, lego, lego-grayscale, nes, pico8, tol-bright, wong), so you can start without any setup. Everything else lives in your own files.

When you pass `-palette nes`, pixelize looks for a palette of that name first in your palette directory, then in the embedded examples. A file you place in your palette directory overrides an embedded example of the same name.

```sh
pixelize palettes              # list the palettes you can use right now
pixelize palettes init         # copy the embedded examples into your palette directory
pixelize palettes where        # print the path of your palette directory
pixelize palette nes -show nes.png   # render a palette as a swatch PNG
```

For the file formats, see [palettes/README.md](palettes/README.md).

## More examples

Print the color counts of a result as JSON:

```sh
pixelize art.png -palette ./custom.csv -stats -json
```

Also render the result in your terminal:

```sh
pixelize photo.jpg -size 64x64 -palette nes -preview
```

Process every image in a folder:

```sh
pixelize batch ./sprites -palette gameboy -o ./out
```

Rerun automatically when an image changes on disk:

```sh
pixelize watch sprite.png -palette gameboy -o sprite_gb.png
```

Speed up large batches against one palette with `-lut` (long form `-lookup-table`), available on `batch` and `watch`:

```sh
pixelize batch ./sprites -palette gameboy -lut -o ./out
```

It builds a color lookup table once and reuses it, replacing the per-pixel nearest-color search with a single table read — a real win when the palette is large and the table is reused across many images. The trade-off is a small approximation (~2–6% of pixels take their second-nearest color). For a single image the exact default is both faster and more accurate, so `-lut` is not offered there.

Reduce an animated GIF, keeping its timing and loop count:

```sh
pixelize loop.gif -palette pico8 -gif loop_pico8.gif
```

Grow a sprite across several sizes into one animated GIF:

```sh
pixelize sprite.png -size-list 8,16,32,64 -palette pico8 -gif growth.gif -loop full
```

For a full walkthrough of every feature against six paintings and two animated GIFs, see [DEMO.md](DEMO.md).

## Use it as a library

The same engine is a Go package. Load a palette, apply it to an image, and write the result:

```go
package main

import (
	"context"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"

	"github.com/noelruault/pixelize"
)

func main() {
	pal, err := pixelize.LoadFile("nes.csv")
	if err != nil {
		panic(err)
	}

	f, err := os.Open("photo.jpg")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		panic(err)
	}

	pat, err := pal.Apply(context.Background(), img, pixelize.ApplyOptions{
		Width:  64,
		Height: 64,
		Dither: true,
	})
	if err != nil {
		panic(err)
	}

	out, err := os.Create("photo_nes.png")
	if err != nil {
		panic(err)
	}
	defer out.Close()
	png.Encode(out, pat.Image)
}
```

To **derive** a palette instead of loading one, use the `quantize` package. It
returns the palette paired with the matched assignment metric, so the perceptual
result holds through `Apply`:

```go
res, err := quantize.Generate(img, 16, nil) // nil = defaults (space auto-picked by N)
if err != nil {
	panic(err)
}
pat, err := res.Palette.Apply(ctx, img, pixelize.ApplyOptions{
	Width: 64, Height: 64,
	Distance: res.Distance, // matched assignment (OKLab when the OKLab regime was chosen)
})
```

`quantize.Merge(palette, dist)` collapses near-duplicate colors in any palette.

The full reference is at [pkg.go.dev/github.com/noelruault/pixelize](https://pkg.go.dev/github.com/noelruault/pixelize)
(and [.../quantize](https://pkg.go.dev/github.com/noelruault/pixelize/quantize)).

## Configuration

The single-image flags below also apply to `batch` and `watch`. Run `pixelize help` for the complete list, including the animated-GIF flags.

| Flag | What it does |
| --- | --- |
| `-size WxH` | Resize before quantizing. Omit to keep the original size. |
| `-palette NAME` or `-palette PATH` | A palette by name (your directory, then embedded), or a palette file (`.csv`, `.hex`, `.gpl`, `.json`). |
| `-palette auto:N` | Derive an N-color palette from the image (e.g. `auto:16`). Bare `auto` = 16. Picks the working color space by size automatically. |
| `-quantize SPACE` | With `-palette auto`: force the color space — `auto` (default), `rgb`, `oklab`. |
| `-curve-init` | With `-palette auto`: space-filling-curve initializer (helps at N≥256). |
| `-merge DIST` | Merge palette colors closer than DIST (8-bit RGB). Works on derived or loaded palettes. `0` = off. |
| `-mode MODE` | Resize mode: `nn` (default), `avg`, `bilinear`, `catmullrom`. |
| `-dither` | Floyd-Steinberg dithering. Off by default, which snaps each pixel to the nearest color. |
| `-build-map PATH` | Write a per-pixel build map. |
| `-pieces PATH` | Write a piece-count CSV. |
| `-preview` | Render the result in the terminal (iTerm2, Kitty, or ANSI). |
| `-stats` | Print a color histogram. Add `-json` for machine-readable output. |
| `-o PATH` | Output PNG. Defaults to `<input>_<palette>.png`. |

Two settings live outside the flags:

- Palette names resolve against your palette directory first, then the embedded examples. Run `pixelize palettes where` to see the directory, and `pixelize palettes init` to populate it.
- Set `NO_COLOR` to turn off colored terminal output.

## Architecture

pixelize is one Go module. The command lives in `cmd/pixelize`. The image work lives in the root `pixelize` package and its sub-packages:

```
pixelize/
  cmd/pixelize/   command, flag parsing, subcommands
  *.go            root package: load, resize, quantize, dither, GIF, stats, batch, watch
  decode/         image decoding and extra input formats
  preview/        terminal rendering backends (iTerm2, Kitty, ANSI)
  palettes/       embedded example palettes and name resolution
  quantize/       derive a palette from an image (-palette auto:N) + merge
```

Color matching goes through a `DistanceFunc`. The default is the standard library's unweighted Euclidean metric. Pass a `Distance` in `ApplyOptions` to override it, for example with a perceptual metric such as CIEDE2000.

## Benchmark vs ImageMagick

Same task in both tools: reduce an image to a fixed palette and write a PNG — same
input pixels, same size, no resize, no dither — so the only thing measured is each
tool's nearest-color step. pixelize always picks the **true nearest** color;
ImageMagick's remap is approximate, so the **Output diff** column is ImageMagick's
error rate — the share of pixels it sends to a non-nearest color. Reproduce with
`bench/compare.sh`.

**Speed & CPU** — milliseconds per run, `wall / CPU`, averaged over six images,
lower is better. "Speedup" is wall time, pixelize vs ImageMagick.

| Palette | Colors | Size | pixelize | ImageMagick | Speedup | Output diff |
| --- | --- | --- | --- | --- | --- | --- |
| Game Boy | 4 | 256² | 16 / 20 | 27 / 69 | 1.7× | 0.1% |
| Game Boy | 4 | 512² | 47 / 59 | 61 / 140 | 1.3× | 0.1% |
| NES | 55 | 256² | 24 / 39 | 65 / 177 | 2.7× | 3.0% |
| NES | 55 | 512² | 74 / 133 | 200 / 514 | 2.7× | 3.0% |
| lego | 162 | 256² | 25 / 35 | 64 / 173 | 2.6× | 13.2% |
| lego | 162 | 512² | 79 / 121 | 185 / 481 | 2.3× | 13.2% |

**Peak memory** (resident set size), same task:

| Image size | pixelize | ImageMagick |
| --- | --- | --- |
| 512² | ~9–10 MiB | ~17 MiB |
| 2048 px wide | ~57 MiB | ~68 MiB |

In short: pixelize is **faster on every cell in both wall and CPU time, uses less
memory, and stays exact** — while ImageMagick's error rate climbs with the palette:
~0.1% at 4 colors, 3% at 55, **13% at 162**. The "Colors" column is distinct colors:
lego ships 188 entries (162 distinct), the NES palette 64 (55 distinct).

Measured on a shared cloud container (Linux x86_64, Intel Xeon @ 2.80 GHz, 4 cores),
ImageMagick 6.9.12, Go 1.25.0, on 2026-06-01. It is a virtualized, shared machine,
so treat absolute numbers as indicative — the ratios between tools survive the noise
better than the raw values. Run `bench/compare.sh` for numbers on your own hardware;
it prints the machine, cores, and tool versions it ran on.

The reverse-engineering of ImageMagick's approximate remap, the algorithm
bake-offs behind the exact kd-tree and the `-lut` fast mode, and the full
measured evidence for the numbers above live in the
[nearest-color-scaling research record](https://github.com/noelruault/research/tree/86bd65da70c654c33cbf33fde213b6bf78180391/nearest-color-scaling)
in `noelruault/research` (pinned to the import commit so the cited evidence stays put).

## Contributing

There is no `CONTRIBUTING.md` yet. The smallest useful change is a new palette: add a CSV to `palettes/` and it becomes available by name. Two other good entry points are a new input format in `decode/` and a new terminal backend in `preview/`. Each is a small, self-contained package.

## License

MIT. See [LICENSE](LICENSE).
