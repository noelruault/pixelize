# pixelize

Resize images and quantize their colors to any palette. Library + CLI in Go.

## Use cases

- Fit a photo into a 64x64 pixel-art editor that rejects larger uploads.
- Reduce a sprite to a fixed palette (NES, Game Boy, PICO-8, your own).
- Generate a build map and piece count for a physical lego / perler / cross-stitch mosaic.

## Install

```sh
go install github.com/noelruault/pixelize/cmd/pixelize@latest
```

## Quick start

```sh
pixelize photo.jpg -size 64x64 -palette nes -o photo_nes.png
pixelize sprite.png -palette ./my-palette.csv -o sprite_reduced.png
pixelize portrait.jpg -size 48x48 -palette lego -build-map mosaic.txt -pieces parts.csv -o mosaic.png
```

## Palettes

Palettes are CSV (or HEX / GPL / JSON) files. A few examples ship in `palettes/` to demonstrate the format and bootstrap new users; anything else lives in your own files.

```sh
pixelize palettes              # list resolvable palettes (yours + shipped examples)
pixelize palettes init         # copy shipped examples to $XDG_CONFIG_HOME/pixelize/palettes/
pixelize palette nes -show nes.png   # render a palette as a swatch PNG
```

Authoring a palette: see `palettes/README.md` for the file formats.

## Fast color matching with `-lut`

By default pixelize finds the exact nearest palette color for every pixel.
That is the right choice for a single image, and it is already fast.

When you process **many** images against the **same** palette, add `-lut`
(or its long form `-lookup-table`) to the `batch` and `watch` commands:

```sh
pixelize batch ./photos -palette nes -size 64x64 -lut -o ./out
pixelize watch sprite.png -palette nes -size 64x64 -lut -o sprite_nes.png
```

`-lut` precomputes a color **lookup table** for the palette once, then reuses
it to map every pixel of every image with a single table read instead of a
fresh nearest-color search. The result is roughly a **4x speedup** on large
batches.

The trade-off is a small approximation: about **2-6% of pixels** get their
second-nearest color instead of the exact nearest (the table groups similar
colors into buckets). For pixel art against a fixed palette this is usually
invisible; when you need the exact result, just leave `-lut` off.

It is only offered on `batch` and `watch` on purpose: the table costs a moment
to build, so it only pays off when **reused** across images. On a single
conversion the plain (exact) command is both faster and more accurate, so
`-lut` is not available there.

## Status

Published. See [DEMO.md](DEMO.md) for an end-to-end walkthrough of every feature against six paintings and two animated GIFs.

## License

See `LICENSE`.
