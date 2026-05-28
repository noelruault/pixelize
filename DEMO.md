# pixelize end-to-end demo

Every feature of pixelize, exercised against six famous paintings and two animated GIFs. Inputs, exact commands, and outputs all sit under `docs/demo/`.

## Setup

```sh
go build -o pixelize ./cmd/pixelize
```

Outputs in this doc were generated with this exact binary built from the repo at HEAD.

## Inputs

Six paintings rescaled to a long-axis of 512 px (landscapes by width, portraits by height) and two animated GIFs reduced via gifsicle. JPGs encoded at quality 70 to keep the repo light (1.9 MB total for the input set).

| File | Subject | Dim |
|------|---------|-----|
| `docs/demo/inputs/starry.jpg`    | Van Gogh, *The Starry Night* (1889)             | 512×405 |
| `docs/demo/inputs/monet.jpg`     | Monet, *Impression, Sunrise* (1872)             | 512×397 |
| `docs/demo/inputs/liberty.jpg`   | Delacroix, *Liberty Leading the People* (1830)  | 512×405 |
| `docs/demo/inputs/athens.jpg`    | Raphael, *The School of Athens* (1509-1511)     | 512×397 |
| `docs/demo/inputs/napoleon.jpg`  | David, *Napoleon Crossing the Alps* (1801)      | 420×512 |
| `docs/demo/inputs/pearl.jpg`     | Vermeer, *Girl with a Pearl Earring* (~1665)    | 432×512 |
| `docs/demo/inputs/sugarhigh.gif` | meme: cotton-candy girl                          | 200×196 |
| `docs/demo/inputs/who-king.gif`  | meme: "who is the king"                          | 200×150 |

| Starry Night | Sunrise | Liberty |
|---|---|---|
| ![starry](docs/demo/inputs/starry.jpg) | ![monet](docs/demo/inputs/monet.jpg) | ![liberty](docs/demo/inputs/liberty.jpg) |
| **School of Athens** | **Napoleon** | **Pearl Earring** |
| ![athens](docs/demo/inputs/athens.jpg) | ![napoleon](docs/demo/inputs/napoleon.jpg) | ![pearl](docs/demo/inputs/pearl.jpg) |

## Palettes used

Seven built-in (embedded into the binary via `//go:embed`), two custom (file path).

| Name | Source | Size | Swatch |
|------|--------|------|--------|
| `lego`           | embedded                                | 188 | ![lego](docs/demo/outputs/swatches/lego.png) |
| `lego-grayscale` | embedded                                | 29  | ![lego-gs](docs/demo/outputs/swatches/lego-grayscale.png) |
| `pico8`          | embedded                                | 16  | ![pico8](docs/demo/outputs/swatches/pico8.png) |
| `gameboy`        | embedded                                | 4   | ![gameboy](docs/demo/outputs/swatches/gameboy.png) |
| `nes`            | embedded                                | 64  | ![nes](docs/demo/outputs/swatches/nes.png) |
| `wong`           | embedded                                | 8   | ![wong](docs/demo/outputs/swatches/wong.png) |
| `tol-bright`     | embedded                                | 7   | ![tol](docs/demo/outputs/swatches/tol-bright.png) |
| `oil-classic`    | `docs/demo/palettes/oil-classic.csv`    | 16  | ![oil](docs/demo/outputs/swatches/oil-classic.png) |
| `impressionist`  | `docs/demo/palettes/impressionist.csv`  | 16  | ![imp](docs/demo/outputs/swatches/impressionist.png) |

Reproduce the swatches:

```sh
for p in lego lego-grayscale pico8 nes gameboy wong tol-bright; do
  pixelize palette $p -show docs/demo/outputs/swatches/${p}.png -cell 24 -cols 16
done
pixelize palette docs/demo/palettes/oil-classic.csv   -show docs/demo/outputs/swatches/oil-classic.png   -cell 32 -cols 8
pixelize palette docs/demo/palettes/impressionist.csv -show docs/demo/outputs/swatches/impressionist.png -cell 32 -cols 8
```

## 1. List resolvable palettes

```sh
pixelize palettes
```

```
gameboy                        (embedded)
lego                           (embedded)
lego-grayscale                 (embedded)
nes                            (embedded)
pico8                          (embedded)
tol-bright                     (embedded)
wong                           (embedded)
```

JSON for scripting:

```sh
pixelize palettes -format json | jq '.[].Name'
```

## 2. Paintings × palettes

Six paintings, four palettes each. Aspect ratio preserved via `WxH` with one zero (auto): `-size 128x0` for landscapes, `-size 0x128` for portraits.

```sh
# Landscapes
for img in starry monet liberty athens; do
  for p in lego pico8; do
    pixelize docs/demo/inputs/$img.jpg -size 128x0 -palette $p \
      -o docs/demo/outputs/${img}_${p}.png
  done
  for p in oil-classic impressionist; do
    pixelize docs/demo/inputs/$img.jpg -size 128x0 \
      -palette docs/demo/palettes/$p.csv \
      -o docs/demo/outputs/${img}_${p}.png
  done
done
# Portraits: same loop, with -size 0x128.
```

### Starry Night

| lego (188) | pico8 (16) | oil-classic (16) | impressionist (16) |
|---|---|---|---|
| ![](docs/demo/outputs/starry_lego.png) | ![](docs/demo/outputs/starry_pico8.png) | ![](docs/demo/outputs/starry_oil-classic.png) | ![](docs/demo/outputs/starry_impressionist.png) |

### Impression, Sunrise

| lego | pico8 | oil-classic | impressionist |
|---|---|---|---|
| ![](docs/demo/outputs/monet_lego.png) | ![](docs/demo/outputs/monet_pico8.png) | ![](docs/demo/outputs/monet_oil-classic.png) | ![](docs/demo/outputs/monet_impressionist.png) |

### Liberty Leading the People

| lego | pico8 | oil-classic | impressionist |
|---|---|---|---|
| ![](docs/demo/outputs/liberty_lego.png) | ![](docs/demo/outputs/liberty_pico8.png) | ![](docs/demo/outputs/liberty_oil-classic.png) | ![](docs/demo/outputs/liberty_impressionist.png) |

### School of Athens

| lego | pico8 | oil-classic | impressionist |
|---|---|---|---|
| ![](docs/demo/outputs/athens_lego.png) | ![](docs/demo/outputs/athens_pico8.png) | ![](docs/demo/outputs/athens_oil-classic.png) | ![](docs/demo/outputs/athens_impressionist.png) |

### Napoleon Crossing the Alps

| lego | pico8 | oil-classic | impressionist |
|---|---|---|---|
| ![](docs/demo/outputs/napoleon_lego.png) | ![](docs/demo/outputs/napoleon_pico8.png) | ![](docs/demo/outputs/napoleon_oil-classic.png) | ![](docs/demo/outputs/napoleon_impressionist.png) |

### Girl with a Pearl Earring

| lego | pico8 | oil-classic | impressionist |
|---|---|---|---|
| ![](docs/demo/outputs/pearl_lego.png) | ![](docs/demo/outputs/pearl_pico8.png) | ![](docs/demo/outputs/pearl_oil-classic.png) | ![](docs/demo/outputs/pearl_impressionist.png) |

## 3. Build map + pieces CSV (lego mosaic workflow)

```sh
pixelize docs/demo/inputs/liberty.jpg -size 64x0 -palette lego \
  -o docs/demo/outputs/liberty_lego_64.png \
  -build-map docs/demo/outputs/liberty_build_map.txt \
  -pieces docs/demo/outputs/liberty_pieces.csv
```

![](docs/demo/outputs/liberty_lego_64.png)

- `liberty_build_map.txt`: one line per pixel, `[x][y] = R:..,G:..,B:.. -Name`. Take it to a real workbench.
- `liberty_pieces.csv`: `id,name,hex,count` sorted by count desc. Take it to BrickLink.

## 4. Dither on/off

```sh
pixelize docs/demo/inputs/athens.jpg -size 128x0 \
  -palette docs/demo/palettes/oil-classic.csv \
  -o docs/demo/outputs/athens_oil_nodither.png

pixelize docs/demo/inputs/athens.jpg -size 128x0 \
  -palette docs/demo/palettes/oil-classic.csv -dither \
  -o docs/demo/outputs/athens_oil_dither.png
```

| no dither | Floyd-Steinberg |
|---|---|
| ![](docs/demo/outputs/athens_oil_nodither.png) | ![](docs/demo/outputs/athens_oil_dither.png) |

Dither adds high-frequency noise that breaks up palette banding.

## 5. Resize mode comparison

```sh
for m in nn avg catmullrom; do
  pixelize docs/demo/inputs/monet.jpg -size 128x0 -palette lego -mode $m \
    -o docs/demo/outputs/monet_mode_${m}.png
done
```

| nn (default) | avg (block average) | catmullrom |
|---|---|---|
| ![](docs/demo/outputs/monet_mode_nn.png) | ![](docs/demo/outputs/monet_mode_avg.png) | ![](docs/demo/outputs/monet_mode_catmullrom.png) |

- `nn`: sharpest edges, classical pixel-art look.
- `avg`: block-average smoothing pre-pass, softens noisy textures.
- `catmullrom`: high-quality smooth resampler before quantize.

## 6. Stats (text + JSON)

```sh
pixelize docs/demo/inputs/pearl.jpg -size 0x128 \
  -palette docs/demo/palettes/oil-classic.csv \
  -o docs/demo/outputs/pearl_oil_stats.png -stats
```

```
size:           108x128
total pixels:   13824
palette size:   16
unique colors:  15
histogram (top 16):
      0  #1B130E  Ink Black                       5706
      1  #3D2A1C  Deep Bistre                     1846
      ...
```

JSON for scripting:

```sh
pixelize docs/demo/inputs/pearl.jpg -size 0x128 \
  -palette docs/demo/palettes/oil-classic.csv \
  -o docs/demo/outputs/pearl_oil_stats.png -stats -json \
  > docs/demo/outputs/pearl_oil_stats.json
```

Snippet:

```json
{
  "width": 108,
  "height": 128,
  "total_pixels": 13824,
  "unique_colors": 15,
  "palette_size": 16,
  "histogram": [
    {"index": 0, "entry": {"R": 27, "G": 19, "B": 14, "Meta": {"id": "01", "name": "Ink Black", "hex": "1B130E"}}, "count": 5706}
  ]
}
```

![](docs/demo/outputs/pearl_oil_stats.png)

## 7. Animated GIF: progressive resolution

`-size-list` produces one frame per size, all upscaled (nearest-neighbor) back to the largest size so the animation has consistent bounds.

```sh
pixelize docs/demo/inputs/starry.jpg \
  -size-list 16,32,48,64,96,128 \
  -palette pico8 \
  -gif docs/demo/outputs/starry_progression.gif \
  -loop full
```

![](docs/demo/outputs/starry_progression.gif)

`-loop full` plays 16 → 32 → 48 → 64 → 96 → 128 then 96 → 64 → 48 → 32 in reverse, looping seamlessly.

## 8. GIF inputs

Two paths depending on whether you want a still or an animation.

### 8a. Still: first frame quantized

When the output is `-o foo.png` (no `-gif`), stdlib decodes the first frame only.

```sh
pixelize docs/demo/inputs/sugarhigh.gif -size 128x0 -palette pico8 \
  -o docs/demo/outputs/sugarhigh_pico8.png

pixelize docs/demo/inputs/who-king.gif -size 128x0 -palette nes \
  -o docs/demo/outputs/who-king_nes.png
```

| Sugarhigh Girl (pico8) | who-king (nes) |
|---|---|
| ![](docs/demo/outputs/sugarhigh_pico8.png) | ![](docs/demo/outputs/who-king_nes.png) |

### 8b. Animated: every frame quantized

When input is a `.gif` and output is `-gif foo.gif`, pixelize decodes every frame (composing GIF disposal methods so each frame is the visible image), quantizes each frame independently, and re-encodes preserving the original per-frame delays and loop count.

This is what you want for a "GIF on a 2-euro AliExpress display" or a "GIF on a Game Boy screen": fixed-palette, fixed-resolution, loops forever.

```sh
# Game Boy 4-color, native screen 160x144
pixelize docs/demo/inputs/sugarhigh.gif -size 160x144 -palette gameboy \
  -gif docs/demo/outputs/sugarhigh_gb_native.gif
pixelize docs/demo/inputs/who-king.gif -size 160x144 -palette gameboy \
  -gif docs/demo/outputs/who-king_gb_native.gif

# Or keep native aspect, just enforce the palette
for p in gameboy pico8 nes; do
  pixelize docs/demo/inputs/sugarhigh.gif -size 200x0 -palette $p \
    -gif docs/demo/outputs/sugarhigh_${p}.gif
  pixelize docs/demo/inputs/who-king.gif -size 200x0 -palette $p \
    -gif docs/demo/outputs/who-king_${p}.gif
done

# Lego palette also works (188 colors, larger files)
pixelize docs/demo/inputs/sugarhigh.gif -size 200x0 -palette lego \
  -gif docs/demo/outputs/sugarhigh_lego.gif
```

#### Game Boy screen (160×144, 4 shades)

| Sugarhigh Girl | who-king |
|---|---|
| ![](docs/demo/outputs/sugarhigh_gb_native.gif) | ![](docs/demo/outputs/who-king_gb_native.gif) |

#### Native aspect, palette swap

| Palette | sugarhigh | who-king |
|---------|-----------|----------|
| gameboy | ![](docs/demo/outputs/sugarhigh_gameboy.gif) | ![](docs/demo/outputs/who-king_gameboy.gif) |
| pico8   | ![](docs/demo/outputs/sugarhigh_pico8.gif)   | ![](docs/demo/outputs/who-king_pico8.gif) |
| nes     | ![](docs/demo/outputs/sugarhigh_nes.gif)     | ![](docs/demo/outputs/who-king_nes.gif) |
| lego    | ![](docs/demo/outputs/sugarhigh_lego.gif)    | (lego on who-king skipped, size) |

The first-frame still in 8a and the animated GIFs here are produced by the same `pixelize` binary; the only switch is `-gif PATH` vs `-o PATH.png`.

## 9. Batch mode

Process a whole directory concurrently. Same flags, one input dir, one output dir.

```sh
pixelize batch docs/demo/inputs -palette gameboy -size 128x0 \
  -o docs/demo/outputs/batch-gameboy
```

Eight inputs → eight Game Boy 4-color outputs:

| starry | monet | liberty | athens |
|---|---|---|---|
| ![](docs/demo/outputs/batch-gameboy/starry.png) | ![](docs/demo/outputs/batch-gameboy/monet.png) | ![](docs/demo/outputs/batch-gameboy/liberty.png) | ![](docs/demo/outputs/batch-gameboy/athens.png) |
| **napoleon** | **pearl** | **sugarhigh** | **who-king** |
| ![](docs/demo/outputs/batch-gameboy/napoleon.png) | ![](docs/demo/outputs/batch-gameboy/pearl.png) | ![](docs/demo/outputs/batch-gameboy/sugarhigh.png) | ![](docs/demo/outputs/batch-gameboy/who-king.png) |

Workers default to `runtime.NumCPU()`, override with `-workers N`.

## 10. Palette swatches

```sh
pixelize palette lego -show outputs/swatches/lego.png -cell 24 -cols 16
```

See the palette table at the top of the doc.

## 11. Terminal preview

Not captured in this doc, but renders inline in any modern terminal:

```sh
pixelize docs/demo/inputs/starry.jpg -size 64x0 -palette pico8 -preview \
  -o docs/demo/outputs/starry_preview.png
```

Protocol auto-detect: iTerm2 inline image protocol > Kitty graphics protocol > truecolor ANSI half-blocks. Respects `NO_COLOR`.

## 12. Watch mode

For iteration while editing:

```sh
pixelize watch docs/demo/inputs/starry.jpg -size 128x0 -palette pico8 \
  -o /tmp/watch.png
```

Reruns the pipeline on every save of the input. fsnotify under the hood, 100 ms debounce, watches the parent directory so editor write-then-rename still triggers.

## 13. User palette dir (XDG)

`-palette ARG` resolution:

1. If `ARG` contains `/`, `.`, or `~`, treat as a path. Decode by extension.
2. Otherwise, look up in `$XDG_CONFIG_HOME/pixelize/palettes/<name>.{csv,hex,gpl,json}`, then the embedded examples.

Bootstrap an editable palette set:

```sh
pixelize palettes init        # copy embedded examples into ~/.config/pixelize/palettes/
pixelize palettes where       # print the resolved user dir
```

Files in the user dir win over embedded copies. Edit `~/.config/pixelize/palettes/lego.csv` and `pixelize -palette lego ...` picks up your edits.

## Feature checklist

| Feature | Status in this demo |
|---------|---------------------|
| Single-image conversion + built-in palette | section 2 |
| `-palette PATH` (custom CSV) | sections 2, 4, 5, 6 |
| `-size WxH` aspect-preserving (zero = auto) | every conversion |
| `-mode nn / avg / catmullrom` | section 5 |
| `-dither` Floyd-Steinberg | section 4 |
| `-build-map`, `-pieces` (palette-agnostic outputs) | section 3 |
| `-stats` text + `-stats -json` | section 6 |
| `-size-list` + `-gif` + `-loop full` | section 7 |
| GIF input, still output (first frame decoded) | section 8a |
| GIF input, animated output (every frame quantized, delays preserved) | section 8b |
| `palettes list / init / where` | sections 1, 13 |
| `palette NAME -show` (swatch PNG) | section 10 + palette table |
| `batch DIR` concurrent | section 9 |
| `watch IMAGE` fsnotify rerun | section 12 |
| Terminal preview (iTerm2 / Kitty / ANSI) | section 11 |
| WebP input | not exercised (no source supplied) |
| JXL input via djxl subprocess | not exercised |

## Reproducing the demo

From the repo root, after `go build -o pixelize ./cmd/pixelize`:

```sh
./pixelize batch docs/demo/inputs -palette gameboy -size 128x0 -o /tmp/demo
ls /tmp/demo
```

If the output filenames match what is documented above, the demo is reproducible end-to-end.
