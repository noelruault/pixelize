# 01 — Execution Plan: Hybrid Nearest-Color Matcher for pixelize

**Companion to** `00-overview.md` (read it first for the evidence and verdicts).
This plan is **actionable**: someone can pick up Phase 1 and start without
rerunning the research. It is **planning only** — it changes no pixelize source.

**Guiding principle (honor the framing):** do NOT force a single winner. Ship a
**regime-based strategy selector** that dispatches to the best matcher per
`(palette size P, image size N, exact-vs-fast need)`, with cheap enhancements
layered under all paths. Separate **proven, low-risk wins (Phase 1)** from
**put-it-into-practice experiments (Phase 2)** from **integration + borrowed
enhancements (Phase 3)**.

---

## 0. Reality check before you start

- **The `/tmp` prototypes are present** (verified): `/tmp/quantlab/main.go` (blind
  track A/B/C/D/E) and `/tmp/challenger/main.go` (informed track: parallel-linear,
  exact-kd with the `<=` fix, IM-climb tree, 6-bit LUT, IM dither cache). The
  challenger source is also preserved verbatim at the bottom of
  `.plans/research/04-informed-challenger-data.txt` as a backup if a container
  reset loses `/tmp`.
- **`bench/compare.sh` already exists** and is the harness to reuse: subcommands
  `build` / `verify` / `matrix` / `im` (incl. remap-vs-I/O isolation) / `accuracy`
  / `all`; it reuses the `/tmp` prototypes if present and rebuilds the challenger
  from `04-…-data.txt` otherwise. Phase 1 = run and extend it, not create it.
- **Distance equivalence is established:** 8-bit-RGB unweighted squared Euclidean
  == stdlib `color.Palette.Index` for opaque images (0 mismatches, report 03). Any
  new exact matcher must be verified against `color.Palette.Index` /
  `distance.go::EuclideanRGBA` as the oracle, including the alpha term for
  non-opaque inputs.
- **Environment for measurement:** the research box was a contended 4-vCPU
  container. For trustworthy scaling curves, **run benchmarks on an idle machine**
  and report best-of-N. The parallel-scaling sweep in report 03 §8 is unreliable;
  re-measure it cleanly.

---

## 1. Recommended architecture: the strategy selector

A single internal interface plus a dispatcher. Conceptually (names illustrative;
do not write code yet):

```
type matcher interface {
    // index of nearest palette entry for an 8-bit RGBA pixel
    nearest(r, g, b, a uint8) int
    exact() bool            // true => bit-identical to the linear-scan oracle
}
```

**Dispatch (`selectMatcher(P, N, mode)`)** chooses the implementation. `mode` is
one of `Exact` (default) or `Fast` (opt-in, documented approximate).

### 1.1 Default-per-cell dispatch table

| P (palette) | N (image) | mode = Exact (default) | mode = Fast (opt-in) |
|-------------|-----------|------------------------|----------------------|
| **≤ ~32 (small)** | any | **parallel-linear** (B) | **6-bit LUT** (or 5-bit if even faster needed) |
| **~32 – ~64 (medium)** | ≤ ~1 MP | **parallel-linear** (B) + Hamerly/coherence pruning | 6-bit LUT |
| **~32 – ~64 (medium)** | > ~1 MP (2K–8K) | **parallel-linear** (B) + pruning; promote to **exact kd / boundary-LUT** once proven (Phase 2) | 6-bit LUT |
| **≳ 64 – several hundred (large)** | ≤ ~1 MP | **parallel-linear** (B) (30 ms @ 512²/256) | 6-bit LUT |
| **≳ 64 – several hundred (large)** | > ~1 MP (2K–8K) | **exact kd / boundary-aware exact LUT** (Phase 2); **parallel-linear B is the fallback** until those land | **6-bit LUT** (5-bit for max speed) |

**Crossover thresholds are placeholders to be confirmed by the clean benchmark.**
Report 03 puts the linear↔tree crossover near P ≈ 32–64 (P=16 favors B; P=256
favors kd). Lock the exact numbers from the Phase 1 / Phase 2 benchmark sweeps and
bake them into `selectMatcher` as named constants (e.g. `smallPaletteMax`,
`largeImageMinPixels`).

### 1.2 Cross-cutting infrastructure (under every matcher)

- **Flat-buffer access:** read from `src.Pix` and write a flat `[]int`/`[]uint16`
  index buffer + the RGBA output directly — eliminate the per-pixel
  `At()/SetRGBA()` interface calls in today's `applyNearest`. Also replace the
  `[][]int` `Indices` allocation with a flat buffer (keep the public
  `Pattern.Indices [][]int` shape via a thin view if the API must be preserved).
- **Run-length collapse:** one lookup per run of identical adjacent pixels, then
  bulk-write the index (IM's `x += count`). Free win on flat/pixel-art regions.
- **Row/band parallelism:** worker pool over contiguous row bands, `GOMAXPROCS`
  workers, read-only shared structure, per-worker scratch. Gate parallelism on
  `N > threshold` (~1–2 MP) so tiny images don't pay goroutine overhead.
- **Band streaming for 8K:** process 64–256-row bands to bound peak memory; build
  the structure once, stream all bands against it.
- **`context` cancellation:** preserve today's `ctx.Err()` checks (per band, not
  per pixel, to avoid overhead).

---

## 2. Phase 1 — Proven, low-risk wins (build first)

**Goal:** exact output, big speedup, zero accuracy risk on the default path; plus
an opt-in fast path with documented accuracy. Everything here is *measured* in
report 03.

### 2.1 Task: Parallelize the existing exact scan (the #1 win)
- **Start from:** report 03 algorithm B / the challenger's `bruteMatcher` +
  `remapParallel` (recoverable from `04-…-data.txt`). Current code:
  `palette.go::applyNearest` (line 93) and `nearest()` (line 170).
- **Do:** worker pool over row bands; per-worker no shared mutable state; read
  `src.Pix` directly; flat index/output buffers; run-length collapse in the inner
  loop; keep the exact squared-Euclidean metric (and the stdlib-equivalent path).
- **Acceptance criteria:**
  - **Bit-for-bit identical** output to the current `applyNearest` (and to
    `color.Palette.Index`) on a corpus of real + synthetic images, all
    sizes/palettes — **0 mismatching pixels** (this is the exactness gate).
  - **Measured speedup ≥ 3× on 4 cores** at 2K/256 vs current serial (report 03
    saw ~3.5×); near-linear core scaling on bigger machines (confirm on an idle
    box — the research sweep was unreliable).
  - No new heap allocation in the hot loop (`b.ReportAllocs()` ≈ flat across
    sizes; allocation dominated by the index/output buffer only).
- **Benchmark:** the regime matrix (P ∈ {16,64,256} × N ∈ {512²,2K,4K,8K}) via
  the new `bench/compare.sh`; compare against the report 03 "A" and "B" numbers.

### 2.2 Task: Add the 6-bit LUT behind an opt-in Fast/preview flag
- **Start from:** report 03 algorithm E / challenger `buildLUT6` + `lut6`. Use
  proper 6→8-bit replication for the cell key (`r<<2 | r>>4`), not zero-fill;
  build in parallel; cell-center keys.
- **Do:** add a `Fast` mode (or `Quality: Preview` option) on `ApplyOptions`.
  Default stays `Exact`. Offer 5-bit as an even-faster, higher-error sub-option.
- **Acceptance criteria:**
  - **Speed:** 8K/256 in **≤ ~150 ms** (report 03: 128.8 ms for 6-bit; 55.5 ms
    for 5-bit), palette-independent `O(1)` per-pixel.
  - **Accuracy budget (documented, enforced in tests):** on a *random* palette,
    6-bit ≤ **~8% of pixels differ** with **max color error ≤ ~35 RGB units**
    (report 03 measured 4.6–7.2% / ≤34); 5-bit ≤ **~15%** / ≤46. Assert these
    bounds in a test so regressions are caught. Document that *tuned* palettes do
    much better.
  - **Memory:** 6-bit = 512 KiB–1 MiB fixed; 5-bit = 64 KiB. Verified bounded at
    8K (structure is constant-size; only the band buffers scale).
  - **Never the silent default** — Fast must be explicitly requested, with the
    accuracy caveat in the doc comment and CLI help.
- **Benchmark:** same matrix; also record the `% pixels differ` and `max color
  error` vs the exact path per (P, size) — this is the accuracy regression guard.

### 2.3 Task: Run and extend the benchmark + verification harness (`bench/compare.sh`)
- **`bench/compare.sh` already exists** with `build`/`verify`/`matrix`/`im`/
  `accuracy`/`all` subcommands (it reuses the `/tmp` prototypes or rebuilds the
  challenger from `04-…-data.txt`). Phase 1 = **run it on an idle box**, confirm it
  reproduces report 03's ratios, and extend its CSV/output as needed
  (`algo,pal,size,pixels,build_ms,remap_ms,best_ms,mpix_s,pct_differ,max_err`).
- **IM-remap isolation (the `im` subcommand — the honest IM number report 04 never measured):**
  time `convert in.ppm -dither None -remap palN.ppm out.ppm` (full) minus
  `convert in.ppm out.ppm` (identity I/O baseline) on **raw PPM** to strip the PNG
  codec; best-of-3. This produces the apples-to-apples IM-remap-only figure that
  is currently missing (overview §3.3). Also keep the **end-to-end PNG-included**
  comparison, since that is what users actually experience.
- **Verification subcommand:** for every exact matcher, count pixels whose chosen
  color is strictly farther than the true nearest (ties not counted) — must be 0.
  Reuse the challenger's `verify` design (recoverable from `04-…-data.txt`).
- **Acceptance:** harness reproduces report 03's headline ratios within container
  noise on a comparable box; CI-runnable as a regression guard.

**Phase 1 exit criteria:** exact default is ~3×+ faster and bit-identical; 6-bit
Fast path is tens-of-ms at 8K with documented, test-enforced accuracy bounds;
benchmark harness exists and includes the IM-remap-isolated comparison.

---

## 3. Phase 2 — Put-it-into-practice experiments (unlock the big wins)

**Goal:** fill the **large-palette exact** gap (where parallel-linear B is
seconds at 8K/256) with a method that is exact *and* sub-linear/`O(1)`. Two
candidates; prototype and measure both, then pick.

### 3.1 Task: Fix-and-verify the exact kd-tree (branch-and-bound)
- **Start from:** the challenger's `kdTree`/`kdSearch` (already encodes the fix:
  far-child prune `diff*diff <= bestD`, the `<=` that handles equidistant ties —
  the precise bug report 03's quantlab kd hit with `<`). Recover from
  `04-…-data.txt`.
- **Do:** integrate as an exact matcher; row-parallel; per-worker `kdSearch`
  scratch; run-length collapse.
- **Acceptance criteria:**
  - **Exactness: 0 non-nearest pixels** vs the linear-scan oracle, every
    size/palette (this is non-negotiable — gate in CI). Re-verify specifically the
    equidistant-tie cases the `<` bug missed.
  - **Speed:** confirm report 03's shape — **~3× faster than parallel-linear at
    8K/256** (C=3224 ms vs B=10648 ms), and confirm it is **slower at small P**
    (8K/16: C=1607 vs B=575) so the selector routes correctly.
  - Establish the **exact P crossover** where kd overtakes B; bake into
    `selectMatcher`.
- **Benchmark:** matrix + a dedicated P-sweep (P ∈ {16,32,48,64,96,128,256}) at
  2K and 8K to pin the crossover.
- **Note:** a uniform **cell-list grid + exact ring expansion** (report 05 §1) is
  a viable *alternative* to kd for this same slot — simpler, cache-friendlier,
  five-field-validated. If kd integration is fiddly or the crossover is poor,
  prototype the grid as a substitute. Pick whichever is faster *and* verifiably
  exact; do not ship both.

### 3.2 Task: Prototype & validate the boundary-aware EXACT LUT (the standout)
- **Start from:** scratch (`/tmp/pixelize-proto-boundary-lut/`), guided by report
  05 §A and §2. Reuse the 6-bit LUT build and the linear-scan oracle from the
  challenger harness.
- **Do:**
  1. Build a 6-bit (and try 5-bit / 7-bit) inverse-colormap grid.
  2. **Classify each cell** as *interior* (entirely inside one Voronoi region) or
     *boundary* (a Voronoi boundary crosses it). Cheap straddle test (report 05
     §2/§A): a cell is boundary iff ≥2 references lie within the cell's diagonal of
     it — the MD cell-list "neighbors within cutoff" logic.
  3. Per pixel: interior cell → `O(1)` LUT label (provably exact); boundary cell →
     fallback **exact scan** (or grid ring-expansion, or sub-cell tetrahedral
     point-location).
- **Acceptance criteria:**
  - **Exactness: 0 non-nearest pixels** vs the oracle (the whole point). Gate in
    CI.
  - **MEASURE the boundary-cell and boundary-*pixel* fraction** across P ∈
    {16,64,256, several hundred} and grid resolutions (5/6/7-bit), on both random
    and tuned palettes. **This is the make-or-break number** (overview §4.7 risk):
    if the boundary shell captures a large fraction of *pixels* on dense palettes,
    the fallback scans erode the speed win.
  - **Speed target:** approach the 6-bit LUT's throughput (tens-of-ms at 8K) while
    being exact — i.e. the interior fast path must dominate. Quantify the
    speed-vs-exactness-vs-grid-resolution tradeoff.
  - Characterize memory (LUT + boundary flag structure; should stay ≤ ~1–2 MiB).
- **Benchmark:** matrix + boundary-fraction table + head-to-head vs exact-kd and
  parallel-linear at large P / 8K.
- **Decision rule:** accept if, in its target regime (large P, huge image), it
  gives a meaningful speedup over parallel-linear **while being provably exact**,
  and the boundary fallback fraction is small enough that throughput stays close
  to the plain LUT. If the boundary fraction is too high on realistic palettes,
  fall back to exact-kd / cell-grid for the large-P-exact slot and keep the plain
  LUT only as the Fast path.

**Phase 2 exit criteria:** at least one *verified-exact, sub-linear/`O(1)`* method
for large P at large N is integrated (kd, cell-grid, or boundary-LUT), with the
crossover thresholds measured and the boundary-fraction characterized.

---

## 4. Phase 3 — Integration, the selector, and proven enhancements

**Goal:** wire the matchers behind the strategy selector and add the
cross-disciplinary enhancements that *proved out* in Phases 1–2.

### 4.1 Task: Implement `selectMatcher(P, N, mode)` with measured thresholds
- Encode the dispatch table (§1.1) with the **measured** crossover constants from
  Phases 1–2. Default mode `Exact`; `Fast` opt-in.
- **Acceptance:** for every cell of the regime matrix, the selector picks the
  method that the benchmark shows is fastest *at the required exactness*; a test
  asserts the routing. End-to-end, pixelize is never slower than today and is
  dramatically faster in the large-P / large-N cells.

### 4.2 Task: Layer in the cheap, proven enhancements
- **Hamerly + previous-pixel coherence pruning** on the linear/scan paths
  (report 05 §2/§7): per-reference nearest-other-distance `s(c)`; seed each pixel's
  upper bound with the previous pixel's chosen reference (scanline coherence).
  **Exact**, trivial memory. Acceptance: measurably extends the regime where the
  exact scan stays competitive (pushes the linear↔tree crossover to higher P);
  still bit-exact.
- **Morton (Z-order) LUT/grid layout** if a measured cache-hit improvement
  justifies it (report 05 §8). Pure layout change, exact. Acceptance: a
  non-trivial throughput gain on the LUT path; otherwise skip as not worth the
  complexity.
- **SoA + optional AVX2 distance kernel** behind a build tag, with a pure-Go
  fallback (report 05 §9). Only if Phase 1/2 benchmarks show the scan paths are
  compute-bound (not memory-bound) at the target sizes. Acceptance: exact, pure-Go
  fallback always present, measurable speedup on the scan path.

### 4.3 Task: Dithering path (if/when in scope)
- Today `applyDither` uses stdlib `draw.FloydSteinberg`. If a faster/parallel
  dither is wanted, note that error diffusion is **serial along the scan** (no row
  parallelism) and that a color→index memo cache (IM's `cache[]`) helps only here
  (run coherence is broken by dithering). Out of scope for the nearest-color
  speedup; documented so it isn't accidentally "fixed" with the wrong tool.

**Phase 3 exit criteria:** the hybrid selector is live; enhancements that proved
out are in; CI guards exactness and the accuracy/speed budgets.

---

## 5. Acceptance criteria summary (all phases)

- **Exactness (every path claiming exact):** 0 pixels strictly farther than the
  true nearest vs the linear-scan / `color.Palette.Index` oracle, all
  sizes/palettes, including equidistant-tie and non-opaque-alpha cases.
  CI-gated.
- **Speed targets (idle box, best-of-N):**
  - Exact default (parallel-linear): **≥ 3× faster than current serial** at
    2K/256; near-linear core scaling.
  - Large-P exact (Phase 2): **~3× faster than parallel-linear at 8K/256** (kd
    baseline) or better (boundary-LUT).
  - Fast LUT: **8K/256 ≤ ~150 ms** (6-bit) / ~55 ms (5-bit), palette-independent.
- **Accuracy budget (approximate paths, test-enforced):** 6-bit ≤ ~8% px differ,
  max err ≤ ~35 (random palette); 5-bit ≤ ~15%, ≤46. Documented as worst-case
  neutral; tuned palettes better.
- **Memory (bounded at 8K):** structures ≤ ~2 MiB; peak working memory bounded by
  the streaming band size, not the full frame. Verified via `runtime.MemStats` /
  RSS in the harness.
- **Benchmark:** `bench/compare.sh` reproduces the regime matrix and the
  IM-remap-isolated + IM-end-to-end comparisons.

---

## 6. Risks, open questions, and what to measure to resolve them

| # | Risk / open question | How to resolve (what to measure) | Phase |
|---|----------------------|----------------------------------|-------|
| 1 | **Boundary-cell fraction** of the exact LUT may be large on dense/random palettes, killing its speed advantage. | Measure boundary-cell *and boundary-pixel* fraction across P and grid resolution, random vs tuned palettes (§3.2). | 2 |
| 2 | **Exact kd crossover** (P where kd beats parallel-linear) is only roughly known (≈ 32–64). | Dedicated P-sweep at 2K and 8K; pin the constant for the selector. | 2 |
| 3 | **Parallel scaling curve** is unmeasured cleanly (research sweep was contended, showed only ~1.3–1.5×; matrix implies ~3.5× on 4 cores). | Re-run GOMAXPROCS=1..N sweep on an **idle** box. | 1 |
| 4 | **IM-remap-isolated time** was never measured (report 04 env failure); current IM numbers are PNG-I/O-contaminated. | PPM full-minus-identity-baseline subtraction (§2.3). | 1 |
| 5 | **Accuracy on tuned vs random palettes** — report 03 used random (pessimistic); real palettes do better, but by how much? | Re-measure LUT accuracy on representative tuned palettes (e.g. brand/LEGO ramps). | 1–2 |
| 6 | **Alpha / non-opaque inputs** — research assumed opaque RGB; stdlib metric is 4D RGBA. | Verify exactness with the alpha term included; ensure LUT/grid key handles alpha or documents opaque-only. | 1 |
| 7 | **kd vs cell-grid** for the large-P-exact slot — which is faster *and* simpler? | Head-to-head at large P / 8K; ship only the winner. | 2 |
| 8 | **`/tmp` prototypes could be lost on a container reset** (currently present). | Challenger is recoverable verbatim from `04-…-data.txt` (and `bench/compare.sh` auto-rebuilds it); quantlab would need reconstruction from report 03 §3, or just reuse challenger. | 1 |
| 9 | **Go has weak auto-vectorization**; SIMD needs asm/`avo` (not pure-source Go). | Only pursue SIMD if scan paths prove compute-bound; keep behind build tag with pure-Go fallback. | 3 |

---

## 7. One-screen "start here" for Phase 1

1. `bench/compare.sh build` (uses the existing `/tmp` prototypes, or rebuilds the
   challenger from `.plans/research/04-informed-challenger-data.txt`).
2. `bench/compare.sh verify` to confirm parallel-linear and exact-kd give **0
   non-nearest** (kd has the `<=` fix).
3. In pixelize, replace `applyNearest`'s serial `At/SetRGBA` loop with a
   row-banded parallel scan reading `src.Pix`, flat buffers, run-length collapse —
   **keeping output bit-identical**. Verify against `color.Palette.Index`.
4. Add the 6-bit LUT as an opt-in `Fast` mode with the documented accuracy bounds
   and a test asserting them.
5. Create `bench/compare.sh`; run the regime matrix on an idle box; record CSV +
   the IM-remap-isolated comparison. Confirm ~3×+ exact speedup and the LUT's
   tens-of-ms-at-8K numbers.

Everything beyond this (exact kd / cell-grid, boundary-aware exact LUT, the
selector, and the borrowed enhancements) is Phases 2–3, gated on the measurements
above.
