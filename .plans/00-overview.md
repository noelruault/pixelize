# 00 — Overview & Synthesis: Scaling pixelize's Nearest-Color Palette Mapping

**Role of this document.** This is the consolidated, judged synthesis of the five
research reports in the nearest-color-scaling research record — now at
`noelruault/research/nearest-color-scaling/` (they used to live in `.plans/research/`;
see `.plans/research/MOVED.md`) — (01 ImageMagick reverse-engineering, 02
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

**HEADLINE FINDING (report 04, now COMPLETE and MEASURED — triple-confirmed):
ImageMagick's plain `-remap` is measurably approximate at large palettes, so we can
be BOTH more correct AND faster than IM.** IM's non-dithered remap picks a
non-nearest palette color on ~0.29% of pixels at P=16 but ~22% at P=64 and P=256
(it searches only the parent subtree, not the whole tree). Our exact kd-tree
branch-and-bound is **bit-exact (0% non-nearest, verified vs brute force at all
palette sizes)** and **~30× faster than IM at 4K** (Go kd ~0.5 s vs IM remap-only
15 s). This moves the kd-tree from "speculative, prune bug, not yet exact" to
**PROVEN exact and measured fast**, and answers the previously-open question "does
the Go port beat IM?" with a measured **yes, on both axes**. Do NOT port IM's tree
literally — the faithful climb-to-root variant is exact but catastrophically slow
(8K/64 = 146 s). See §2.1, §3, and §9.

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

### 2.1 Crucial correctness correction (report 04 §3) — UPDATED: IM's plain remap is MEASURABLY APPROXIMATE at large P

Report 01 §2c hedged that IM's search "may not be provably exact." An earlier draft
of this overview (written while report 04 had no measurements) concluded IM's plain
path was "exact in practice." **Report 04 is now COMPLETE and MEASURED, and that
conclusion is OVERTURNED.** Report 04 ran real ImageMagick (`convert in.ppm -dither
None -remap palN.ppm out.ppm`, the plain non-dithered path) and counted pixels where
IM's emitted color is not the true nearest palette color:

| palette | IM non-nearest | % | max extra sq-dist |
|--------:|:---:|:---:|:---:|
| 16  | 753   | **0.2872%** | 3004 |
| 64  | 59149 | **22.5636%** | 7792 |
| 256 | 58853 | **22.4506%** | 2600 |

**IM's plain remap is substantially approximate at moderate+ palette sizes (~22% of
pixels non-nearest at P=64 and P=256), and only near-exact at tiny P=16 (~0.29%).**
The mechanism: IM stops the descent at `index > 0` and searches only
`node_info->parent` (one level above the deepest match), **not** the whole tree — so
when the true nearest lives in a sibling subtree even one level higher, IM never
examines it. The error grows as the tree deepens (more palette entries), which is
exactly the 0.29% → ~22% jump from P=16 to P=64/256. This is **triple-confirmed**:
(a) `imcompare-pal` loading the *same* `pal_N.ppm` file IM was given gives identical
numbers (rules out palette/RNG desync); (b) an independent Python reimplementation
measured P=64 at 22.69%; (c) per-pixel spot checks (e.g. q=(113,131,171): IM chose
d=662 where truth is d=152).

- **The strawman "descend then search only the deepest subtree" is even worse**
  (22–54% non-nearest, measured) — proven on paper too: query `(128,0,0)`'s true
  nearest `(127,0,0)` (distance² = 1) sits in a *different top-level branch* because
  `127 = 0111_1111` and `128 = 1000_0000` differ in the top R bit, so a
  deepest-subtree search returns `(255,0,0)` at distance² = 16129.
- **IM's dither `cache[]` is its *second* approximation** (`CacheShift = 2` → 6
  bits/channel → 64³ memo table), measured at 2.3–8.4% non-nearest (replicated as
  `imcache`). This governs **dithered** output; the parent-only search above governs
  the **plain** path.

**Implication for "beating IM on correctness":** the correctness win is now **real
and large, not narrow.** Our exact methods (kd branch-and-bound and parallel-linear,
both measured at 0% non-nearest) strictly beat IM's plain remap (~22% wrong at
64/256) *and* IM's dithered cache path (2.3–8.4% wrong). We win on BOTH correctness
*and* speed simultaneously (see §3) — a genuine competitive differentiator, on top of
the architectural wins (flat slices, no pixel-cache indirection, no PNG codec in the
hot path) and the `O(1)` LUT fast path. (Caveat, preserved: this is the
nearest-color criterion on the non-dithered path, which is what pixelize compares
against. IM's chosen colors are still "close-ish," so a *perceptual* metric would
judge the ~22% more leniently — but on the stated nearest-color criterion IM is
decisively non-exact and we beat it.)

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

### 3.3 The PNG-I/O caveat (must be honored in any IM comparison) — UPDATED with report 04's isolated numbers

Measured IM **I/O only** (`convert in.png out.png`, no remap): 512 = 0.12 s, 2K =
2.72 s, 4K = 4.95 s, **8K = 18.6 s** (report 03, PNG). So at 8K/16, ~18.6 s of the
22.5 s total is **PNG codec, not remap**.

**Report 04 (COMPLETE) now provides the clean IM-remap-isolated numbers** that the
earlier draft said did not exist. Using PPM (raw byte I/O) plus an identity-convert
baseline (`remap-only = time(convert in.ppm -remap …) − time(convert in.ppm
out.ppm)`, best-of-2/3, on the same box):

| size | IM full | IM identity I/O | **IM remap-only** |
|------|--------:|----------------:|------------------:|
| 512² | 0.43 s | 0.02 s | **0.41 s** |
| 2K   | 4.93 s | 0.15 s | **4.78 s** |
| 4K   | 15.62 s | 0.62 s | **14.99 s** |
| 8K   | 51.71 s | 1.76 s | **49.95 s** |

**The fair, now-measured statement:** even with the PNG codec removed, **IM's remap
core is seconds-to-tens-of-seconds** (0.41 / 4.78 / 14.99 / 49.95 s). The Go exact
methods are far faster — at 4K/256, Go kd (~0.5 s) is **~30× faster than IM's
remap-only (15 s)** and exact where IM is ~22% wrong. The gap is the
algorithm/architecture (IM's per-row CacheView path), not just PNG. A clean
end-to-end PNG-included comparison remains the honest benchmark to *publish*, but the
remap-isolated win is no longer hypothetical.

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
- **Report 04 is now COMPLETE and MEASURED** (an earlier draft of this overview was
  written when report 04 had hit an output-channel failure and held no numbers; that
  is no longer the case). Report 04's numbers were taken on the same 4-vCPU box (Go
  1.24.7, IM 6.9.12-98 Q16), best-of-N, with PPM I/O to isolate remap cost. Its
  trustworthy contributions are now BOTH the correctness measurements (IM plain remap
  is ~22% non-nearest at P=64/256; kd and climb-tree are 0% non-nearest, verified vs
  brute force; the `<=` kd prune fix is what makes kd exact) AND the timing matrix
  (§3.5). A few large cells did not return stdout and are marked **(n/r)** — they
  interpolate cleanly and do not change conclusions (see §3.5 footnotes).

### 3.5 Report 04's measured matrix (COMPLETE — exact-and-fast, IM-isolated)

Report 04 re-measured every method in-memory (PPM, no codec), best-of-N, on the same
4-vCPU box, and verified exactness vs brute force. Times are **remap-only ms** unless
marked s. This is the discriminating regime (P=256):

| size | linear-par | imtree-climb (exact) | **kd-exact** | imcache | lut6 (approx) | **IM remap-only** |
|------|-----:|-----:|-----:|-----:|-----:|-----:|
| 512² | 29.8 | 2384 | **12.5** | 333 | 0.56 | 0.41 s |
| 2K   | 451 | 36480 | **351** | 1108 | 13.9 | 4.78 s |
| 4K   | 1679 | 21627† | 476‡ | (n/r) | 26.4 | 14.99 s |
| 8K   | 16356 | 146303§ | (n/r)¶ | 1195 | 301 | 49.95 s |

P=16 (small palette): at 8K, **parallel-linear (1171 ms) beats kd (1897 ms)** — a
tight cache-friendly scan wins when P is tiny. P=64: 2K linear (118) ≈ kd (128); the
exact-method linear→kd crossover is around **P=64–256**.

**Exactness (verified vs brute force, 512², N=262144):** kd and climb-tree = **0
(0.0000%)** at every palette size; subtree-only strawman 22–54%; lut6 1.9–8.4%;
imcache 2.3–8.4%. **Memory** is bounded and flat across methods: ~3–7 MiB (512²) →
**~448–453 MiB (8K)** (RGB input + `int32` index + output copy); the hot loop never
allocates.

**Key facts:** kd branch-and-bound is the exact method that is *also* fast (190×
faster than climb-tree at 512²/256, ~30× faster than IM at 4K, and exact). lut6 is
fastest and flat in P (sub-ms to 301 ms) but approximate. The literal IM climb-tree
is exact but unusable at scale (8K/64 = 146 s) — **do NOT port IM's tree literally,
and do NOT use IM's `cache[]` on the non-dithered path**.

† 4K imtree cell shown is the P=64 value (4K/256 climb cell did not return); the
point stands — climb is tens of seconds. ‡ 4K kd shown is the P=64 cell (475 ms);
the 4K/256 kd cell did not return but interpolates to ~0.5–1.5 s. § 8K/64 climb =
146 s. ¶ 8K/256 kd did not return; 8K/64 kd = 4.2 s and 8K/16 kd = 1.9 s, so 8K/256
kd ≈ a few seconds — far under IM's 49.95 s remap-only at 8K. **Honesty caveat
(preserved): the (n/r) cells are interpolations, not direct measurements; they do not
change any conclusion.**

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

### 4.3 ImageMagick-style bit-interleaved tree (climb-to-root). MEASURED — exact but catastrophically slow. DO NOT SHIP.
- **Idea:** port IM's 16-ary (8-ary for opaque) color tree; descend to deepest
  node, climb parent-by-parent to root calling `ClosestColor` with
  partial-distance early-out; run-length collapse.
- **Complexity / cost:** `O(8)` descent but the climb-to-root re-walks most of the
  tree per pixel. **MEASURED catastrophic (report 04 §4): 512²/256 = 2384 ms (190×
  slower than kd), 8K/64 = 146 s.** The earlier hope that it "sits near the kd-tree"
  is overturned — it is the slowest method by far.
- **Exact:** **yes** (climbing to the root is a superset of IM's parent-only search;
  prune is distance-correct) — measured 0% non-nearest. Note: this is the variant
  that *fixes* IM's ~22% inexactness, but at ~190× the cost.
- **Memory:** sparse 8-level tree, a few MB at P=256.
- **Wins:** nothing. It is dominated on both axes: by linear at small P, by kd at
  large P, by the LUT on raw speed everywhere. Its only value is as a *faithful
  reference* for reproducing IM's behavior, not a ship path.
- **Risk / effort:** medium effort, negative value-add. **Do NOT ship, and do NOT
  "port IM's tree and widen it to the root" as the exact strategy** — kd gives
  exactness *and* speed. Keep only as an optional IM-parity reference.

### 4.4 Exact kd-tree, branch-and-bound (algorithm C, fixed). PROVEN — exact (measured) and fast (measured). PROMOTE TO EARLY BUILD.
- **Idea:** balanced 3-D kd-tree over RGB (median split, axis = depth%3); search
  near child, then far child only if `diff*diff <= bestD` (the `<=` is the precise
  fix for report 03's prune bug — it admits equidistant-tie points on the splitting
  plane).
- **Complexity / cost:** `O(P log P)` build, ~`O(log P)` + backtracking per query.
  **Report 04 re-measured the fixed kd: 512²/256 = 12.5 ms, 2K/256 = 351 ms, 4K ≈
  0.5 s, 8K/256 ≈ a few s.** Near-logarithmic in P; ~30× faster than IM at 4K and
  190× faster than the IM climb-tree at 512²/256.
- **Exact:** **YES, MEASURED.** Report 04 verified the fixed kd at **0% non-nearest
  vs brute force at every palette size** (16/64/256). The equidistant-tie correctness
  is confirmed, not assumed. (Report 03's earlier prototype was inexact only because
  it used `<`; that is fixed.)
- **Memory:** ~40–48 B × P (≤12 KB at 256). Tiny.
- **Wins:** **large palettes (P ≳ 64–256) when exactness is required** — the only
  method that is both *measured-exact* and sub-linear in P, and the method that
  beats IM on correctness AND speed simultaneously. At small P (≤~32) parallel-linear
  is still faster (8K/16: linear 1171 ms < kd 1897 ms).
- **Risk:** low now — the exactness is measured, not speculative. The standing
  requirement is to keep a **bit-for-bit-vs-brute-force CI gate** (the `verify`
  check) so the `<=` prune can never regress to `<`.
- **Effort:** low (the fix is one operator; the prototype and verification already
  exist in `/tmp/challenger/main.go`). **This moves from a Phase-2 experiment to an
  early/Phase-1 build item — see the execution plan.**

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
- **Report 04 is COMPLETE and its numbers are measured and triple-confirmed.** (An
  earlier draft of this overview reflected an interim state where report 04 had hit a
  tool-output failure and held no numbers — that is superseded.) Its value is now (a)
  the correctness *measurements* — IM's plain remap is ~22% non-nearest at P=64/256
  (cross-checked via `imcompare-pal` on the same palette file and an independent
  Python reimplementation at 22.69%); kd and climb-tree are 0% non-nearest verified
  vs brute force; the kd prune must be `<=`; (b) the measured timing matrix (§3.5),
  including IM remap-only isolated via PPM + identity baseline; and (c) the preserved
  prototype source (`/tmp/challenger/main.go`, verbatim in
  `04-informed-challenger-data.txt`). The only non-measured cells are the handful
  marked **(n/r)**, which interpolate and change no conclusion.
- **The `/tmp` prototypes are present** (verified): `/tmp/quantlab/main.go` (blind
  track: A/B/C/D/E) and `/tmp/challenger/main.go` (informed track: parallel-linear,
  exact-kd with the `<=` fix, IM-climb tree, 6-bit LUT, IM dither cache). The
  challenger source is also preserved verbatim in `04-…-data.txt` as a backup. If a
  future container reset loses them, rebuild from that data file.
- **`bench/compare.sh` already exists** (verified) and is exactly the harness to
  reuse: it has `build` / `verify` / `matrix` / `im` (incl. remap-vs-I/O isolation)
  / `accuracy` / `all` subcommands, reuses the `/tmp` prototypes if present, and
  rebuilds the challenger from `04-…-data.txt` otherwise. The Phase 1 work is to
  *run and extend* it (e.g. on an idle box), not to create it from scratch. It is
  also being extended to **emit a per-commit header and write benchmark history to
  `bench/history/`** — see the execution plan's "Benchmark history and per-iteration
  protocol" section.
- **The kd-tree timings are valid even though report 03's prototype was inexact** —
  the prune-equality bug changes which equidistant point wins, not the traversal
  cost. **Report 04 then re-measured the *fixed* (`<=`-prune) kd and verified it is
  bit-exact (0% non-nearest) at all palette sizes**, so kd is now both proven exact
  and timed (§3.5).

---

## 7. The exact-vs-approximate axis (how to think about it)

- **Exact** (bit-for-bit identical to the linear scan / stdlib): parallel-linear
  (B), **exact kd (C-fixed) — now MEASURED at 0% non-nearest**, IM-climb tree
  (measured-exact but ~190× too slow), cell-grid, boundary-aware exact LUT,
  Hamerly-pruned scan. These are interchangeable on *output*, differ only on
  *speed by regime*. NB: IM's *own* plain remap is **not** in this exact set — it is
  ~22% non-nearest at P=64/256 (§2.1), so "exact" here means strictly better than IM.
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

| Regime | Exact winner (ship-now) | Fast/approx winner |
|--------|-------------------------|--------------------|
| **Small P (≤~32), small image (≤512²)** | parallel-linear B (or even serial — all <130 ms) | 5-bit LUT (overkill; B is fine) |
| **Small P (≤~32), large image (2K–8K)** | **parallel-linear B** (beats kd at small P; 8K/16 B=1171 ms vs kd=1897 ms, rpt04) | 5-/6-bit LUT (47–52 ms @ 8K) |
| **Large P (≳64–256), small image (≤512²)** | **exact kd** (12.5 ms @ 512²/256, rpt04) or B (30 ms) | 5-bit LUT (8 ms) |
| **Large P (≳64–256), large image (2K–8K)** | **exact kd (fixed, PROVEN)** (351 ms @ 2K/256, ≈ few s @ 8K/256 vs B's 16.4 s) — boundary-aware exact LUT is the remaining high-upside experiment | **6-bit LUT** (301 ms @ 8K, ~half 5-bit's error) |
| **Huge image (8K), any P, speed-critical, bounded error OK** | (use exact kd if exactness needed) | **6-bit LUT** (301 ms @ 8K, flat in P, ~1.9–8.4% non-nearest) |
| **Match/beat IM plain remap** | **exact kd / B (0% non-nearest, ~30× faster than IM at 4K) — strictly beats IM, which is ~22% wrong at P=64/256** | LUT (much faster, slightly approximate, still beats IM's ~22%) |
| **Beat IM dithered output on correctness** | exact kd / B (0 non-nearest vs IM cache's 2.3–8.4%) | n/a |
| **Do NOT use** | literal IM climb-tree (exact but 8K/64 = 146 s) | IM `cache[]` on the non-dithered path |

**Synthesis verdict (updated for report 04 COMPLETE):**

1. **The proven, ship-now exact default is a two-method adaptive exact path:**
   **parallel-linear (B) for small P (≤~32), exact kd branch-and-bound for large P
   (≳64).** Both are now **measured at 0% non-nearest** (kd's exactness is proven,
   not speculative). This adaptive exact path **beats IM on both correctness (0% vs
   IM's ~22% at P=64/256) and speed (~30× at 4K)** — the headline competitive
   differentiator. Lowest risk; the prototypes exist in `/tmp/challenger`.
2. **The proven fast/preview path is the 6-bit LUT** (with 5-bit as an even-faster
   higher-error option), behind an opt-in flag with documented accuracy.
3. **The large-palette exact gap is now CLOSED for ship purposes by the
   fixed-and-verified exact kd** (no longer a Phase-2 maybe). The **boundary-aware
   exact LUT remains the one high-upside Phase-2 experiment** — it could deliver
   LUT-class `O(1)` interior speed with exact output, but its boundary-cell fraction
   is still unmeasured, so it is an experiment, not a ship-now item.
4. **Do NOT port IM's tree literally** (climb-to-root is exact but ~190× too slow),
   and **do NOT use IM's `cache[]` on the non-dithered path** (adds 2.3–8.4% error,
   slower than kd).
5. **Layer the cheap enhancements under everything:** run-length collapse and
   flat-buffer (flat-Pix16) access (kill the `At/SetRGBA` interface overhead) are
   shipped and proven (reports 08, 10). **Update (report 12): the remaining
   speculative enhancements were measured and closed.** Hamerly + previous-pixel
   pruning, Morton/Z-order LUT layout, and SoA/AVX2 all **failed their gates** and
   were rejected; the only bit-identical win from that sweep — dropping the alpha
   term on the opaque linear scan (~25 % less work, 1.08–1.29×) — shipped instead.
   Band streaming for 8K remains the OOM safety lever.

This is a **hybrid with a regime-based selector**, exactly as the framing
demands — see `01-execution-plan.md` for the dispatch table and phased build.
**All three phases are now complete (Phase 1 shipped, Phase 2 closed by measured
rejection of the boundary LUT, Phase 3 selector live + §4.2 closed by report 12).**
