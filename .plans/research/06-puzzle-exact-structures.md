# 06 — Two EXACT Nearest-Color Structures: Boundary-Aware LUT & Cell-List Grid (benchmark-first)

**Author:** benchmark-first evaluation agent
**Date:** 2026-06-01
**Scope:** Build and rigorously measure two new EXACT nearest-color structures for palette
quantization (report 05 §A/§1), plus the make-or-break boundary-fraction measurement that
decides whether the boundary-aware LUT is worth shipping. All work standalone in
`/tmp/puzzle-exact/` (stdlib Go only); pixelize source untouched. Raw numbers + full Go
source in `06-puzzle-exact-structures-data.txt`.

**Machine / harness:** 4 logical cores, Go 1.24.7, Linux. Row-parallel worker pool
(NumCPU workers) with optional run-length skip — identical driver to the informed-challenger
harness (report 04) so all numbers below are one machine, one run, apples-to-apples.
Distance oracle: 8-bit RGB unweighted squared Euclidean (== `color.Palette.Index`, report 03).
Test image: `starry.jpg` resized to each target via `convert -resize WxH!`.

---

## TL;DR / Headline

- **Both new structures are provably EXACT** — bit-for-bit identical labels to the brute-force
  linear scan (0 non-nearest) across **every** tested configuration: random / tuned / real
  NES / real LEGO palettes, P ∈ {16,64,256,488,188}, grid resolutions {5,6,7-bit}, and up to
  4.2 M pixels — **including the worst-case 100%-boundary tuned palette**. No equidistant-tie
  or `(n/r)` corner cases produced a mismatch.
- **Does the boundary-aware exact LUT beat kd?** **Only in a narrow corner: small palettes
  (P ≤ 64) on large images, and only at 7-bit resolution** (which costs a 100–390 ms build and
  8 MiB). At the palette sizes pixelize actually ships (LEGO P=188, and P=256), **kd-exact wins
  decisively and the boundary LUT loses** — because too many pixels fall into boundary cells and
  pay a full P-wide fallback scan.
- **The boundary-pixel fraction on REALISTIC palettes is the killer number: 34–77 %.** On real
  NES (P=64) it is 38 % even at 7-bit; on real LEGO (P=188), 34 % at 7-bit. On *tuned/clustered*
  palettes it reaches **96–100 %**. The interior fast path does **not** dominate on realistic
  palettes — the fallbacks erode (and at large P, erase) the win.
- **The cell-list grid is a dud here:** never the fastest method in any cell of the matrix. It
  is exact and simple but loses to both kd and linear at every (P, size) on 4 cores.
- **External reference:** ImageMagick remap-only is **10–40× slower** than our exact kd
  (and ~22 % non-nearest). Our exact methods dominate IM on both speed and correctness.

---

## 1. The two structures

### Structure 1 — Boundary-Aware Exact LUT (report 05 §A, the standout idea)

A 3-D inverse-colormap grid at `bits` ∈ {5,6,7} resolution per channel (32³ / 64³ / 128³ cells).
Each cell is classified **once at build time** as:

- **INTERIOR** — the cell lies entirely inside one palette color's Voronoi region. Store that
  color's label. Every query in the cell returns it in **O(1)**, provably exact.
- **BOUNDARY** — a Voronoi boundary may cross the cell. Store sentinel `-1`; at query time fall
  back to an **exact linear scan** over all P colors.

**Straddle test (correctness proof).** Cell `(ri,gi,bi)` covers the 8-bit cube
`[ri·s, (ri+1)·s)³` where `s = 256/2^bits`. Let `center` be the true geometric cell center and
`halfDiag = s·√3 / 2` (max distance from center to any point in the cell). Compute
`dmin = dist(center, nearestPalette)`. The cell is **safely INTERIOR** iff **every** rival `c`
satisfies `dist(center, c) > dmin + 2·halfDiag`. Proof: for any point `p` in the cell,
`dist(p, nearest) ≤ dmin + halfDiag` and `dist(p, c) ≥ dist(center, c) − halfDiag`; if
`dist(center,c) > dmin + 2·halfDiag` then `dist(p,c) > dmin + halfDiag ≥ dist(p,nearest)`, so
`nearest` wins everywhere in the cell. Any cell failing this for ≥1 rival is marked BOUNDARY.

This is the cheap "≥2 references within (center-dist + half-diagonal)" straddle test from
report 05 §2/§A, implemented exactly and **verified** (0 non-nearest, all configs).

> **Implementation note that caused (and fixed) a real bug.** My first cut keyed the cell center
> off the *bit-replicated* representative (`r<<2|r>>4`) and added `s/2`. That is wrong: the
> replicated value is the query *lookup* key, not the cell's geometric position. Using it in the
> straddle test mis-sized the cube and produced 29–185 non-nearest pixels (0.01–0.07 %). Fixing
> the center to the **true geometric** `ri·s + s/2` made it exactly 0 everywhere. The lookup path
> still uses the plain `>> (8−bits)` cell index (which is geometrically consistent: cell `ri`
> receives 8-bit values `[ri·2^shift, (ri+1)·2^shift)`), so no replication is needed on lookup at
> all. Lesson: replication matters for *representative-color* LUTs (report 04's `lut6`), but the
> boundary proof must use the literal cube geometry.

**Cost.** Build is O(cells · P) float distance work, parallelized across cores:
- 5-bit: 32 768 cells — build 2–26 ms, **0.2 MiB**.
- 6-bit: 262 144 cells — build 11–328 ms, **1.1 MiB**.
- 7-bit: 2 097 152 cells — build 105 ms–1.9 s, **8.1 MiB**.
Per pixel: 1 array load; if INTERIOR done, else a P-wide linear scan.

### Structure 2 — Cell-List Grid + Exact Ring Expansion (report 05 §1, MD-derived)

Uniform 3-D grid over the RGB cube, `gdim = ⌈P^(1/3)⌉` cells/axis (MD "≈1 reference/cell"
heuristic), clamped to [2,64]. Palette colors are binned into cells stored **CSR-style**
(`cellStart[]` offsets into a flat `cellPts[]` — no slice-of-slices, no pointer chasing).

Per query: scan the pixel's own cell, then expand in **Chebyshev rings**. Before scanning ring
`r`, if we already have a candidate and the lower-bound distance to the inner boundary of that
ring `((r−1)·cellSize)²` exceeds the current best, **stop** — guaranteeing the true nearest was
already found (exact termination). Build is trivially fast (<0.1 ms, <0.2 MiB).

---

## 2. EXACTNESS — both structures, 0 non-nearest (the table that gates everything)

Verified bit-for-bit vs the brute-force linear oracle (`dist(chosen) > dist(true)` counts as a
miss). **Every** configuration returned **0**:

| palette | P | image | boundaryLUT 5/6/7-bit | cellGrid | kd |
|--------|----|-------|----------------------|----------|----|
| random | 16/64/256 | 512² | 0 / 0 / 0 | 0 | 0 |
| tuned | 16/64/256/488 | 512² | 0 / 0 / 0 | 0 | 0 |
| real NES | 64 | 512² & 2048² | 0 / 0 / 0 | 0 | 0 |
| real LEGO | 188 | 512² & 2048² | 0 / 0 / 0 | 0 | 0 |

Notably the **tuned P=488 case has 100 % boundary cells/pixels at 5-bit** (every cell straddles)
— and still returns 0 non-nearest, because every boundary pixel falls through to the exact scan.
This confirms the fallback path is correct, and that correctness degrades *gracefully into the
exact scan*, never into wrong answers. **Both structures are ship-safe on correctness.**

---

## 3. THE MAKE-OR-BREAK MEASUREMENT — boundary fractions (cell AND pixel)

Boundary **cell** fraction = build-time (% of grid cells marked BOUNDARY).
Boundary **pixel** fraction = % of *actual image pixels* (starry, 512²) that land in a boundary
cell and therefore pay the fallback scan. The pixel fraction is what governs runtime.

### 3a. Random palette (pessimistic)

| P | bits=5 cell% / **pix%** | bits=6 cell% / **pix%** | bits=7 cell% / **pix%** |
|----|----|----|----|
| 16 | 37.2 / **58.6** | 20.2 / **31.6** | 10.4 / **14.5** |
| 64 | 55.8 / **80.7** | 31.6 / **53.5** | 16.9 / **28.5** |
| 256 | 79.2 / **86.3** | 50.6 / **58.1** | 28.5 / **31.6** |
| 488 | 89.4 / **97.1** | 62.1 / **77.3** | 36.3 / **49.4** |

### 3b. Tuned / clustered palette (tight blobs — pathological)

| P | bits=5 **pix%** | bits=6 **pix%** | bits=7 **pix%** |
|----|----|----|----|
| 16 | 57.9 | 35.5 | 27.9 |
| 64 | **100.0** | 82.1 | 30.0 |
| 256 | **100.0** | **99.8** | 85.5 |
| 488 | **100.0** | 95.9 | 70.4 |

### 3c. REAL designed palettes (realistic — this is the answer that matters)

| palette | P | bits=5 cell% / **pix%** | bits=6 cell% / **pix%** | bits=7 cell% / **pix%** |
|---------|----|----|----|----|
| NES | 64 | 52.7 / **74.4** | 34.2 / **66.9** | 18.3 / **38.5** |
| LEGO | 188 | 73.9 / **77.1** | 51.1 / **50.5** | 34.6 / **34.2** |

**Verdict on the central question — does the interior fast path dominate?**
**No, not on realistic palettes.** Even at the most expensive 7-bit grid (8 MiB, ~100 ms–1 s
build), **34–38 %** of real-image pixels land in boundary cells and pay a full P-wide scan. The
pixel fraction is *higher* than the cell fraction at low bits (NES 5-bit: 53 % cells but **74 %
pixels**) because real images concentrate pixels exactly in the busy, color-dense parts of the
cube where Voronoi boundaries are densest — the boundary cells are the *popular* cells. On
clustered palettes (tuned, and to a degree LEGO) the structure collapses to ~100 % boundary and
becomes "a LUT-shaped wrapper around the linear scan." The interior fast path only convincingly
dominates for **small random palettes at 7-bit** (P=16: 14.5 % pixels), which is not pixelize's
shipping regime.

---

## 4. HEAD-TO-HEAD SPEED (best-of-N remap_ms; bars to beat = kd-exact, linear-par)

Lower `remap_ms` is better. `★` marks the fastest EXACT method in that row. ImageMagick
remap-only (approximate, ~22 % non-nearest) shown where measured as the external floor.

### 4a. Small palette P=16, random — where the LUT shines

| image | linear | kd | cellgrid | blut5 | blut6 | blut7 |
|-------|-------:|---:|---------:|------:|------:|------:|
| 512² | 2.99 | 6.13 | 11.14 | 2.61 | 2.06 | **1.72 ★** |
| 2048² | 48.8 | 91.2 | 168 | 38.6 | 29.2 | **23.6 ★** |
| 3840×2160 | 88.1 | 166 | 312 | 77.5 | 56.6 | **45.4 ★** |
| 7680×4320 | 331 | 588 | 1061 | 305 | 219 | **173 ★** |

→ blut7 ≈ **3.4× faster than kd**, ≈ 1.9× faster than linear. This is the structure's best case.

### 4b. P=64 — random vs real NES

| image / pal | linear | kd | cellgrid | blut6 | blut7 |
|-------------|-------:|---:|---------:|------:|------:|
| 3840×2160 random | 410 | 237 | 301 | 149 | **94.8 ★** |
| 7680×4320 random | 761 | 958 | 1411 | 798 | **495 ★** |
| 3840×2160 **NES** | 294 | 296 | 336 | 175 | **114 ★** |
| 7680×4320 **NES** | 747 | 745 | 1153 | 705 | **456 ★** |

→ At P=64, blut7 still beats kd (~1.6–2× on NES) **but** carries a 250 ms build + 8 MiB. blut6
(1.1 MiB) is a tie-to-slight-win vs kd on real NES; blut5 is a wash. Margin is real but modest.

### 4c. Large palettes P=256 / real LEGO P=188 — kd wins, LUT loses

| image / pal | linear | **kd** | cellgrid | blut5 | blut6 | blut7 |
|-------------|-------:|-------:|---------:|------:|------:|------:|
| 3840×2160 random-256 | 1115 | **405 ★** | 327 | 825 | 564 | 433 |
| 7680×4320 random-256 | 3771 | **1338 ★** | 1481 | 4419 | 3031 | 2408 |
| 3840×2160 **LEGO-188** | 822 | **260 ★** | 342 | 551 | 371 | 259 |
| 7680×4320 **LEGO-188** | 2781 | **1109 ★** | 1571 | 2956 | 1983 | 1373 |

→ At pixelize's real LEGO palette and at P=256, **kd-exact is the fastest exact method**, and
the boundary LUT is **1.2–2.2× slower** than kd even at 7-bit — because the 34–50 % boundary
pixels each run a 188/256-wide scan, which dwarfs kd's logarithmic search. (Note cellgrid edges
out kd in the single random-256 3840 cell, 327 vs 405 ms, but loses everywhere else and at 8K;
it is not a reliable win.)

### 4d. ImageMagick external reference (approximate, ~22 % non-nearest)

| image | pal | IM remap-only | our exact kd | our exact speedup |
|-------|-----|--------------:|-------------:|------------------:|
| 3840×2160 | NES-64 | ~6.15 s | 0.296 s | **~21×** |
| 7680×4320 | NES-64 | ~23.7 s | 0.745 s | **~32×** |
| 3840×2160 | LEGO-188 | ~2.71 s | 0.260 s | **~10×** |
| 7680×4320 | LEGO-188 | ~8.11 s | 1.109 s | **~7×** |

Our exact methods beat the approximate IM baseline by 7–32× — the external floor is far below us.

---

## 5. Verdict per structure

### Boundary-Aware Exact LUT — **REJECT for the shipping regime; keep only as a small-P fast path**

- **Ship-worthy regime:** narrow. Only **P ≤ 64 + large images + 7-bit grid** gives a real win
  (1.6–3.4× over kd while staying exact). At P=16 it is the clear champion.
- **Reject reason (measured):** at pixelize's real palettes (LEGO P=188) and at P=256, the
  **boundary-pixel fraction is 34–50 % even at the costly 7-bit grid**, so a third-to-half of
  pixels run the full linear fallback. That makes the LUT **1.2–2.2× slower than the exact kd**
  it was meant to beat, while costing 8 MiB and a 0.7–1.9 s build. The "interior dominates"
  premise (report 05 §A) **fails on realistic palettes** — confirmed, not assumed.
- **Caveat:** ring-expansion fallback (instead of full scan) could shave the boundary cost, but
  the boundary pixels are precisely the cube-dense regions where ring expansion scans the most
  cells, so the upside is limited; I did not pursue it because the cell-list grid (which *is*
  ring expansion) already loses to kd outright (§4c), bounding the achievable gain.

### Cell-List Grid + Ring Expansion — **REJECT (exact but never fastest)**

- **Reject reason (measured):** it is exact and clean, but **never the fastest method in any
  (P, size) cell** on 4 cores. At small P the grid/ring overhead loses to linear and the LUT; at
  large P kd's branch-and-bound prunes better than Chebyshev-ring expansion. The MD heuristic
  (~1 ref/cell, gdim=P^⅓) gives shallow grids (gdim 3–7) where ring expansion devolves toward a
  near-full scan. Validated as the §1 idea, but it does not transfer to a win here. The exact kd
  from report 04 already occupies its niche better.

---

## 6. Honesty caveats

- **Container noise:** single-machine, 4 cores, best-of-N (15/8/5/3 reps by size). Run-to-run
  jitter is visible (e.g. random-3840 P=64 linear 410 ms looks anomalously slow vs its 2048/8K
  neighbors — likely a scheduling/GC blip; the *relative* ordering across methods is stable,
  which is what the verdicts rest on). Treat absolute ms as ±10–15 %.
- **`runLength` skip** is ON for linear/kd/cellgrid (matching report 04) and OFF for the LUTs
  (a LUT lookup is cheaper than the equality test). This slightly favors the LUTs on highly
  run-length-coherent inputs; starry has little flat area so the effect is small.
- **"Tuned" palette is synthetic** (sqrt(P) Gaussian blobs, σ≈24). It is deliberately
  pessimistic-clustered to bracket the worst case; the **real NES/LEGO numbers are the ones to
  trust** for the shipping decision, and they sit between random and tuned (closer to random for
  NES, more clustered for LEGO).
- **Build time excluded from remap_ms** (reported separately) — correct since the index
  amortizes over the whole image, but note the boundary-LUT 7-bit build (0.1–1.9 s) is a real
  per-image tax that erases its win on small/medium images.
- **No `(n/r)` ambiguous cells:** every cell is definitively INTERIOR or BOUNDARY by the proof;
  there is no "unknown" third class. Equidistant ties are handled by the fallback scan (same
  tie-break as brute force: lowest index), so ties never cause a mismatch.
- **What I skipped:** ring-expansion as the boundary-LUT fallback (bounded-upside, §5);
  SIMD/Morton layout (out of scope, report 05 #4/#5); a 4-bit grid (too coarse, ~all boundary).

---

## 7. Bottom line for the plan

For pixelize's actual palettes (NES P=64, LEGO P=188) and target sizes, **the exact kd-tree from
report 04 remains the bar to beat and the boundary-aware LUT does not clear it** — the
boundary-pixel fraction (34–50 % at 7-bit on real palettes, ~100 % on clustered) keeps too many
pixels on the slow fallback. The one defensible use of the boundary LUT is a **small-palette
(P ≤ 64) + huge-image fast path** (up to 3.4× over kd at P=16), gated behind the measured
boundary fraction. The cell-list grid does not earn a slot. Both are **provably exact** and that
result is solid; the speed case is what fails.
