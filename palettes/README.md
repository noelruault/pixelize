# Palette file formats

Pixelize loads palettes from plain text files. Anything you can edit in a text editor or a spreadsheet works.

`-palette` accepts:

- A **path** (contains `/`, `.`, or `~`) pointing to one of the formats below.
- A **name** that resolves against `$XDG_CONFIG_HOME/pixelize/palettes/` (user dir) then the embedded examples in this directory.

Bootstrap your user dir with the shipped examples:

```sh
pixelize palettes init
```

This copies the example CSVs into `$XDG_CONFIG_HOME/pixelize/palettes/` so you can edit them. Files in your user dir win over the embedded copies.

## Supported formats

### CSV (recommended, easiest to author)

Header row required. Columns recognized: `hex`, `name`, `id`, `r`, `g`, `b`. Order is free. Lines starting with `#` are treated as comments.

Minimal:

```csv
hex,name
#FF0000,Red
#00FF00,Green
#0000FF,Blue
```

With explicit RGB columns:

```csv
r,g,b,name,id
255,0,0,Red,01
0,255,0,Green,02
0,0,255,Blue,03
```

Extra columns are captured under `Entry.Meta.More` and shown in build maps when relevant.

Either `hex` **or** all of (`r`, `g`, `b`) must be present.

### HEX (one color per line)

```
# Pico-8
000000
1d2b53
7e2553
008751
```

The leading `#` is optional. Lines starting with `#` followed by other characters are comments.

Names are auto-derived as `color_1`, `color_2`, ... If you need named colors, use CSV.

### GPL (GIMP palette format)

```
GIMP Palette
Name: Endesga 32
Columns: 8
#
190  72  53   Brick
234 117  47   Orange
...
```

Standard, exportable from GIMP / Inkscape / Aseprite.

### JSON (structured, supports arbitrary metadata)

```json
{
  "name": "lego",
  "entries": [
    {"r":201,"g":26,"b":9,"hex":"C91A09","name":"Red","id":"4"},
    {"r":0,"g":85,"b":191,"hex":"0055BF","name":"Blue","id":"1","meta":{"category":"opaque"}}
  ]
}
```

Use JSON when you need typed extra fields (categories, SKUs, BrickLink IDs).

## Choosing a format

- Quick sketch or one-off: HEX.
- Anything with names: CSV.
- Exported from GIMP / Aseprite: GPL.
- Rich metadata for downstream tooling: JSON.

## Example palettes shipped here

| Name              | Use case                        | Source |
|-------------------|---------------------------------|--------|
| `nes`             | NES sprites                     | community standard |
| `gameboy`         | Game Boy 4-shade green          | DMG-01 hardware |
| `pico8`           | PICO-8 fantasy console          | Lexaloffle |
| `lego`            | Lego mosaics with brick IDs     | rebrickable.com |
| `lego-grayscale`  | Monochrome lego builds          | rebrickable.com |
| `wong`            | Color-blind safe (8 colors)     | Wong 2011, Nature Methods |
| `tol-bright`      | Color-blind safe (7 colors)     | Paul Tol notes |

These exist to teach the format and bootstrap new users. Anything beyond this is your own file.
