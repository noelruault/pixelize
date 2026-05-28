# Examples

Reference images showing pixelize output. Two flavors of Van Gogh's Starry Night quantized against the lego palettes.

| File | Palette |
|------|---------|
| `all_colors-starry_night-vincent_van-gogh.png` | `lego` (188 colors, full rebrickable set) |
| `grayscale-starry_night-vincent_van-gogh.png`  | `lego-grayscale` (29 grayscale lego colors) |

Reproduce from a source JPG:

```sh
pixelize starry.jpg -size 320x253 -palette lego           -o all_colors.png
pixelize starry.jpg -size 320x253 -palette lego-grayscale -o grayscale.png
```

Add build map and shopping CSV for a physical mosaic:

```sh
pixelize starry.jpg -size 48x48 -palette lego \
  -o mosaic.png -build-map mosaic.txt -pieces shopping.csv
```
