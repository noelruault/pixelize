# 08 — Fastest bit-exact nearest-color scan for pixelize

**Question.** The shipped `applyNearest` splits rows into GOMAXPROCS bands and, per
pixel, calls `image.RGBA.At()` → `color.Palette.Index` → `out.SetRGBA()`, writing a
`[][]int` index buffer and a per-worker `map[int]int` usage table. Is there a faster
*bit-identical* variant?

**Answer (TL;DR).** Yes. Reading/writing `src.Pix`/`out.Pix` directly, replicating
`color.Palette.Index`'s 16-bit math against a precomputed palette table, writing a flat
`[]int` index buffer, and accounting usage with a per-worker `[]int` count array
(**`flat-Pix16`**) is bit-identical on every test and **~1.65x faster (geomean)** than the
shipped `baseline-At`, ranging from **1.2x to 3.3x** depending on regime, while cutting hot-loop
allocations from **~4.2M/op (2K) / ~33M/op (8K)** down to **~30 allocs/op total**. Recommended
swap-in.

All numbers are real, measured on this machine: `go version go1.24.7 linux/amd64`, 4 cores,
GOMAXPROCS=4, in a container (noisy — see caveats). Source images are `starry.jpg` upscaled with
`convert in.jpg -resize WxH! out.ppm`. Work was done in `/tmp/eval-scan/`; pixelize source was not
modified. Full raw output + Go source: `08-scan-variants-data.txt`.

---

## The exactness oracle (what "bit-identical" means here)

`color.Palette.Index` (Go 1.24 `image/color/color.go`):

```go
cr,cg,cb,ca := c.RGBA()                  // color.RGBA: each chan v -> v|v<<8 (16-bit), A=255 -> 0xffff
ret, bestSum := 0, uint32(1<<32-1)
for i, v := range p {
    vr,vg,vb,va := v.RGBA()
    sum := sqDiff(cr,vr)+sqDiff(cg,vg)+sqDiff(cb,vb)+sqDiff(ca,va)
    if sum < bestSum { if sum==0 { return i }; ret,bestSum = i,sum }
}
return ret
// sqDiff(x,y) = d:=|x-y|; (d*d)>>2
```

Three things must be replicated exactly: (1) the **8→16-bit promotion** `v|v<<8` before squaring
(squaring 8-bit values is *not* the same), (2) the **alpha term** `sqDiff(ca,va)` — for opaque src
and opaque palette it is always 0, but a non-255 src alpha must still be read and folded in, and (3)
the **first-match tie-break**: strict `<` keeps the lowest index on ties, plus an early `return` on an
exact-zero match. Any variant that promotes differently, drops alpha, or breaks ties toward a different
equidistant color is **not** bit-identical.

`flat-Pix16` reimplements exactly this on the raw 4 bytes from `src.Pix` (R,G,B,A), so it is provably
the same decision, verified empirically below.

---

## Variants built (standalone, stdlib only; types mirror pixelize)

| # | name | read/write | index buffer | usage | palette |
|---|------|-----------|--------------|-------|---------|
| 1 | `baseline-At` | `At()`/`SetRGBA()` | `[][]int` | per-worker map | `cp.Index` (interface) |
| 2 | `flat-Pix-iface` | `src.Pix`/`out.Pix` | flat `[]int` | per-worker map | `cp.Index(color.RGBA{…})` |
| 3 | `flat-Pix16` | `src.Pix`/`out.Pix` | flat `[]int` | per-worker `[]int` | precomputed 16-bit `[]pal16`, inlined `indexRaw` |
| 5a | `flat-Pix16-mapCount` | flat | flat `[]int` | per-worker **map** | same as #3 (isolates map vs []int) |
| 6 | `flat-Pix16-strided` | flat | flat `[]int` | per-worker `[]int` | interleaved row assignment |

Variant 4 (band sweep) and variant 5 (usage accounting) are reported as sub-studies on the winner.

---

## EXACTNESS GATE — all variants PASS

Verified bit-identical **image Pix, index buffer, AND usage map** against the serial
`color.Palette.Index` oracle, on the full cross product of:

- **images:** random noise 512² and 1024² (defeats spatial coherence), real painting `starry 512²`,
  and an **adversarial** 256² image of channel-boundary values `{0,1,63,64,126,127,128,129,191,192,254,255}`.
- **palettes:** random P16 / P64 / P256, plus a hand-built **tie palette** `{0,127,128,255,…}` chosen to
  force exact equidistant ties around the 127.5 boundary.

Result: **`pixDiff=0, idxDiff=0, usageOK=true` for every (variant × image × palette) cell.** All five
variants — including the `flat-Pix16` raw-byte reimplementation and the strided/work-stealing forms —
are bit-identical. No variant was disqualified.

**Race check:** `go run -race . exact` → exit 0, **no DATA RACE warnings**. Workers write disjoint output
regions, disjoint index ranges, and private count arrays; only `src`/`tab`/`palB` are read-shared. Clean.

---

## Benchmark matrix (best-of-N: N=7 ≤2K, 5 @4K, 3 @8K). remap_ms, lower is better.

P = palette size. Mpix/s = megapixels / second. `4k`=3840×2160, `8k`=7680×4320.

### remap_ms — `baseline-At` vs `flat-Pix16` (+ speedup)

| P | size | baseline-At | flat-Pix-iface | **flat-Pix16** | flat16 speedup |
|---|------|------------:|---------------:|---------------:|---------------:|
| 16 | 512² | 25.62 | 20.07 | **13.52** | **1.89x** |
| 64 | 512² | 80.70 | 67.93 | **49.40** | **1.63x** |
| 256 | 512² | 161.84 | 141.99 | **136.42** | 1.19x |
| 16 | 2048² | 661.49 | 320.04 | **198.33** | **3.34x** |
| 64 | 2048² | 1309.46 | 977.75 | **855.03** | 1.53x |
| 256 | 2048² | 2395.37 | 1524.06 | 2392.15 | 1.00x (noisy cell, see below) |
| 16 | 4k | 657.00 | 465.24 | **423.17** | 1.55x |
| 64 | 4k | 1898.37 | 1365.75 | **1305.18** | 1.45x |
| 256 | 4k | 7621.07 | 5478.09 | **4595.09** | **1.66x** |
| 16 | 8k | 4131.38 | 2051.65 | **1437.26** | **2.87x** |
| 64 | 8k | 4494.64 | 3440.74 | **2685.30** | 1.67x |
| 256 | 8k | 12547.69 | 11916.58 | **10456.94** | 1.20x |

**Geomean speedup of `flat-Pix16` over the shipped `baseline-At` across all 12 cells: 1.65x.**

The **`256/2048²` cell reads 1.00x** — this is a container-noise outlier, not a real regime. The very
same workload (`flat-Pix16` contiguous, 2048², P256) measured **1278 ms in the usage-accounting sub-run**
(≈1.9x vs baseline). The matrix happened to capture this cell during a noisy CPU window. I am reporting
the raw number honestly and flagging it. The trend everywhere else is consistent: flat-Pix16 ≥ baseline.

### Allocation story (the headline win)

| variant | allocs/op @2048² | allocs/op @8k | B/op @8k | peak heap @8k |
|---------|-----------------:|--------------:|---------:|--------------:|
| baseline-At | **4,196,668** | **33,185,606** | 580 MB | 713 MB |
| flat-Pix-iface | 4,194,620 | 33,177,920 | 531 MB | 664 MB |
| **flat-Pix16** | **30** | **30** | 398 MB | 929 MB |

`baseline-At` does **one heap allocation per pixel** (every `At()` boxes a `color.Color` interface =
~4.2M allocs at 2K, ~33M at 8K), which is exactly the ~4.2M/op the prompt cited. `flat-Pix16` reduces
the hot loop to **zero allocations**; the residual ~30 allocs/op are the one-time output image, the flat
index buffer, the usage map, and per-worker slices. It also allocates **fewer total bytes** (398 MB vs
580 MB at 8K), because it skips the per-pixel interface boxing and uses a single contiguous `[]int`
index buffer instead of `[][]int`.

**Caveat — peak heap is higher (929 MB vs 713 MB at 8K).** The flat `[]int` index buffer is
`w*h*8 = 265 MB` held as one live contiguous block, whereas baseline's `[][]int` is fragmented and GC
reclaims interface garbage between bands, so the *instantaneous* high-water mark is lower for baseline
even though it churns far more. If peak RSS at 8K matters, an `int32` flat index buffer would halve this
(palette indices fit in int32) — but that changes the `Indices` field type, so it's out of scope for a
bit-identical drop-in.

---

## Band-granularity sweep (variant 4)

At **P256 the per-pixel 256-color scan dominates** and band size is in the noise (all bands ~2.4–2.5 s).
The meaningful sweep is at **P16** (overhead-sensitive), 2048², contiguous bands:

| band rows | remap_ms | Mpix/s | allocs/op |
|-----------|---------:|-------:|----------:|
| 1 | 194.34 | 21.6 | 6156 |
| 8 | 168.18 | 24.9 | 780 |
| 32 | 105.39 | 39.8 | 204 |
| 64 | **101.51** | **41.3** | 108 |
| 128 | 109.51 | 38.3 | 60 |
| **h/GOMAXPROCS (512)** | 102.00 | 41.1 | **24** |

**Finding:** fine bands (1–8 rows) are ~1.6–1.9x *slower* — they spawn thousands of goroutines (one per
band in the contiguous form), and that scheduling overhead and per-band slice allocation dominate.
Anything from **32 rows up to h/GOMAXPROCS is statistically tied** and optimal. The shipped
**`band = h/GOMAXPROCS`** (one contiguous band per worker, 24 allocs) is already the right choice — keep it.

**Contiguous vs work-stealing (32-row chunks) vs strided** (2048², same workload): all three within ~3%
of each other (100–104 ms). No measurable false-sharing penalty from contiguous bands at this row width,
and work-stealing buys nothing on a uniform-cost scan. **Keep contiguous bands** — simplest, no shared
chunk counter / mutex.

---

## Usage-accounting: per-worker `[]int` count vs per-worker `map` (variant 5)

2048², `flat-Pix16` contiguous:

| P | `[]int` count | `map` count | map / arr |
|---|--------------:|------------:|----------:|
| 16 | 102.05 ms | 111.37 ms | **1.09x** |
| 256 | 1277.98 ms | 1352.38 ms | **1.06x** |

**Finding:** the `[]int` count array is a **free 6–9% win** and never loses — no hash, no map growth, no
per-distinct-color allocation; merge is a trivial additive loop over `len(palette)`. For the palette
sizes pixelize uses (tens to a few hundred entries) the count array is strictly better. Adopt it.

---

## GOMAXPROCS scaling (winner, 2048², P256)

| GOMAXPROCS | remap_ms | Mpix/s | scaling |
|-----------|---------:|-------:|--------:|
| 1 | 5097.90 | 0.8 | 1.00x |
| 2 | 2520.05 | 1.7 | 2.02x |
| 4 | 1376.45 | 3.0 | 3.70x |

Near-linear (2.02x at 2 cores, 3.70x at 4). The scan is embarrassingly parallel and memory-bandwidth-light
for small P; flat-Pix16 scales cleanly with cores.

---

## Honesty caveats

- **Container noise.** This is a shared 4-core container. Best-of-N reduces it but does not eliminate it;
  see the `256/2048²` matrix cell (1.00x) which the band/usage sub-runs contradict (~1.9x). Treat
  individual cells as ±10–15%; trust the *pattern* (flat-Pix16 ≥ baseline everywhere, big wins at small P /
  large images) and the geomean (1.65x).
- **Compute-bound at P256.** With a 256-entry linear scan, the inner `indexRaw` loop dominates and the
  scan-mechanics differences (At vs Pix, map vs array) shrink to ~5–20%. The large speedups (1.9–3.3x) are
  in the **small-palette regimes** (P16/P64) where `At()` allocation overhead was the bottleneck — which is
  where most real pixel-art / Lego palettes live.
- **(n/r) cells.** Every matrix cell was measured; the only "missing" data was the four 8K-P256 non-baseline
  cells dropped by the long matrix run, filled separately (the `FILL8K` rows). No cell is interpolated.
- **Peak heap regression** at 8K (flat's contiguous `[]int` vs baseline's fragmented `[][]int`) is real and
  noted above; mitigable with `int32` indices if needed.

---

## RECOMMENDATION

**Swap `applyNearest`'s parallel scan to the `flat-Pix16` form:**

1. Read pixels directly from `src.Pix` (4 bytes R,G,B,A per pixel) and write directly to `out.Pix`.
2. Precompute the palette into a `[]struct{r,g,b,a uint32}` table (each channel `v|v<<8`, alpha
   `255|255<<8`) once before the loop.
3. Per pixel, run the inlined `indexRaw`: `sum = sqDiff(cr,vr)+sqDiff(cg,vg)+sqDiff(cb,vb)+sqDiff(ca,va)`
   with strict-`<` first-match and early zero-return — **byte-for-byte the same decision as
   `color.Palette.Index`** (verified: 0 differing pixels/indices/usage across noise, painting, and
   adversarial-tie inputs).
4. Keep **contiguous bands at `band = h/GOMAXPROCS`** (the sweep confirms this is optimal; fine bands hurt).
5. Replace the per-worker usage `map[int]int` with a per-worker `[]int` of `len(palette)`, merged additively
   (free 6–9% and zero map allocation).
6. The public `Indices [][]int` shape can be preserved by writing into the existing `[][]int` from the flat
   loop (column-major write costs little); or migrate the internal buffer to flat and reshape at the end. The
   exactness gate passed for both the flat `[]int` and the `[][]int` index forms.

**Measured result: bit-identical to the shipped `color.Palette.Index` baseline (image, indices, usage),
race-clean, and ~1.65x faster geomean (1.2x–3.3x by regime, biggest wins at small palettes and large
images), with hot-loop allocations cut from ~4.2M/op (2K) and ~33M/op (8K) to ~30 allocs/op total.**
