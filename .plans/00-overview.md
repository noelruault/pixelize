# 00 — Overview & Synthesis: Scaling pixelize's Nearest-Color Palette Mapping

**Role of this document.** This is the consolidated, judged synthesis of the five
research reports in `.plans/research/` (01 ImageMagick reverse-engineering, 02
algorithm/library survey, 03 measured experiments, 04 informed challenger, 05
cross-disciplinary transfer), grounded against the current pixelize source
(`palette.go`, `distance.go`). It is **planning only** — no pixelize source is
changed. The companion `01-execution-plan.md` turns the verdicts here into a
phased, hybrid-aware build plan.

**The headline recommendation is a HYBRID, not a single winner.** No one method
wins every regime. The right design is a *strategy selector* that dispatches to
the best matcher per `(palette size P, image size N, exact-vs-fast need)`, with a
few cheap, universally-applicable enhancements (run-length collapse, row
parallelism, SoA buffers) layered under all paths. See §9 (verdict per regime)
and the execution plan for the dispatch table.

---

## 1. The problem, and why current pixelize loses on large palettes

### 1.1 What pixelize does today (measured-grounded)

`palette.go::applyNearest` (around line 93) maps every pixel to its nearest
palette color with a **single-threaded linear scan**:

- For the default metric it calls Go stdlib `color.Palette.Index` once per pixel
  (`palette.go:114`); for a custom metric it calls `nearest()` (`palette.go:170`),
  a plain `O(P)` loop over the palette.
- It uses `src.At(x,y)` / `out.SetRGBA(x,y,…)` — **interface-dispatched per-pixel
  access**, which the survey (report 02 §6) flags as a throughput killer versus
  indexing `img.Pix` directly.
- It is **serial**: one goroutine, no row/tile parallelism.
- `Indices` is allocated as `[][]int` (a slice-of-slices), one inner slice per
  column — more allocation and worse locality than a single flat buffer.

Net algorithmic cost: **O(N · P)**, single-core, with per-pixel interface
overhead. `distance.go::EuclideanRGBA` computes squared Euclidean in 16-bit RGBA
(no sqrt — correct for argmin), and report 03 verified that the 8-bit-RGB
unweighted squared-Euclidean used by the prototypes gives the **identical**
nearest index as stdlib's 16-bit alpha-inclusive `color.Palette.Index` (0
mismatches, all sizes/palettes) for opaque images. So the prototype "algorithm A"
(serial linear scan) is a **faithful bit-exact stand-in** for what pixelize ships
today. Every measured number for "A" is therefore directly applicable.

### 1.2 The measured crossover — why large palettes hurt

Because per-pixel cost is `O(P)`, doubling the palette doubles the work. Report 03
measured this directly at 2048×2048 (avg ms):

| algo | P=16 | P=64 | P=256 | 16→256 growth |
|------|-----:|-----:|------:|--------------:|
| **A linear serial (= pixelize today)** | 140.1 | 483.2 | **1898.3** | **13.5×** |
| B linear parallel | 36.0 | 153.9 | 536.4 | 14.9× |
| C kd-tree* | 132.7 | 225.0 | 245.5 | 1.85× |
| D 5-bit LUT | 5.6 | 6.0 | 9.1 | ~flat (1.6×) |
| E 6-bit LUT | 6.0 | 17.1 | 37.2 | 6.2× |

`*` C is not yet bit-exact (prune bug, §6 below).

**The crossover story:**

- pixelize's serial scan (A) grows **~13–15× as the palette grows 16→256**, the
  signature `O(P)` penalty. At 4K/256 it takes **3.7 s**; at 8K/256 it was
  *skipped* in the matrix because it was estimated at ~14–15 s/run.
- The methods whose per-pixel cost is **sub-linear or constant in P** (kd-tree,
  LUT) barely move as P grows. The LUT (D) is essentially **flat** in P (5.6→9.1
  ms over a 16× palette range) — its per-pixel cost is `O(1)`, independent of P.
- **The practical pain point** is exactly the project's stated worry: at large
  palettes on large images, the `O(N·P)` serial scan becomes seconds-to-tens-of-
  seconds, which is where ImageMagick currently beats pixelize.

So pixelize loses on large palettes for two compounding reasons: (1) `O(P)`
per-pixel scaling, and (2) single-threaded execution. Both are addressable, and
(2) is essentially free to fix.

---

## 2. How ImageMagick wins — the exact mechanism, distilled

From report 01's full reverse-engineering of `magick/quantize.c` (IM 6.9.12-98),
and report 04's faithful re-derivation. The `-remap` path with `+dither`
(nearest-color, no dithering) does three things, of which the **assignment** is
the hot loop:

1. **A bit-interleaved 16-ary color tree** (`ColorToNodeId`, line 451). At each
   level it extracts one bit per channel (R→bit0, G→bit1, B→bit2, A→bit3),
   MSB-first, packing them into a 4-bit child index. A root-to-leaf path of depth
   8 spells out the full 8-bit value of every channel. Everything is reduced to 8
   bits via `ScaleQuantumToChar` *before* indexing, so the tree depth is **always
   ≤ 8 regardless of Q8/Q16** — this is precisely why report 01 §3 found the
   Q8↔Q16 benchmark delta negligible (same tree shape, only operand width
   changes). Build is `O(swatch pixels)`, with run-length collapse of identical
   runs.

2. **Descend then bounded-subtree search** (`AssignImageColors` lines 561-615,
   `ClosestColor` line 1072). Per query pixel:
   - **Run-length skip**: consecutive identical pixels collapse to one lookup +
     `count` cheap writes (flat regions become nearly free).
   - **Descent**: walk to the deepest existing node matching the query's high
     bits — `O(8)`, independent of P. The descent loop is `for index=7; index>0;
     index--` — note **`> 0`, not `>= 0`** — so it stops near the root.
   - **Search**: seed `cube.distance` with a value larger than any possible
     squared distance, then `ClosestColor(node_info->parent)` — i.e. search the
     subtree rooted **one level above** the deepest match, near the root.
   - **Distance**: squared Euclidean in RGBA, **no sqrt**, with a
     **partial-distance early-out** cascade (`if distance <= best` after each
     channel) that abandons a candidate the instant its running sum exceeds the
     current best.

3. **Row-parallel, lock-free OpenMP** (`#pragma omp parallel for schedule(static)`
   at line 528). The tree is read-only; each thread privatizes its search scratch
   (`cube = *cube_info`, line 560). Near-linear core scaling because rows are
   independent. (Dithered paths are *not* row-parallel — error diffusion is serial
   along the scan.)

### 2.1 Crucial correctness correction (report 04 §3)

Report 01 §2c hedged that IM's search "may not be provably exact." Report 04
resolved this by reading the descent bound and reproducing it in Go:

- **The strawman "descend then search only the deepest subtree" IS approximate** —
  proven on paper: query `(128,0,0)`'s true nearest `(127,0,0)` (distance² = 1)
  sits in a *different top-level branch* because `127 = 0111_1111` and `128 =
  1000_0000` differ in the top R bit, so a deepest-subtree search returns
  `(255,0,0)` at distance² = 16129.
- **But IM's *actual* plain remap is exact in practice.** Because IM stops the
  descent at `index > 0` and searches `node_info->parent` with a huge seeded
  distance, it effectively searches **at/near the root**, where the
  pathological color *is* visible. So the plain `-dither None`/`+dither` remap
  returns the true nearest.
- **IM's only real approximation is the dither `cache[]`** (`CacheShift = 2` → 6
  bits/channel → 64³ memo table): the first query in a 4×4×4 cell fills it, later
  queries in that cell reuse the answer even if their true nearest differs. This
  governs **dithered** output only; plain remap uses run-length collapse instead.

**Implication for "beating IM on correctness":** the correctness win is **real
but narrow** — it exists against IM's *dithered* output and against any
cell-cache/LUT scheme, but **not** against IM's plain remap, which is already
exact. The bigger wins over IM are architectural (flat slices, no pixel-cache
indirection, no PNG codec in the hot path) and the `O(1)` LUT for the
fast/approximate path.

---

## 3. Consolidated measured results (the trustworthy numbers)

All Go numbers are **pure in-memory quantize time** (PNG decode/encode excluded),
best-of-N where noted; ImageMagick numbers are **end-to-end wall including PNG
I/O** and are a reference point, not apples-to-apples (see the I/O caveat below).
Environment: shared 4-vCPU container, 15 GiB RAM, contended (absolute times carry
noise; best-of is the stable figure), Go 1.24.7, IM 6.9.12-98 Q16.

### 3.1 The regime matrix (avg ms, lower = better; from report 03)

**P = 16 (small palette):**

| algo | 512² | 2K | 4K | 8K |
|------|-----:|----:|----:|-----:|
| A serial (pixelize today) | 8.1 | 140.1 | 248.2 | 988.3 |
| B parallel linear (exact) | 4.1 | 36.0 | 137.0 | 574.7 |
| C kd-tree* | 9.0 | 132.7 | 345.7 | 1607.4 |
| D 5-bit LUT (approx) | 1.0 | 5.6 | 12.0 | 47.0 |
| E 6-bit LUT (approx) | 3.0 | 6.0 | 15.6 | 51.8 |

**P = 256 (large palette):**

| algo | 512² | 2K | 4K | 8K |
|------|-----:|----:|----:|------:|
| A serial (pixelize today) | 121.1 | 1898.3 | 3707.7 | ~14–15 s (skipped) |
| B parallel linear (exact) | 30.4 | 536.4 | 1859.5 | 10648.7 † |
| C kd-tree* | 18.2 | 245.5 | 795.0 | 3224.0 |
| D 5-bit LUT (approx) | 8.1 | 9.1 | 23.8 | 55.5 |
| E 6-bit LUT (approx) | 35.2 | 37.2 | 131.5 | 128.8 |

`*` C not bit-exact (prune bug). `†` 8K/256 B is contention-noisy (best-of 8.4 s).

### 3.2 ImageMagick reference (end-to-end wall, seconds; includes PNG I/O)

| size | P=16 | P=64 | P=256 |
|------|-----:|-----:|------:|
| 512 | 0.18 | 0.31 | 0.71 |
| 2K | 1.88 | 3.27 | 7.93 |
| 4K | 4.19 | 6.30 | 20.39 |
| 8K | 21.80 ‡ | 18.28 ‡ | 35.64 § |

`‡` 8K/16 and 8K/64 noisy (8K/64 < 8K/16 is container noise). `§` 8K/256 = single
sample.

### 3.3 The PNG-I/O caveat (must be honored in any IM comparison)

Measured IM **I/O only** (`convert in.png out.png`, no remap): 512 = 0.12 s, 2K =
2.72 s, 4K = 4.95 s, **8K = 18.6 s**. So at 8K/16, ~18.6 s of the 22.5 s total is
**PNG codec, not remap**. IM's *remap proper* is only a few seconds even at 8K.

**The fair statement:** IM's remap is low-seconds at high res while Go's LUT is
tens of milliseconds; but a large chunk of IM's wall time is PNG I/O that the Go
*quantize-only* timings exclude. Go programs would also pay PNG cost end-to-end
(not separately measured). Report 04 *planned* to isolate IM's remap by
subtracting an identity-convert baseline on raw PPM, but those runs were lost to
an environment failure (see §6) — **so the cleanest available IM-remap-isolated
number does not yet exist and must be measured** (execution plan, Phase 1
benchmark task). Until then, treat the IM comparison as: *Go in-memory remap is
dramatically faster, but a clean end-to-end PNG-included comparison is the honest
benchmark to publish.*

### 3.4 Reconciliation of disagreements / unreliable cells

- **8K/256 B = 10648 ms** is flagged contention-noisy; best-of (8432 ms) is more
  trustworthy. Use the *ratio* C≈3× faster than B at 8K/256, not the absolute B.
- **Parallel scaling sweep (report 03 §8) is unreliable** (ran under concurrent
  load; showed only ~1.3–1.5× from 1→4 cores). **Do not use it.** The trustworthy
  parallel figure is matrix-derived: B (4-core) = 536 ms vs A (serial) = 1898 ms
  at 2K/256 ⇒ **~3.5× effective speedup** from parallelism on 4 cores, matching
  expectation for an embarrassingly-parallel loop.
- **6-bit LUT (E) build cost is visible at small images** (P=256, 2K: 37 ms,
  larger share is the 262144-bucket build). It flattens at 8K (128.8 ms). The
  5-bit LUT (D, 32768 buckets) has a much cheaper build and is flatter, at higher
  accuracy cost.
- **Report 04 obtained NO fresh measurements** (output-channel failure) — every
  challenger cell is `[NOT MEASURED]`. Its trustworthy contributions are the
  *correctness arguments* (IM plain path is exact; the `<=` kd prune fix) and the
  preserved prototype source, **not** any number. All numbers above come from
  report 03.

---

## 4. Every candidate approach, evaluated

For each: idea, complexity, measured/expected cost, exact vs approximate, memory,
winning regime, risk, effort. "Proven now" = measured in report 03. "Strong
signal" = argued/designed but needs a put-it-into-practice experiment.

### 4.1 Linear serial scan — BASELINE (= pixelize today). PROVEN.
- **Idea:** `O(P)` distance loop per pixel, one core.
- **Complexity / cost:** `O(N·P)`. Measured 1898 ms @ 2K/256; 3.7 s @ 4K/256.
- **Exact:** yes (verified == stdlib).
- **Memory:** output only.
- **Wins:** nothing now (dominated by B everywhere at equal exactness). Only
  virtue is simplicity.
- **Risk / effort:** n/a (it is the baseline).

### 4.2 Parallel linear scan (algorithm B). PROVEN — exact, low-risk.
- **Idea:** same exact scan, worker pool over pixel bands, `GOMAXPROCS` workers,
  read-only palette, per-worker scratch.
- **Complexity / cost:** `O(N·P / cores)`. Measured **~3.5× faster than serial**
  on 4 cores (more on bigger machines).
- **Exact:** **yes — byte-identical to A, verified every size/palette.**
- **Memory:** output only.
- **Wins:** **small palettes (P ≤ ~32–64) at any size, when exactness is
  required.** At P=16 it beats the kd-tree (8K/16: B=575 ms vs C=1607 ms).
- **Risk:** very low (no algorithmic change, just parallelism + flat buffers).
- **Effort:** low. **This is the #1 immediate win.**

### 4.3 ImageMagick-style bit-interleaved tree (climb-to-root). PROVEN-by-design (not freshly measured).
- **Idea:** port IM's 16-ary (8-ary for opaque) color tree; descend to deepest
  node, climb parent-by-parent to root calling `ClosestColor` with
  partial-distance early-out; run-length collapse.
- **Complexity / cost:** `O(8)` descent + bounded subtree search per distinct
  pixel; flat in P. Report 04 argues it sits **near the kd-tree** in cost (no
  fresh number).
- **Exact:** **yes** (climb reaches root; prune is distance-correct) — report 04
  §3 Claim B.
- **Memory:** sparse 8-level tree, a few MB at P=256.
- **Wins:** "exact AND palette-scalable," same niche as kd — but report 04
  judges it **dominated**: at small P by linear, at large P by kd (geometry prunes
  better than bit-prefix), on speed everywhere by the LUT. Value is
  compatibility/pedagogy (it is *what IM does*), not a ship recommendation.
- **Risk / effort:** medium effort, low value-add vs kd. **Not recommended to
  ship**; keep as a faithful reference for IM-output comparison if needed.

### 4.4 Exact kd-tree, branch-and-bound (algorithm C, fixed). STRONG SIGNAL — needs fix-and-verify.
- **Idea:** balanced 3-D kd-tree over RGB (median split, axis = depth%3); search
  near child, then far child only if `diff*diff <= bestD`.
- **Complexity / cost:** `O(P log P)` build, ~`O(log P)` + backtracking per query.
  Measured timings are valid (traversal cost unaffected by the prune-equality
  bug): **8K/256: C=3224 ms vs B=10648 ms (~3× faster); but 8K/16: C=1607 ms vs
  B=575 ms (slower at small P).** Near-logarithmic in P (1.85× over 16→256 at 2K).
- **Exact:** **NOT YET** — report 03's prototype differs from exact on 6–9365
  pixels (≤0.22%). Root cause identified: prune used `<` and missed
  equidistant-tie points on the splitting plane. **Fix = use `<=`** (report 04's
  challenger already encodes the fix; report 03's quantlab does not).
- **Memory:** ~40–48 B × P (≤12 KB at 256). Tiny.
- **Wins:** **large palettes (P ≳ 64–256) when exactness is required.** The only
  method that is both exact *and* sub-linear in P.
- **Risk:** medium — must be **re-verified bit-for-bit** against the linear scan
  before shipping (the equidistant-tie correctness is the whole point).
- **Effort:** low-medium (the fix is one operator; the verification is the work).

### 4.5 5-bit LUT (algorithm D). PROVEN — fast, approximate.
- **Idea:** precompute a 32³ = 32768-cell inverse colormap (nearest to each cell
  center); per-pixel = shift+mask+one load.
- **Complexity / cost:** `O(32768 · P)` build (cheap, one-time), **`O(1)`
  per-pixel, independent of P**. Measured **~1.7 ns/pixel**; 8K/256 in **55 ms**;
  ~150–200× faster than serial at 4K/256.
- **Exact:** **no.** Measured ~**8.6–14.4% of pixels differ** from exact on a
  *random* palette (rises with palette density); max color error ≤46 RGB units,
  shrinking as P grows. Two causes: random palette has dense Voronoi boundaries,
  and the bucket-*center* key injects ±4 units/channel on *every* pixel.
- **Memory:** **64 KiB fixed.**
- **Wins:** **fast/preview/batch path at any P/size where bounded error is
  acceptable.** Never a silent default.
- **Risk:** accuracy must be *documented and opt-in*. Error is much larger than
  intuition suggests on neutral palettes (less on tuned palettes).
- **Effort:** low.

### 4.6 6-bit LUT (algorithm E). PROVEN — fast, less-approximate.
- **Idea:** as 5-bit but 64³ = 262144 cells.
- **Complexity / cost:** `O(262144 · P)` build (visible at small images: 37 ms @
  2K/256), `O(1)` per-pixel. 8K/256 in **128.8 ms**; 8K/16 in 51.8 ms.
- **Exact:** **no**, but **~half the error of 5-bit**: ~4.6–7.2% of pixels differ
  (random palette), max error ≤34 units. Cell width 4 vs 8.
- **Memory:** **512 KiB fixed** (1 MiB if `int32` indices for P>256).
- **Wins:** the higher-accuracy approximate variant; the better default for the
  fast path when 5-bit's error is too visible.
- **Risk / effort:** low; same caveats as 5-bit.

### 4.7 Boundary-aware EXACT inverse LUT. STRONG SIGNAL — the standout, needs prototype.
- **Idea (report 05 §A, the cross-disciplinary fusion):** a LUT cell is *wrong*
  only if a Voronoi boundary passes through it. For P references in a fine 3D
  grid, the vast majority of cells lie entirely inside one Voronoi region and are
  **provably exact** — label once, trust forever. Only the thin shell of
  **boundary-straddling cells** (detectable at build time: ≥2 references within
  the cell's diagonal, the MD cell-list "check neighbors within cutoff" test)
  needs a fallback exact scan (or a sub-cell decision). Converts the LUT from
  "approximate everywhere" to "exact except in a small boundary set," i.e. **as
  fast as the approximate LUT on ~most pixels yet bit-exact**.
- **Complexity / cost:** `O(cells)` build with a boundary classification pass;
  per-pixel = `O(1)` for interior cells (the common case) + a bounded exact scan
  for boundary cells. Expected to approach the 6-bit LUT's speed while being
  exact. **No measured number yet** — this is the put-it-into-practice
  experiment.
- **Exact:** **yes, by construction** (interior cells exact; boundary cells fall
  back to exact scan). Tunable: shrink cells / refine boundary regions to bound
  the fallback fraction.
- **Memory:** LUT (256 KiB–1 MiB at 6-bit) + a boundary-cell flag/structure.
- **Wins:** **all P, huge images, when exactness is required** — potentially the
  single best exact path at large N, fusing the LUT's `O(1)` interior with exact
  correctness. The biggest upside in the whole study.
- **Risk:** **the boundary-cell fraction is unmeasured.** With a 6-bit grid and a
  few hundred dense/random references, the boundary shell could be a non-trivial
  fraction of cells (and pixels), eroding the speed win. **Must be measured**
  before committing.
- **Effort:** medium (straddle test + fallback path + verification).

### 4.8 Cross-disciplinary enhancements (report 05) — layered, mostly low-effort.

- **Hamerly/Elkan triangle-inequality pruning + coherence cache.** STRONG
  SIGNAL, low effort. Precompute per-reference nearest-other-reference distance
  `s(c)`; if a query's distance to its current best `< s(best)/2`, no other can be
  closer — skip the rest of the scan. Combined with **caching the previous
  pixel's chosen reference as the initial upper bound** (image scanlines are
  highly color-coherent), the per-pixel scan often terminates almost immediately.
  **Exact**, `O(P)` (or `O(P²)`) precompute, trivial memory. *Hamerly (1 lower +
  1 upper bound) is the right low-D choice; Elkan (k bounds) is for high D and
  wastes memory here.* This is the cheapest way to make the **exact linear scan
  scale better at medium/large P** without a tree — a strong complement to B.
- **Cell-list uniform grid over references + exact ring expansion.** STRONG
  SIGNAL (alternative to kd). Bin P references into a uniform grid; per query,
  scan its cell + expanding rings until the ring's minimum distance ≥ best-so-far.
  **Exact**, `O(1)` amortized for bounded P/cell. Five independent fields (MD,
  point clouds, astronomy, game physics, GIS) converge on this — high confidence.
  Competes with kd at medium/large P; simpler and more cache-friendly. *Either kd
  or cell-grid can fill the exact-large-P slot; pick one after a head-to-head.*
- **Morton (Z-order) cell linearization.** Low effort, exact (pure memory
  layout). Maps 3D cell coords to one cache-friendly integer offset so spatially-
  near cells are memory-near, improving cache hits for the coherent pixel stream.
  This is what IM's tree already exploits implicitly. A nice-to-have layout tweak
  for any LUT/grid.
- **SoA + goroutine-tiled streaming (+ optional AVX2 distance kernel).** Low
  effort for SoA/tiling; medium-high for SIMD. Process R/G/B as separate planes;
  stream horizontal bands to bound memory at 4K/8K; fan out goroutines per band.
  **Exact**, satisfies multicore + bounded-memory requirements. SIMD needs Go
  assembly / `avo` (not pure-source Go) — keep behind a build tag with a pure-Go
  fallback; Go auto-vectorization is weak, so don't rely on it.
- **Banded/streaming processing for 8K.** Essential infrastructure, not an
  algorithm: process in 64–256-row bands so peak memory = `band_rows × width × 4`
  + read-only structure (e.g. ~3.9 MB for a 128-row 8K band vs ~133 MB full
  frame). The search structure is built once and shared read-only across all
  bands — the decisive reason the LUT/grid scales to huge images for free.

---

## 5. What does NOT transfer (avoid these dead ends)

From report 05's honest negative-transfer analysis (and report 02's library
survey). Cataloged so future work does not re-litigate them:

- **The entire high-dimensional ANN stack — HNSW, IVF, Product Quantization,
  Annoy, ScaNN, FAISS-style indexes.** Engineered for D = 100–1500 where exact NN
  suffers the curse of dimensionality. **At D = 3 the curse does not apply**, and
  exact methods (grid, kd, smart linear scan) beat all of them. This is **FAISS's
  own documented position** (low-D "is better addressed with tree-based
  structures… exact search at logarithmic time; tree-based methods do not scale
  above dim 10"). PQ is pointless at D=3 (1–3 trivial subspaces); HNSW/Annoy pay
  graph/tree overhead that only amortizes in high D. **Skip the whole stack.** Its
  one transferable kernel (coarse-bin-then-scan) we already get cleanly from MD
  cell lists.
- **HEALPix equal-area sphere pixelization (astronomy).** Solves equal-area on a
  *sphere*; our color cube is flat Euclidean. Non-transferring — but it
  *re-confirms* the "uniform pixelization + small per-cell scan" pattern from a
  third field.
- **H3 hexagonal grids (GIS).** Hexagons are a 2D-sphere optimization; there is
  no regular hex honeycomb in 3D. Non-transferring (the "k-ring expanding query"
  concept is just cell-list ring expansion under another name).
- **R-trees (GIS).** Target overlapping extents and disk pages; we have points,
  in-RAM, tiny set. Skip.
- **kd-tree / ball-tree / BVH *over the references*** when P is only a few
  hundred — marginal over a grid or smart linear scan; the log factor is swamped
  by constant overhead and cache misses. (kd becomes interesting only if P grows
  into the thousands. We still keep a *fixed* exact kd as the large-P exact
  candidate because report 03 measured it ~3× faster than parallel-linear at
  8K/256 — but a uniform grid is a viable, possibly simpler, alternative.)
- **Tetrahedral / trilinear *output-color* interpolation (color science).** You
  cannot average *labels* ("reference #3" + "reference #7" is meaningless), so it
  does not reduce LUT *labeling* error for hard quantization. Only relevant if a
  soft/dithered palette-*blend* output mode is ever wanted (probably not). The
  tetrahedral *point-location* trick (sort fractional coords to pick 1 of 6
  tetrahedra) *does* transfer as a fast sub-cell decision primitive for the
  boundary-LUT, if needed.
- **GPU 3D-texture sampling.** No portable pure-Go GPGPU; cgo/Vulkan breaks the
  pure-Go constraint. The CPU-SIMD analogue (§4.8) is the realistic transfer.
- **IM's `cache[]` memoization for the *non-dithered* path.** Buys nothing over
  run-length collapse and *introduces* IM's only real inexactness. Only consider
  it if/when dithering is added (where run coherence breaks).

---

## 6. Trust & provenance notes (honest accounting)

- **Report 03 numbers are the trustworthy measured baseline.** They were taken on
  a contended 4-vCPU container — absolute times carry noise, best-of-N is the
  stable figure, and the parallel-scaling sweep is explicitly unreliable. The
  *ratios and scaling shapes* are robust; treat individual large-N absolute cells
  (esp. 8K/256 B) with the noted caveats.
- **Report 04 obtained no fresh measurements** (tool-output channel failed
  mid-session). Its value is (a) the correctness analysis — IM's plain path is
  exact, the kd prune must be `<=` — and (b) the **preserved prototype source**
  (`/tmp/challenger/main.go`, reproduced verbatim in
  `04-informed-challenger-data.txt`). **Do not cite any challenger number;** they
  are all `[NOT MEASURED]`.
- **The `/tmp` prototypes are present** (verified): `/tmp/quantlab/main.go` (blind
  track: A/B/C/D/E) and `/tmp/challenger/main.go` (informed track: parallel-linear,
  exact-kd with the `<=` fix, IM-climb tree, 6-bit LUT, IM dither cache). The
  challenger source is also preserved verbatim in `04-…-data.txt` as a backup. If a
  future container reset loses them, rebuild from that data file.
- **`bench/compare.sh` already exists** (verified) and is exactly the harness to
  reuse: it has `build` / `verify` / `matrix` / `im` (incl. remap-vs-I/O isolation)
  / `accuracy` / `all` subcommands, reuses the `/tmp` prototypes if present, and
  rebuilds the challenger from `04-…-data.txt` otherwise. The Phase 1 work is to
  *run and extend* it (e.g. on an idle box), not to create it from scratch.
- **The kd-tree timings are valid even though the prototype was inexact** — the
  prune-equality bug changes which equidistant point wins, not the traversal cost.

---

## 7. The exact-vs-approximate axis (how to think about it)

- **Exact** (bit-for-bit identical to the linear scan / stdlib): parallel-linear
  (B), exact kd (C-fixed), IM-climb tree, cell-grid, boundary-aware exact LUT,
  Hamerly-pruned scan. These are interchangeable on *output*, differ only on
  *speed by regime*.
- **Approximate** (bounded error, documented): 5-bit LUT (D, ~9–14% px differ),
  6-bit LUT (E, ~5–7%), IM's dither cache. These are **opt-in fast/preview/batch**
  paths, never silent defaults, always with a stated pixel-diff % and max color
  error budget.
- The **boundary-aware exact LUT** is the bridge: it aims to deliver
  approximate-LUT speed with exact output, by paying the exact scan only on
  boundary cells.

---

## 8. Memory & huge-image (4K/8K) behavior

- 8K RGBA input ≈ 133 MB; paletted index output ≈ 33 MB (1 B/px) or ~127 MB
  (`int32`). The **search structures are tiny** by comparison: 5-bit LUT 64 KiB,
  6-bit LUT 512 KiB–1 MiB, kd ≤12 KB, IM-tree a few MB.
- The decisive technique is **band streaming**: process 64–256-row horizontal
  bands, reuse buffers, keep the structure built-once and read-only. Peak working
  memory becomes `band_rows × width × 4` + structure (e.g. ~3.9 MB per 128-row 8K
  band) instead of the full frame.
- The LUT/grid is the cleanest parallel + streaming story: read-only → zero
  contention, trivially tile-able, no per-band rebuild. Trees are also read-only
  and stream fine. Any *mutable* per-pixel cache (lazy memo) would need
  per-worker copies or atomics — prefer the fully-precomputed LUT or per-worker
  scratch.

---

## 9. Clear-eyed verdict per regime

Using report 03's measured crossovers. "Exact" = bit-identical to current
pixelize; "Fast" = bounded-error opt-in.

| Regime | Exact winner (now) | Exact winner (after Phase 2) | Fast/approx winner |
|--------|--------------------|------------------------------|--------------------|
| **Small P (≤~32), small image (≤512²)** | parallel-linear B (or even serial — all <130 ms) | same | 5-bit LUT (overkill; B is fine) |
| **Small P (≤~32), large image (2K–8K)** | **parallel-linear B** (beats kd at small P; 8K/16 B=575 ms vs C=1607 ms) | B (+ Hamerly/coherence to push the crossover) | 5-/6-bit LUT (47–52 ms @ 8K) |
| **Large P (≳64–256), small image (≤512²)** | parallel-linear B (30 ms @ 512²/256) | B; kd if it proves faster | 5-bit LUT (8 ms) |
| **Large P (≳64–256), large image (2K–8K)** | parallel-linear B *as fallback* (536 ms–10.6 s — the pain regime) | **exact kd-tree (fixed)** (~3× faster than B at 8K/256) **or boundary-aware exact LUT** (if it proves out) | **6-bit LUT** (128 ms @ 8K/256, ~half 5-bit's error) |
| **Huge image (8K), any P, speed-critical, bounded error OK** | — | — | **5-bit LUT** (55 ms @ 8K/256, palette-independent ~1.7 ns/px) |
| **Match/beat IM plain remap** | parallel-linear B / kd = IM (both exact) | same | LUT (much faster, slightly approximate) |
| **Beat IM dithered output on correctness** | exact kd / B (0 non-nearest vs IM cache's >0) | same | n/a |

**Synthesis verdict:**

1. **The proven, ship-now exact default is parallel-linear (B).** It is exact,
   ~3.5× faster than today on 4 cores, and the *best* exact choice for small
   palettes at every size. Lowest risk.
2. **The proven fast/preview path is the 6-bit LUT** (with 5-bit as an even-faster
   higher-error option), behind an opt-in flag with documented accuracy.
3. **The large-palette exact gap** (where B becomes seconds at 8K/256) is filled
   by **one of: fixed-and-verified exact kd, OR the boundary-aware exact LUT** —
   both are *strong signals needing a put-it-into-practice experiment*, not
   ship-now items. The boundary-aware exact LUT is the higher-upside bet (exact +
   `O(1)` interior) but the higher-uncertainty one (boundary fraction unmeasured).
4. **Layer the cheap enhancements under everything:** run-length collapse, SoA +
   flat-buffer access (kill the `At/SetRGBA` interface overhead), band streaming
   for 8K, and the Hamerly + previous-pixel-coherence pruning to make the exact
   scan scale better at medium P.

This is a **hybrid with a regime-based selector**, exactly as the framing
demands — see `01-execution-plan.md` for the dispatch table and phased build.
