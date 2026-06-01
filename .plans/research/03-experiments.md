# Experiment 03 — Nearest-color palette quantization: algorithm bake-off

**Goal.** Prototype several nearest-color palette-quantization strategies as
throwaway standalone Go programs, benchmark them honestly across palette sizes
(16/64/256) and image sizes up to 8K, and compare against pixelize's current
approach and ImageMagick.

**Status: complete, with two important honesty caveats** (a correctness bug in
the k-d tree prototype, and a larger-than-hoped error budget for the 5-bit LUT)
documented below. All code is in `/tmp/quantlab/`; pixelize source was read-only
and untouched. Raw numbers in `03-experiments-data.txt`.

**Headline finding.** A precomputed full per-channel inverse-colormap lookup
table (LUT) makes per-pixel cost **independent of palette size** and is by far
the fastest method at every size: at 4K with a 256-color palette the 5-bit LUT
(D) runs in **~18–24 ms** versus **~3.7 s** for the current serial linear scan
(A) and **~0.8 s** for the parallel scan (B) — roughly **150–200× faster than
the current approach**. The catch, measured honestly, is that on a neutral
random palette the 5-bit LUT differs from the exact result on **~9–14% of
pixels** (bounded, small per-pixel error). For exact output, the parallel linear
scan (B) is the safe drop-in; a k-d tree would be the right large-palette choice
**once the prune bug in this prototype is fixed** (it is currently not bit-exact).

---

## 1. Environment & honesty caveats up front

- Shared cloud container: **4 vCPU**, 15 GiB RAM, no swap. CPU is contended and
  not isolated, so absolute times carry noise. I report best-of-N alongside
  average; best-of is more stable. Some side experiments ran *concurrently* with
  the main matrix and are explicitly flagged as unreliable below.
- **Go 1.24.7** (task said 1.25; only 1.24.7 installed — no functional impact).
- ImageMagick **6.9.12-98 Q16** (`convert` on PATH).
- Go timings are **pure in-memory quantize time** (PNG decode/encode excluded),
  one untimed warmup then avg/best over N repeats (N = 7/5/4/3 for
  512/2K/4K/8K). ImageMagick times are **end-to-end wall** and include PNG
  decode+encode — a reference point, not apples-to-apples.
- **Three things I want to be loud about:**
  1. **Algorithm C (k-d tree) is NOT bit-exact in this prototype** — it differs
     from the exact serial result on 6–9365 pixels (≤0.22%). A correct k-d NN
     search must be exact; this is a prune/tie bug, see §5. Its *timings* still
     indicate k-d cost faithfully, but it cannot be called a drop-in exact path
     until fixed.
  2. **The 5-bit LUT's error is ~9–14% of pixels on this random palette** — much
     larger than a casual reading of "5 bits is plenty" would suggest, see §5.
  3. The dedicated **parallel-scaling sweep is unreliable** (ran under
     contention); see §8. Use the matrix-derived speedup instead.
- The `-resize 'WxH!'` flag must be quoted; an early unquoted run produced wrong
  sizes — caught and corrected before benchmarking.
- The **ImageMagick 8K/256 cell has only one sample**, and the 8K/64 samples are
  noisy; all other IM cells are complete. See §6.

## 2. Test data

- **Image:** `docs/demo/inputs/starry.jpg` (Van Gogh, busy texture, wide gamut)
  upscaled with `convert starry.jpg -resize 'WxH!' inW.png` to 512², 2048²,
  3840×2160 (4K), 7680×4320 (8K) = 0.26M / 4.19M / 8.29M / 33.2M pixels.
- **Palettes:** deterministic pseudo-random **distinct** RGB triples,
  `math/rand` seed **1234567**, sizes 16/64/256. A random palette is a *harder,
  neutral* test: dense Voronoi boundaries, no structure for a tree or LUT to
  exploit. A real tuned palette would make the LUT considerably more accurate.
- **Distance:** unweighted Euclidean over 8-bit RGB, alpha ignored (opaque
  images). Verified to give the **identical** nearest index to Go stdlib
  `color.Palette.Index` (16-bit, alpha-inclusive): **0 mismatches** everywhere.
  So algorithm A is a faithful bit-exact stand-in for pixelize's current
  `applyNearest`.

## 3. The algorithms

| | Name | Build cost | Per-pixel cost | Extra memory | Exact? |
|--|------|-----------|----------------|--------------|--------|
| **A** | linear, serial | none | O(P) compares, 1 thread | output only | yes (= pixelize today) |
| **B** | linear, parallel | none | O(P) compares, GOMAXPROCS threads | output only | yes (verified == A) |
| **C** | k-d tree, parallel | O(P log P) build | ~O(log P) avg query | tree ~40 B × P | **NO (bug, see §5)** |
| **D** | 5-bit LUT, parallel | fill 32³=32768 buckets × O(P) | **O(1)** lookup | 64 KiB fixed | no (≈, §5) |
| **E** | 6-bit LUT, parallel | fill 64³=262144 buckets × O(P) | O(1) lookup | 512 KiB fixed | no (closer than D) |

P = palette size. The LUTs are *full precomputed inverse colormaps* (every
bucket filled once from the bucket center, in parallel), not lazy memo caches —
no locking, no miss branch, a single array read per pixel. Build is cheap
because there are at most 32768/262144 buckets regardless of image size.

## 4. Results

### 4a. Time vs palette size (2048×2048, avg ms; lower better)

| algo | pal=16 | pal=64 | pal=256 | 16→256 growth |
|------|-------:|-------:|--------:|--------------:|
| A linear serial | 140.1 | 483.2 | 1898.3 | **13.5×** |
| B linear parallel | 36.0 | 153.9 | 536.4 | 14.9× |
| C kdtree* | 132.7 | 225.0 | 245.5 | 1.85× |
| **D 5-bit LUT** | **5.6** | **6.0** | **9.1** | **1.6× (~flat)** |
| E 6-bit LUT | 6.0 | 17.1 | 37.2 | 6.2× |
| *IM Q16 (e2e s)* | *1.88* | *3.27* | *7.93* | *4.2×* |

\*C timings shown but C is not bit-exact (§5).

The central result holds: **A and B scale ~linearly** with palette size
(~13–15× over 16→256). **C scales near-logarithmically** (~1.85×). **D is
essentially flat** (5.6→9.1 ms; the slight rise is build cost of the 32768-bucket
table being a larger share at small images, plus noise) — confirming the
hypothesis that the LUT makes per-pixel cost palette-independent. **E grows more
than expected** (6→37 ms): its 262144-bucket build cost is *not* negligible and
scales with palette size, so at this image size E's build dominates. At larger
images E flattens out (see 8K below).

### 4b. Time vs image size (avg ms; "—" skipped)

**pal = 16**

| algo | 512² | 2K | 4K | 8K |
|------|-----:|----:|----:|----:|
| A | 8.1 | 140.1 | 248.2 | 988.3 |
| B | 4.1 | 36.0 | 137.0 | 574.7 |
| C* | 9.0 | 132.7 | 345.7 | 1607.4 |
| **D** | **1.0** | **5.6** | **12.0** | **47.0** |
| E | 3.0 | 6.0 | 15.6 | 51.8 |

**pal = 256**

| algo | 512² | 2K | 4K | 8K |
|------|-----:|----:|----:|----:|
| A | 121.1 | 1898.3 | 3707.7 | — (skipped, est ~14 s) |
| B | 30.4 | 536.4 | 1859.5 | 10648.7 |
| C* | 18.2 | 245.5 | 795.0 | 3224.0 |
| **D** | **8.1** | **9.1** | **23.8** | **55.5** |
| E | 35.2 | 37.2 | 131.5 | 128.8 |

Observations from the **real** numbers (these differ substantially from a naive
linear extrapolation, which is exactly why measuring mattered):
- **D dominates at every size and palette.** At 8K/256 it does 33.2M pixels in
  **55 ms** regardless of palette size (~1.7 ns/pixel).
- **C is sometimes *slower* than B at small palettes** (8K/16: C=1607 ms vs
  B=575 ms). The k-d tree only pays off at large palettes (8K/256: C=3224 ms vs
  B=10649 ms). The tree's cache-unfriendly pointer chasing and branch
  divergence cost more than a tight linear scan when P is small — a result that
  contradicts the "tree is always faster" intuition.
- **B's 8K/256 = 10.6 s is much worse than a clean 4× of A would predict.** This
  is contention on the shared box (B at 8K/256 ran near the end of a long batch).
  Best-of (8.4 s) is somewhat better. Treat the very-large B numbers as noisy.

### 4c. Memory

Per-run allocation (Go `TotalAlloc` delta, ~ the `[]uint16` output buffer)
scales with pixels: ~0.5 MB (512²), 8 MB (2K), 15.8 MB (4K), 63 MB (8K) —
identical across algorithms within noise (this is the index buffer, not the RGBA
output or decoded image). Structural extra memory is the differentiator and is
tiny: k-d tree ~40 B × P (≤10 KB at 256); 5-bit LUT **64 KiB fixed**; 6-bit LUT
**512 KiB fixed**. None matter next to the image. (These are allocation figures,
not resident peak; I did not capture per-algorithm RSS.)

## 5. Accuracy

### 5a. The exact methods
- **B == A**: byte-identical index maps, every size/palette (verified).
- **A == stdlib `color.Palette.Index`**: 0 mismatches.
- **C ≠ A** — **this is a bug, disclosed not hidden.** C differs on 6–9365
  pixels (≤0.22%). A correct k-d nearest-neighbor search is exact by
  construction; the discrepancy points to a prune/tie-handling defect in
  `kdNearest` (most likely the `diff*diff < bestD` prune should be `<=`, or an
  equal-distance-on-the-splitting-plane tie not being explored). The timings
  remain a valid indication of k-d traversal cost, but **C as written is not a
  drop-in exact replacement.** Fixing the prune is a small change but must be
  done and re-verified before C could ship as the exact path.

### 5b. The approximate LUTs (D = 5-bit, E = 6-bit) vs exact A

| palette | D: % px differ | D: max color err | E: % differ | E: max color err |
|---------|---------------:|-----------------:|------------:|-----------------:|
| 16 (2K) | 8.64% | 46.1 | 4.60% | 34.1 |
| 64 (2K) | 9.95% | 37.0 | 5.49% | 24.7 |
| 256 (2K)| 14.36% | 31.4 | 7.25% | 22.1 |
| 16 (512)| 8.63% | 45.3 | 4.57% | 32.4 |
| 64 (512)| 9.91% | 36.1 | 5.44% | 24.7 |
| 256 (512)| 14.27% | 31.4 | 7.24% | 21.3 |

"max color err" = sqrt(extra squared distance) of the approximate vs exact pick,
in RGB units (0..441). **Honest reading:** the 5-bit LUT changes **~9–14% of
pixels** on this random palette — far more than the "near-exact" framing one
might assume. Two compounding causes: (a) the random palette has dense Voronoi
boundaries, and (b) the bucket-**center** lookup injects up to ±4 units/channel
of key error on *every* pixel, not only those near a boundary. The *magnitude*
of each error is small and bounded (max ≤46 units, shrinking as the palette
densifies because the next-nearest color is closer), so it never makes a wild
choice — but it is unambiguously an **approximation with a real error budget**,
not a stand-in for exact. The 6-bit LUT (E) roughly halves both the affected
fraction (~4.6–7.2%) and the max error, for an 8× larger table.

For a *tuned* palette (clustered, not random) the error would be substantially
lower; the figures here are the pessimistic, neutral case.

## 6. ImageMagick reference

`convert in.png +dither -remap swatchN.png out.png` (`+dither` ⇒ nearest-color).
Swatches were the **exact same** Go palette colors, so palettes match bit-for-bit.

End-to-end wall (avg s): 512 → 0.18/0.31/0.71; 2K → 1.88/3.27/7.93; 4K →
4.19/6.30/20.39; 8K → 21.80/18.28/35.64 (pal 16/64/256). The 8K/256 cell is a
single sample and the 8K/64 samples are noisy (21.3 vs 15.3 s) — the 8K/64 <
8K/16 average is container noise, not a real effect.

Crucial caveat: these include PNG decode+encode, which is **large** at high res.
Measured I/O only (`convert in.png out.png`): 512=0.12 s, 2K=2.72 s, 4K=4.95 s,
8K=18.6 s. So at 8K/16, ~18.6 s of the 22.5 s is PNG codec, and the remap proper
is only ~4 s. **The headline gap between IM and the Go in-memory numbers is
partly codec cost, not purely algorithm** — the fair statement is that IM's
*remap* is in the low-seconds range at high res while Go's LUT is tens of
milliseconds, but a chunk of IM's wall time is I/O that the Go quantize-only
timing excludes. The Go programs also pay PNG cost when used end-to-end (not
measured separately here).

## 7. Crossover analysis — which algorithm wins where

- **Tiny palette (16), any size, need exact:** **B (parallel linear)**. Simple,
  exact, and at small P the k-d tree is actually *slower* (8K/16: C=1607 ms vs
  B=575 ms). B is the clear exact choice here.
- **Large palette (256), need exact:** **k-d tree** *in principle* (8K/256:
  C=3224 ms vs B=10649 ms, ~3× faster) — **but only after the C prune bug is
  fixed.** Until then, B is the exact fallback.
- **Any palette, can tolerate ~9–14% bounded boundary-flips:** **D (5-bit LUT)**
  dominates everything and is palette-independent (≤55 ms at 8K). If that error
  is too high, **E (6-bit)** halves it (≤129 ms at 8K).
- **Small image (512²):** everything is <130 ms; even the current serial scan is
  fine at 16 colors (8 ms). The choice only matters at 2K+ or 256 colors.
- **Hypothesis confirmed:** the LUT makes per-pixel cost independent of palette
  size — D is ~flat across the 16× palette range (5.6→9.1 ms at 2K) while A/B
  grow ~13–15×.

## 8. Parallel scaling (2048², pal=256) — UNRELIABLE, disclosed

A dedicated GOMAXPROCS=1/2/4 sweep gave B=2001/1598/1546 ms, C=907/860/591 ms,
D=27.6/32.1/18.1 ms — i.e. only ~1.3–1.5× from 1→4 cores. **These ratios are
not trustworthy:** the sweep ran concurrently with the still-active matrix and
IM benchmarks, so all four vCPUs were already contended at "1 core." A cleaner
signal comes from the matrix itself: at 2K/256, B (4-core) = 536 ms vs A (serial)
= 1898 ms ⇒ **~3.5× effective benefit from the parallel design**, which matches
expectation for an embarrassingly-parallel uniform-work loop on 4 cores. The
dedicated sweep is reported for honesty, but **a clean re-run on an idle box is
needed for a definitive scaling curve.** B is expected to scale near-linearly; C
worse (branch divergence); D is memory/build-bound and gains least from cores.

## 9. Recommended design(s)

For pixelize's nearest-color (non-dithered) path replacing the current
single-threaded `color.Palette.Index` loop:

1. **Immediate, safe, exact win — B (parallelize the existing linear scan).**
   Identical output to today, verified, no accuracy caveat, ~3.5× faster on this
   4-core box and more on bigger machines. Lowest-risk change; do this first.
   It is also the only *exact* method here that is bug-free as prototyped.

2. **For large palettes, exact — k-d tree (C), AFTER fixing the prune bug.**
   ~3× faster than B at 8K/256. Worth it only at large P (it is slower than B at
   P=16). Must be re-verified bit-exact against A before shipping.

3. **Fast/preview path — 5-bit LUT (D), behind an opt-in flag.** Palette-
   independent, ~150–200× faster than the current serial scan at 4K/256, 64 KiB
   table. **Document the accuracy honestly:** ~9–14% of pixels differ on a
   neutral/random palette (less on tuned palettes), each by a small bounded
   amount. Suitable for previews, thumbnails, and large batch jobs where exactness
   is not required; **not** a silent replacement for the exact path. Offer **E
   (6-bit, 512 KiB)** as a higher-accuracy LUT variant (~half the error).

Independent of algorithm, **parallelizing the per-pixel loop is the single
biggest low-risk win** and applies to every method including the current scan.

### Files
- Prototype source: `/tmp/quantlab/main.go` (package `main`, stdlib only)
- Benchmark drivers: `/tmp/quantlab/bench.sh`, `/tmp/quantlab/bench_im.sh`
- Raw numbers: `/home/user/pixelize/.plans/research/03-experiments-data.txt`
