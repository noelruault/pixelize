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

**Optimization target (the star): wall-clock time.** Minimize per-image elapsed
time. CPU and RAM are spendable to reduce wall time, BUT only when the trade is
efficient:

- **Efficiency gate.** Adopt a faster-but-heavier variant over the current best
  only if the proportional wall-time reduction is **>=** the proportional increase
  in resource cost. Example: +20% CPU work for only -10% time is **rejected** (cost
  20 > gain 10). Faster-and-not-heavier: always take it. Tie on time: take the
  leaner one.
- **"CPU cost" means total CPU work (core-seconds), not utilization.** Using more
  of the cores you already have via parallelism keeps total work ~flat while wall
  time drops, so **parallelism always passes** the gate. The gate bites only when a
  variant does genuinely more total work, makes redundant passes, holds more memory,
  or uses a costlier metric for its speed.
- **Two hard exceptions that override "spend freely":** (a) never let memory growth
  push 8K toward OOM — an OOM-kill is infinitely slow, so band streaming stays
  available as the memory-safety lever; (b) never let "faster" silently downgrade
  the **exact default** to an approximate path — exact stays the default, Fast/
  approximate is opt-in only. Within the exact set the selector picks the lowest
  measured time; "fastest overall" applies only to the explicitly-opted-in Fast mode.

**RE-PHASING (report 04 now COMPLETE and MEASURED — see `00-overview.md` §2.1,
§3.5).** Report 04's measured results change the phasing:
- **The exact kd-tree branch-and-bound moves UP from a Phase-2 experiment into the
  Phase-1 / early-build set.** It is now MEASURED bit-exact (0% non-nearest vs brute
  force at P=16/64/256) with the `<=` far-child prune, and measured fast (512²/256 =
  12.5 ms; 2K/256 = 351 ms; 4K ≈ 0.5 s; ~30× faster than ImageMagick at 4K). It is no
  longer speculative — the only remaining work is integration + a standing
  bit-for-bit CI gate, not a "does it work" experiment.
- **The exact large-palette win over ImageMagick is now PROVEN, not hypothetical:**
  IM's plain `-remap` is ~22% non-nearest at P=64/256 (measured, triple-confirmed),
  so our exact kd / parallel-linear are BOTH more correct (0% vs ~22%) AND faster
  (~30× at 4K). This is a headline competitive differentiator.
- **The boundary-aware exact LUT remains the one high-upside Phase-2 experiment**
  (its boundary-pixel fraction is still unmeasured). Parallel-linear stays the
  small-P exact path; the 6-bit LUT stays the opt-in fast path.
- **Do NOT port IM's tree literally:** report 04 measured the faithful climb-to-root
  variant at 8K/64 = 146 s — exact but unusable. kd gives exactness AND speed.

---

## 0. Reality check before you start

- **The `/tmp` prototypes are present** (verified): `/tmp/quantlab/main.go` (blind
  track A/B/C/D/E) and `/tmp/challenger/main.go` (informed track: parallel-linear,
  **exact-kd with the `<=` fix — report 04 MEASURED it bit-exact, 0% non-nearest at
  P=16/64/256, and ~30× faster than IM at 4K**, IM-climb tree, 6-bit LUT, IM dither
  cache). The challenger source is also preserved verbatim at the bottom of
  `.plans/research/04-informed-challenger-data.txt` as a backup if a container
  reset loses `/tmp`. The kd is now the proven large-P exact path, not a speculative
  prototype.
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
| **~32 – ~64 (medium)** | > ~1 MP (2K–8K) | **parallel-linear** (B) + pruning, with **exact kd (Phase 1, proven)** at the upper end of P | 6-bit LUT |
| **≳ 64 – several hundred (large)** | ≤ ~1 MP | **exact kd** (PROVEN, 12.5 ms @ 512²/256) or parallel-linear (B, 30 ms) | 6-bit LUT |
| **≳ 64 – several hundred (large)** | > ~1 MP (2K–8K) | **exact kd branch-and-bound (Phase 1, PROVEN exact + fast)** — beats IM on correctness (0% vs ~22%) and speed (~30× at 4K); boundary-aware exact LUT is the optional Phase-2 upside | **6-bit LUT** (5-bit for max speed) |

**Crossover thresholds are placeholders to be confirmed by the clean benchmark.**
Report 04 confirms the linear↔kd crossover near P ≈ 64–256 (P=16: 8K linear 1171 ms <
kd 1897 ms, so linear wins; 2K/256: kd 351 ms < linear 451 ms, so kd wins; scales far
better at 8K). Lock the exact numbers from the Phase 1 benchmark sweeps and bake them
into `selectMatcher` as named constants (e.g. `smallPaletteMax`,
`largeImageMinPixels`). **Both exact paths (parallel-linear and kd) are now measured
bit-exact**, so the selector chooses purely on speed within the exact set.

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
an opt-in fast path with documented accuracy. Everything here is *measured* (the
linear/LUT methods in report 03; the **exact kd** now also measured exact-and-fast
in report 04, which is why it joins Phase 1 — §2.4 below).

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
- **IM-remap isolation (the `im` subcommand — reproduce report 04's measured IM number):**
  time `convert in.ppm -dither None -remap palN.ppm out.ppm` (full) minus
  `convert in.ppm out.ppm` (identity I/O baseline) on **raw PPM** to strip the PNG
  codec; best-of-3. Report 04 already measured this (IM remap-only 0.41 / 4.78 /
  14.99 / 49.95 s at 512/2K/4K/8K, overview §3.3); the harness re-confirms it on an
  idle box and keeps it as the standing IM comparison. Also keep the **end-to-end
  PNG-included** comparison, since that is what users actually experience. Record IM's
  **% non-nearest** too (~0.29% at P=16, ~22% at P=64/256) — the correctness baseline
  the exact paths beat.
- **Verification subcommand:** for every exact matcher, count pixels whose chosen
  color is strictly farther than the true nearest (ties not counted) — must be 0.
  Reuse the challenger's `verify` design (recoverable from `04-…-data.txt`).
- **Acceptance:** harness reproduces report 03's headline ratios within container
  noise on a comparable box; CI-runnable as a regression guard.

### 2.4 Task: Integrate the exact kd-tree branch-and-bound (PROMOTED from Phase 2 — now PROVEN)
- **Why it is Phase 1 now:** report 04 (COMPLETE, MEASURED, triple-confirmed) shows
  the `<=`-prune kd is **bit-exact (0% non-nearest vs brute force at P=16/64/256)**
  and **fast** (512²/256 = 12.5 ms; 2K/256 = 351 ms; 4K ≈ 0.5 s; 8K/256 ≈ a few s).
  This is no longer a "does the structure work / is it exact" experiment — those
  questions are answered. The remaining work is straight integration + a CI gate.
- **Start from:** the challenger's `kdTree`/`kdSearch` (encodes the fix: far-child
  prune **`diff*diff <= bestD`**, the `<=` that admits equidistant-tie points on the
  splitting plane — the precise bug report 03's quantlab kd hit with `<`). Recover
  from `04-…-data.txt` (or `/tmp/challenger/main.go`).
- **Do:** integrate as an exact matcher behind the `matcher` interface; balanced
  median-split build (axis = depth%3); row-parallel; per-worker `kdSearch` scratch;
  run-length collapse; band streaming for 8K.
- **Acceptance criteria:**
  - **Exactness: 0 non-nearest pixels** vs the linear-scan / `color.Palette.Index`
    oracle, every size/palette, including equidistant-tie and non-opaque-alpha cases.
    **CI-gate this bit-for-bit** — the `<=` prune must never regress to `<` (the whole
    correctness win depends on it).
  - **Speed:** reproduce report 04's shape — kd faster than parallel-linear at large
    P (2K/256: kd 351 ms vs linear 451 ms; scales far better at 8K), and **slower at
    small P** (8K/16: kd 1897 ms vs linear 1171 ms) so the selector routes linear at
    small P. Confirm **kd beats IM remap-only by ~30× at 4K** (Go kd ~0.5 s vs IM
    15 s) and is exact where IM is ~22% wrong.
  - Establish the **exact P crossover** (linear→kd, ≈ 64–256) and bake it into
    `selectMatcher`.
- **Benchmark:** matrix + a P-sweep (P ∈ {16,32,48,64,96,128,256}) at 2K and 8K to
  pin the crossover; include the `verify` exactness check and the IM-remap-isolated
  comparison.
- **Do NOT** port IM's climb-to-root tree (measured exact but 8K/64 = 146 s, unusable)
  and **do NOT** use IM's `cache[]` on the non-dithered path (adds 2.3–8.4% error,
  slower than kd). kd is the exact-and-fast path; these are dead ends, kept only as
  IM-parity references if ever needed.

**Phase 1 exit criteria:** the exact default is **two proven, measured-bit-exact
paths** — parallel-linear (B) for small P (~3×+ faster than serial at 2K/256) and
**exact kd** for large P (PROVEN 0% non-nearest, ~30× faster than IM at 4K, faster
than parallel-linear from P ≈ 64–256) — both bit-identical to the oracle; 6-bit Fast
path is tens-of-ms at 8K with documented, test-enforced accuracy bounds; benchmark
harness exists and includes the IM-remap-isolated comparison (which now reproduces
report 04's IM remap-only figures: 0.41 / 4.78 / 14.99 / 49.95 s at 512/2K/4K/8K).

---

## 3. Phase 2 — Put-it-into-practice experiments (the remaining high-upside bet)

**Goal:** the large-palette exact gap is **already closed for ship purposes by the
Phase-1 exact kd** (proven, measured). Phase 2 is now the *one* remaining high-upside
experiment: the **boundary-aware exact LUT**, which could deliver LUT-class `O(1)`
interior speed *with* exact output and beat even kd at huge N — but its make-or-break
number (boundary-pixel fraction) is still unmeasured. (The cell-list grid remains a
possible simpler substitute for the same large-P-exact slot if kd ever proves fiddly
in production — see the §3.1 cell-grid note — but kd is the proven incumbent and the default.)

### 3.1 Task: Prototype & validate the boundary-aware EXACT LUT (the standout)
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
    being exact — i.e. the interior fast path must dominate. **The bar to clear is
    the Phase-1 exact kd** (proven: 2K/256 = 351 ms, 8K/256 ≈ a few s); boundary-LUT
    is only worth shipping if it beats kd at large P / 8K while staying exact.
    Quantify the speed-vs-exactness-vs-grid-resolution tradeoff.
  - Characterize memory (LUT + boundary flag structure; should stay ≤ ~1–2 MiB).
- **Benchmark:** matrix + boundary-fraction table + head-to-head vs the Phase-1
  exact kd and parallel-linear at large P / 8K.
- **Decision rule:** accept if, in its target regime (large P, huge image), it gives
  a meaningful speedup **over the already-shipped exact kd** while being provably
  exact, and the boundary fallback fraction is small enough that throughput stays
  close to the plain LUT. If the boundary fraction is too high on realistic palettes,
  **keep exact kd (Phase 1) as the large-P-exact path** and the plain LUT only as the
  Fast path; the cell-grid (next note) is the simpler fallback if kd ever proves
  fiddly in production.
- **Note (cell-grid substitute):** a uniform **cell-list grid + exact ring
  expansion** (report 05 §1) is a viable *alternative* exact structure for the
  large-P slot — simpler, cache-friendlier, five-field-validated. kd is the proven
  incumbent (report 04), so only prototype the grid if kd integration is awkward;
  ship whichever is faster *and* verifiably exact, never both.

**Phase 2 exit criteria:** the boundary-aware exact LUT is either (a) accepted —
verified-exact (0 non-nearest), boundary-pixel fraction characterized across P and
grid resolution, and meaningfully faster than the Phase-1 exact kd at large P / 8K —
or (b) rejected with the measured boundary-fraction documented, leaving exact kd as
the shipped large-P-exact path. Either way the make-or-break boundary number is now
measured, not assumed.

---

## 4. Phase 3 — Integration, the selector, and proven enhancements

**Goal:** wire the matchers behind the strategy selector and add the
cross-disciplinary enhancements that *proved out* in Phases 1–2.

### 4.1 Task: Implement `selectMatcher(P, N, mode)` with measured thresholds
- Encode the dispatch table (§1.1) with the **measured** crossover constants. The
  exact set is now **parallel-linear (small P) + exact kd (large P)** — both proven
  in Phase 1 — plus the boundary-aware exact LUT *if* Phase 2 accepts it. Default
  mode `Exact`; `Fast` opt-in.
- **Acceptance:** for every cell of the regime matrix, the selector picks the
  method that the benchmark shows is fastest *at the required exactness*; a test
  asserts the routing. End-to-end, pixelize is never slower than today, is
  dramatically faster in the large-P / large-N cells, **and beats ImageMagick on
  both correctness (0% vs IM's ~22% non-nearest at P=64/256) and speed (~30× at 4K
  remap-only)** in the large-P regime.

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
  - Exact default (parallel-linear, small P): **≥ 3× faster than current serial** at
    2K/256; near-linear core scaling.
  - Large-P exact (**exact kd, Phase 1, PROVEN**): faster than parallel-linear from
    P ≈ 64–256 (rpt04: 2K/256 kd 351 ms vs linear 451 ms; scales far better at 8K)
    and **~30× faster than IM remap-only at 4K** (Go kd ~0.5 s vs IM 15 s), at **0%
    non-nearest**. Boundary-LUT (Phase 2) only if it beats kd while staying exact.
  - Fast LUT: **8K/256 ≤ ~150 ms** (6-bit) / ~55 ms (5-bit), palette-independent.
  - **Beat-IM gate:** in the large-P regime the exact path must be both more correct
    (0% vs IM's ~22%) and faster (remap-only) than ImageMagick — measured, not
    assumed.
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
| 1 | **Boundary-cell fraction** of the exact LUT may be large on dense/random palettes, killing its speed advantage. | Measure boundary-cell *and boundary-pixel* fraction across P and grid resolution, random vs tuned palettes (§3.1). | 2 |
| 2 | **Exact kd crossover** (P where kd beats parallel-linear) — report 04 measured it near P ≈ 64–256 (P=16 favors linear; P≥64 favors kd, scaling far better at 8K). RESOLVED in principle; just re-pin on an idle box. | P-sweep at 2K and 8K on an idle box; bake the constant into `selectMatcher`. | 1 |
| 3 | **Parallel scaling curve** is unmeasured cleanly (research sweep was contended, showed only ~1.3–1.5×; matrix implies ~3.5× on 4 cores). | Re-run GOMAXPROCS=1..N sweep on an **idle** box. | 1 |
| 4 | **IM-remap-isolated time** — **RESOLVED by report 04**: PPM full-minus-identity baseline gives IM remap-only 0.41 / 4.78 / 14.99 / 49.95 s at 512/2K/4K/8K, and IM is ~22% non-nearest at P=64/256. Re-confirm on an idle box and keep as the standing IM comparison. | Already measured (rpt04 §3, §4, §6); reproduce via `bench/compare.sh im`. | 1 |
| 5 | **Accuracy on tuned vs random palettes** — report 03 used random (pessimistic); real palettes do better, but by how much? | Re-measure LUT accuracy on representative tuned palettes (e.g. brand/LEGO ramps). | 1–2 |
| 6 | **Alpha / non-opaque inputs** — research assumed opaque RGB; stdlib metric is 4D RGBA. | Verify exactness with the alpha term included; ensure LUT/grid key handles alpha or documents opaque-only. | 1 |
| 7 | **kd vs cell-grid** for the large-P-exact slot — **largely RESOLVED:** report 04 proved exact kd bit-exact and fast, so kd is the incumbent. Cell-grid is only a contingency if kd integration proves awkward in production. | If kd is awkward in prod, head-to-head cell-grid vs kd at large P / 8K; ship only the winner. | 1–2 |
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

6. Integrate the **exact kd** (§2.4) — it is Phase 1 now (report 04 proved it
   bit-exact and ~30× faster than IM at 4K). Wire it as the large-P exact matcher,
   CI-gate the `<=`-prune bit-for-bit, and pin the linear→kd crossover (≈ P 64–256).

Everything beyond this (the boundary-aware exact LUT, the cell-grid alternative, the
selector, and the borrowed enhancements) is Phases 2–3, gated on the measurements
above. The boundary-aware exact LUT is the one remaining high-upside experiment; the
large-P exact win over ImageMagick is already secured by the Phase-1 exact kd.

---

## 8. Benchmark history and per-iteration protocol (standing requirement)

The project stores **benchmark history alongside commits**. This is a standing rule,
not a one-off: it makes every algorithm change auditable and makes the
"more-correct-AND-faster-than-IM" claim continuously verifiable.

### 8.1 Standing rule
- **Every commit that touches the matcher / quantization algorithms** (anything under
  the matcher path: parallel-linear, exact kd, LUTs, the strategy selector, the
  distance kernel, run-length/band code, or `bench/compare.sh` itself) MUST re-run the
  benchmark suite — `bench/compare.sh` (matrix + `im`) **plus the exactness
  verification** (`verify`, bit-for-bit vs brute force) — **before it is committed**.
- **Each result is saved to `bench/history/`** as a file stamped with a HEADER
  containing, at minimum:
  - commit SHA (short **and** full),
  - commit subject,
  - date/time and timezone,
  - machine + tool versions (CPU / `nproc`, `go version`, `convert --version` /
    ImageMagick build, OS).
  - Optionally, a short **"why / what changed"** narrative for that iteration (e.g.
    "switched kd far-child prune to `<=`; 0% non-nearest restored").
- **`bench/compare.sh` is being extended to emit this commit header automatically**
  (shell out to `git rev-parse HEAD` / `--short`, `git log -1 --format=%s`, `date`,
  `go version`, `convert -version`) and write the stamped result file into
  `bench/history/` so the history is produced as a side effect of running the suite,
  not by hand.
- **Naming:** suggested `bench/history/<UTC-date>-<shortsha>[-<tag>].txt` so files
  sort chronologically and map 1:1 to commits.

### 8.2 Per-iteration benchmark plan (the ONGOING change)
For each phase/task in this plan, run exactly the following before committing. This is
the concrete checklist that produces each `bench/history/` entry.

- **Benchmark cells to run (every iteration):** the full regime matrix —
  **palette sizes P ∈ {16, 64, 256}** × **image sizes {512², 2K, 4K, 8K}** — for the
  method(s) the commit touches, plus the unchanged exact path as a reference column.
  Record per cell: `build_ms`, `remap_ms` (best-of-N), `Mpix/s`, `heapMiB`.
  - Phase 1 / §2.1 parallel-linear: all 12 cells; confirm ≥3× vs serial at 2K/256.
  - Phase 1 / §2.4 exact kd: all 12 cells **plus** a P-sweep
    P ∈ {16,32,48,64,96,128,256} at 2K and 8K to pin the linear→kd crossover.
  - Phase 1 / §2.2 6-bit LUT (Fast): all 12 cells (timing is ~flat in P).
  - Phase 2 / §3.1 boundary-LUT: all 12 cells **plus** the boundary-pixel-fraction
    table across P and grid resolution (5/6/7-bit), random and tuned palettes.
  - **IM comparison (every iteration that touches an exact path):** the
    remap-isolated IM number (`convert in.ppm -dither None -remap palN.ppm out.ppm`
    minus identity-I/O baseline, best-of-3, PPM) at all four sizes, plus the
    end-to-end PNG-included number. Reference targets from report 04: IM remap-only
    0.41 / 4.78 / 14.99 / 49.95 s at 512/2K/4K/8K.
- **Exactness check (every iteration):** **bit-for-bit vs brute force** on the
  512²/2K/4K corpus for **every path claiming exact** (parallel-linear, exact kd, and
  the boundary-LUT). Report the **% non-nearest** (must be **0.0000%**) and re-test
  the equidistant-tie cases (the `<`→`<=` kd bug) and the `(128,0,0)`/`(127,0,0)`
  cross-top-bit stress case. Also record IM's measured % non-nearest (~0.29% at P=16,
  ~22% at P=64/256) as the competitive baseline we beat.
- **Regression gate (commit blocked if violated):**
  1. **No exact path may ever become non-exact** — any exact matcher reporting >0
     non-nearest fails the commit (this is the kd `<=`-prune guard above all).
  2. **No perf regression beyond noise** vs the previous `bench/history/` entry on a
     comparable machine: best-of-N `remap_ms` must not regress by more than the
     measured run-to-run noise band (state the band, e.g. ±10% on a contended box,
     tighter on idle) in any matrix cell the commit touches. A real regression must
     be justified in the "why/what changed" narrative or fixed before commit.
  3. **Beat-IM invariant** (large-P regime): the exact path stays 0% non-nearest
     (vs IM ~22%) and remap-only faster than IM; if a change erodes either, it is a
     regression.
  4. **Efficiency gate (wall-clock is the star).** When a commit adopts a heavier
     variant to go faster, record total CPU work (core-seconds) and peak RAM
     alongside `remap_ms`, and verify the proportional time gain **>=** the
     proportional resource increase vs the prior entry. A change that wins <X% time
     for >X% more CPU work or RAM (e.g. +20% CPU for -10% time) fails the gate unless
     the narrative justifies the exception. Parallelism (more utilization, ~flat
     total work) always passes. Among equally-fast variants, the leaner one wins.
- **Accuracy budget for approximate paths (test-enforced, recorded each iteration):**
  for the 6-bit LUT, **≤ ~8% of pixels differ** with **max color error ≤ ~35 RGB
  units** on a random palette (report 03: 4.6–7.2% / ≤34; report 04 verify: 1.9–8.4%
  across P); for the 5-bit LUT, **≤ ~15%** / **≤ 46**. Record the measured
  `pct_differ` and `max_err` per (P, size); exceeding the budget fails the commit.
  The approximate paths must never be selected as a silent default and must stay
  strictly better than IM's dithered `cache[]` (2.3–8.4%) where that comparison
  applies.
- **Honesty caveat to carry forward (preserve in every history entry):** a few large
  cells in report 04 (e.g. 4K/256, 8K/256 for kd / imcache / imtree) did **not**
  return stdout and are marked **(n/r)**; they interpolate and change no conclusion.
  Any cell an iteration cannot measure must likewise be marked **(n/r)** with the
  interpolation basis stated — never silently filled with a guessed number.
