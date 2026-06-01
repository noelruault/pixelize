# 07 — The Puzzle: Stacking Base Matchers with Composable Enhancements

**Goal.** Stop guessing from the parts. Measure how the three proven base matchers
(linear / kd-tree / 6-bit LUT) combine with the four composable enhancements
(PAR, RL, HAM, BAND) — individually and **stacked** — so a strategy selector can be
handed the *winning combination per regime cell*, not just the winning core.

All numbers are **real, measured** on this box. Harness, raw output and verbatim
source: `07-puzzle-enhancement-combinations-data.txt`. Workspace `/tmp/puzzle-mix/`.
The pixelize source tree was **not** modified.

---

## 0. Setup

**Box:** 4 vCPU, Go 1.25.0 linux/amd64, ImageMagick 6.9.12-98. No GNU `/usr/bin/time`;
peak RSS read from `/proc/self/status` `VmHWM`. Timing is **remap-only** (decode/encode
outside the timed region for full-frame paths; the BAND path's timing *includes* the
streamed write — flagged below). best-of-N: N=7 (512 / scaling), 5 (2K/4K), 3 (8K).

**Puzzle pieces.**
- **Base matchers** (exclusive cores): `L` linear scan, `KD` exact kd-tree (`<=` prune),
  `LUT6` 6-bit inverse colormap (approximate).
- **Enhancements** (stackable): `FLAT` flat `src.Pix` access (baseline infra, always on);
  `PAR` GOMAXPROCS row-parallel pool; `RL` run-length collapse (one lookup per run);
  `HAM` Hamerly bound + previous-pixel coherence (seed the upper bound with the previous
  pixel's winner and its squared nearest-other distance `no2`; skip the full scan when
  `4·d ≤ no2[prev]`; **L only**); `BAND` row-band streaming to bound memory.

**Two inputs with deliberately opposite run structure** (this is what makes RL/HAM
measurable):

| input | how built | mean run | RL collapse |
|---|---|---|---|
| **starry** (photographic) | `convert starry.jpg -resize WxH!` | 1.00–1.13 | 0.4–11.9 % |
| **flat** (pixel-art) | `-resize 48x48 -posterize 4 -scale WxH!` | 15–190 | 93–99.5 % |

So on *photographic* input adjacent pixels almost never repeat (RL has ~nothing to
collapse); on *flat* input 93–99.5 % of pixels are inside a run (RL skips almost all work).

**Exactness (the non-negotiable).** Every `L*` and `KD*` combination is bit-exact —
**0 non-nearest pixels** vs the brute oracle at every P and size tested (verified
512×512 P∈{16,64,256} and a 4.2 Mpix scale-check):

```
L+PAR+RL    nonNearest=0 (0.0000%)   differVsTruth=0
KD+PAR+RL   nonNearest=0 (0.0000%)   differVsTruth=4183 (0.10%)  maxExtraD=0
L+PAR+RL+HAM nonNearest=0 (0.0000%)  differVsTruth=2228 (0.05%)  maxExtraD=0
LUT6+PAR    nonNearest=202958 (4.84%) maxExtraD=1043   <- approximate, expected
```
`differVsTruth>0` with **`maxExtraD=0`** means KD/HAM pick a *different but exactly
equidistant* palette entry on ties — still an optimal nearest color. LUT6 is the only
approximate path: **1.9 % / 4.8 % / 8.2 %** of pixels differ at P=16/64/256 on photographic
content (error grows with P as cells hold more candidates), but only **0.10 %** on flat
content (few distinct colors land cleanly in 6-bit cells).

---

## 1. Per-regime winning combination

Two answers per cell because **exactness is a hard constraint for some callers**:
the **exact winner** (only `L*`/`KD*` eligible) and the **overall winner** (LUT6 allowed).
Throughput in Mpix/s, best-of-N. "margin" = winner ÷ next-best *in the same class*.

### Photographic (starry)

| size | P | **exact winner** | Mpix/s | margin vs next exact | overall winner | Mpix/s |
|---|---|---|---|---|---|---|
| 512  | 16  | **L+PAR+RL**  | 103 | 1.8× over KD+PAR+RL (58) | LUT6+PAR | 636 |
| 512  | 64  | **KD+PAR+RL** | 30  | 1.03× over L+PAR+RL (29) | LUT6+PAR | 577 |
| 512  | 256 | **KD+PAR+RL** | 21  | 2.8× over L+PAR+RL (7.6) | LUT6+PAR | 578 |
| 2K   | 16  | **L+PAR(+RL)** | 110 | 1.7× over KD+PAR (66)   | LUT6+PAR | 959–1018 |
| 2K   | 64  | **KD+PAR+RL** | ~20 | ~1.1× over L+PAR        | LUT6+PAR | 724 |
| 2K   | 256 | **KD+PAR+RL** | 25  | 3.3× over L+PAR (7.6)   | LUT6+PAR | 1009 |
| 4K   | 16  | **L+PAR+RL**  | 111 | 1.65× over KD+PAR+RL (67)| LUT6+PAR | 962 |
| 4K   | 64  | **KD+PAR+RL** | 36  | 1.12× over L+PAR+RL (32) | LUT6+PAR | 982 |
| 4K   | 256 | **KD+PAR+RL** | 28  | 3.3× over L+PAR+RL (8.4) | LUT6+PAR | 1032 |
| 8K   | 16  | **L+PAR+RL**  | 125 | 1.6× over KD+PAR+RL (79) | LUT6+PAR | 1032 |
| 8K   | 64  | **KD+PAR+RL** | 42  | 1.1× over L+PAR+RL (38)  | LUT6+PAR | 1037 |
| 8K   | 256 | **KD+PAR+RL** | 34  | 3.4× over L+PAR+RL (9.9) | LUT6+PAR | 1046 |

**Photographic pattern:** linear wins **only at P=16** (palette small enough that the flat
16-iteration scan beats kd's branch overhead); kd takes over at **P≥64** and its margin
over linear *widens* with P (3.3–3.4× at P=256). RL is essentially free here (runs ≈ 1)
so `+RL` neither helps nor hurts beyond noise. LUT6+PAR is ~1 Gpix/s flat across all P
(table lookup is P-independent) but it is **approximate**.

### Flat / pixel-art

| size | P | **exact winner** | Mpix/s | margin vs next exact | overall winner | Mpix/s |
|---|---|---|---|---|---|---|
| 512  | 16  | **L+PAR+RL**  | 452 | 1.06× over KD+PAR+RL (428) | LUT6+PAR | 602 |
| 512  | 64  | **KD+PAR+RL** | 324 | 1.25× over L+PAR+RL (259)  | LUT6+PAR | 575 |
| 512  | 256 | **KD+PAR+RL** | 288 | 2.9× over L+PAR+RL (98)    | LUT6+PAR | 639 |
| 2K   | 16  | **L+PAR+RL**  | 513 | 1.12× over KD+PAR+RL (459) | LUT6+PAR+RL | 525 |
| 2K   | 64  | **KD+PAR+RL** | 544 | 1.31× over L+PAR+RL (414)  | (KD+PAR+RL) | 544 |
| 2K   | 256 | **KD+PAR+RL** | 392 | 2.3× over L+PAR+RL (169)   | (KD+PAR+RL) | 392 |
| 4K   | 16  | **KD+PAR+RL** | 1178| 1.2× over L+PAR+RL (986)   | (KD+PAR+RL) | 1178 |
| 4K   | 64  | **KD+PAR+RL** | 1077| 1.23× over L+PAR+RL (873)  | (KD+PAR+RL) | 1077 |
| 4K   | 256 | **KD+PAR+RL** | 1096| 1.85× over L+PAR+RL (593)  | (KD+PAR+RL) | 1096 |
| 8K   | 16  | **KD+PAR+RL** | 1194| 1.2× over L+PAR+RL (996)   | (KD+PAR+RL) | 1194 |
| 8K   | 64  | **KD+PAR+RL** | 1202| 1.35× over L+PAR+RL (888)  | (KD+PAR+RL) | 1202 |
| 8K   | 256 | **KD+PAR+RL** | 1182| 1.57× over L+PAR+RL (751)  | (KD+PAR+RL) | 1182 |

**Flat pattern — the headline result:** with RL collapsing 93–99.5 % of pixels, the matcher
runs on only ~0.5–7 % of the image. The remaining cost is **memory-bandwidth bound**, and
there **`KD+PAR+RL` beats even the approximate LUT6** at 4K/8K (1.1–1.2 Gpix/s, exact) —
because LUT6 still does a full table lookup per pixel (RL only saves its small per-pixel
work) while RL lets kd skip the lookup entirely on repeats. **On flat content the exact
kd stack is both the fastest and the most accurate option.**

---

## 2. Enhancement-by-enhancement contribution

### RL (run-length) — the single most decisive enhancement, but only on flat content
- **Flat input:** transformative. `L` 7.2 → `L+RL` 162 Mpix/s at 2K/P=64 (**22×**);
  `L+PAR` 55 → `L+PAR+RL` 513 at 2K/P=16 (**9×** on top of PAR). It converts an
  O(N·cost) scan into O(runs·cost).
- **Photographic input:** **neutral to mildly harmful.** Runs ≈ 1.0–1.1, so RL almost
  never fires and its per-pixel equality test is pure overhead. Measured at 2K/P=256:
  `L+PAR` 554 ms vs `L+PAR+RL` 1056 ms — here RL nearly *doubled* the time (the extra
  branch defeated some auto-vectorization of the inner scan). At smaller P the penalty is
  within noise. **RL must be gated on detected run structure**, not always-on.

### PAR (row-parallel pool) — scales, with honest variance
- Always a win for compute-bound matchers (L, KD); near-useless for the already
  bandwidth-bound LUT6 at small sizes. See the clean curve in §3.

### HAM (Hamerly + previous-pixel coherence) — **did not pay off here; net negative**
- On **photographic** input it is the *worst* exact option: `L` 582 → `L+HAM` 734 ms at
  2K/P=64. Runs ≈ 1, so the previous-pixel seed is almost never within the Hamerly radius;
  the seed-distance computation and the `4·d ≤ no2` test are paid on every pixel for almost
  no skips, and the fall-through still does the full scan.
- On **flat** input HAM's coherence is **redundant with RL** — RL already collapses the
  identical runs for free, and on a run *boundary* the colors differ so the Hamerly bound
  rarely holds. `L+PAR+RL` 513 vs `L+PAR+RL+HAM` 396 Mpix/s at 2K/P=16: stacking HAM on
  top of RL **costs ~23 %**.
- HAM did **not** extend the P-range where linear beats kd. The crossover stayed at
  P≈16→64 regardless. *Honest verdict: on this access pattern (raster order, real images)
  RL captures the cheap coherence wins and HAM adds overhead. HAM would matter for a
  matcher with no LUT/kd structure and high-coherence non-repeating gradients — not seen
  in this corpus.* I kept it exact and measured it rather than dropping it.

### BAND (row-band streaming) — a **memory** lever, mild speed cost; see §4.

### FLAT (flat-buffer infra) — the floor everything stands on
- Not toggled off (the original `At/SetRGBA` path was already abandoned in report 04).
  Every number here is on direct `src.Pix` indexing; the row-matcher interface keeps
  per-worker state (kd search struct, HAM previous-pixel) private with no per-pixel
  allocation (heap delta ≈ 0.1 MiB across all runs).

---

## 3. The clean PAR scaling curve (1 / 2 / 4 cores)

Representative cell **starry 2K, P=256**, best-of-7, `GOMAXPROCS` swept:

| combo | 1 core | 2 cores (speedup) | 4 cores (speedup) |
|---|---|---|---|
| **KD+PAR+RL** | 667 ms | 338 ms (**1.97×**) | 170 ms (**3.92×**) |
| **L+PAR+RL**  | 2094 ms| 1062 ms (**1.97×**)| 539 ms (**3.89×**) |
| **L+PAR**     | 2183 ms| 1096 ms (**1.99×**)| 552 ms (**3.95×**) |
| **KD+PAR**    | 665 ms | 333 ms (**2.00×**) | 168 ms (**3.96×**) |
| **LUT6+PAR**  | 266 Mpix/s | 529 Mpix/s (**1.99×**) | 1009 Mpix/s (**3.79×**) |

**Compute-bound matchers (L, KD) scale near-linearly to all 4 cores: ~3.9×.** This is a
cleaner result than the research box's earlier ~1.3–1.5×, and it is honestly *variable*:
an **interleaved** sweep (all combos in one process, cache/scheduler contention) on the
same cell earlier measured only **2.3×** at 4 cores for KD+PAR+RL (350 ms → 292 ms from 2→4).
best-of-N on an isolated process recovers the near-linear ~3.9×. **Report both:** the
realistic ceiling is ~3.9× on 4 cores when the box is quiet; expect ~2–2.5× under
contention. LUT6 is bandwidth-bound and scales worse (3.8×) and noisier.

---

## 4. Memory: BAND bounds the 8K peak

8K (7680×4320 = 33.2 Mpix). True peak RSS via `VmHWM`:

| path | peak RSS | working set | remap_ms (starry, KD, P=256) |
|---|---|---|---|
| **full-frame** KD+PAR+RL | **329 MiB** | whole src 97 + idx 130 + out 97 MiB | ~987 ms (compute only) |
| **BAND** band=256 | 38 MiB | 19.2 MiB bands | ~2.4 s incl. write |
| **BAND** band=128 | **28 MiB** | 9.6 MiB bands | ~2.4 s incl. write |
| **BAND** band=64  | **17 MiB** | 4.8 MiB bands | ~2.4 s incl. write |

**BAND keeps the 8K peak bounded to 17–38 MiB — a 9–19× reduction vs the 329 MiB
full-frame footprint**, independent of image height (only `band × W` rows are resident,
plus the streamed output buffer). The full-frame index array alone is 130 MiB; BAND never
materializes it.

**Honest cost:** BAND's `remap_ms` *includes the streamed PPM write* (full-frame timing
excludes encode), so its 2.4 s on photographic 8K is not comparable to the 987 ms
compute-only figure — most of the gap is I/O, not the band machinery. On **flat** 8K, BAND
KD+PAR+RL streams in **280 ms** (RL collapses the compute, so write dominates and the band
overhead is negligible). Net: **BAND is the memory-safety lever for 8K; use it when the
peak-RSS budget matters, accept the streamed-write time.**

---

## 5. Consolidated recommendation — input to the strategy selector

The selector needs **two facts at dispatch: P (palette size) and a cheap run-structure
probe** (sample a few rows, estimate mean run length). Then:

```
EXACT REQUIRED (the safe default for a faithful quantizer):
  run-structure FLAT (mean run ≳ 8, e.g. pixel-art / posterized / upscaled):
      → KD + PAR + RL          (exact, AND fastest overall on flat — beats LUT6,
                                1.1–1.2 Gpix/s at 4K/8K; at P=16 use L+PAR+RL, ~equal)
  run-structure PHOTOGRAPHIC (mean run ≈ 1, gradients/noise):
      P = 16   → L  + PAR (+RL off)   (small palette: flat scan beats kd branch)
      P ≥ 64   → KD + PAR (+RL off)   (kd pruning wins; margin grows with P, 3.3× at 256)

APPROX ACCEPTABLE (preview / speed-over-fidelity):
  any regime → LUT6 + PAR           (~1 Gpix/s, P-independent; 1.9–8.2% pixels off-nearest
                                      on photographic, 0.1% on flat)

MEMORY-CONSTRAINED (8K and the RSS budget is tight):
  wrap the chosen exact stack in BAND (band=64–128 rows) → 17–28 MiB peak,
  accept the streamed-write time.
```

**Rules the data justifies:**
1. **Always PAR** for L/KD (near-linear to ~3.9× on 4 cores; never harmful).
2. **RL only when a run probe says runs are long.** It is a 9–22× win on flat content and
   a ~2× *penalty* on photographic content at high P. Gate it.
3. **kd is the exact core for P≥64**; linear only for the tiny-palette P=16 corner.
4. **HAM: drop it** for this corpus/access-pattern — it never won a cell and stacking it on
   RL cost ~23 %. Keep the code, leave it off by default.
5. **BAND for 8K memory safety**, not for speed.

---

## 6. Honesty caveats

- **Container noise.** 4 shared vCPUs. Parallel speedup is the noisiest axis: isolated
  best-of-7 gives ~3.9× on 4 cores, an interleaved/contended run gave ~2.3×. I report both
  and treat ~3.9× as the quiet-box ceiling, ~2–2.5× as the realistic contended figure.
- **BAND timing includes the output write**, full-frame timing does not — so BAND vs
  full-frame *speed* is not apples-to-apples (it's a memory result, not a speed result).
  I flagged this everywhere it appears.
- **(n/r) cells / what I skipped.** I did **not** run the full 14-combo sweep at every one
  of the 12 (P×size) cells × 2 inputs — that is ~336 cells. I ran the full 14-combo sweep
  at the 2K workhorse (both inputs, all P) to characterize every enhancement, then swept
  only the 4 *candidate winners* across 512/4K/8K. Combinations I judged dominated
  (`KD+HAM` — HAM is L-only by construction; `LUT6+HAM` — meaningless; serial-no-PAR at 4K/8K)
  were not measured at every cell. The `L+HAM`/`L+PAR+RL+HAM` rows at 2K are enough to
  retire HAM.
- **LUT6 accuracy is content- and P-dependent**, not size-dependent; I report % differ and
  maxExtraD at 512 (all P) and a 2K scale-check, not at every cell.
- **Tie-breaking.** KD/HAM report nonzero `differVsTruth` with `maxExtraD=0` — equidistant
  ties resolved differently from the linear oracle. Bit-identical *images* to the oracle
  require a canonical tie rule (e.g. lowest index); the chosen colors are always optimal
  nearest. I verified `nonNearest=0`, which is the property that matters.
- **Palette RNG** is the report-04 xorshift (`0x9E3779…`), not report-03's `math/rand`;
  accuracy %s are magnitude-comparable across reports, timing is palette-content-independent
  for fixed P.

*Raw output + verbatim harness source: `07-puzzle-enhancement-combinations-data.txt`.*
