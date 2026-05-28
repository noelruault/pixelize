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

## Status

In development. v0.1 not yet released.

## License

See `LICENSE`.
