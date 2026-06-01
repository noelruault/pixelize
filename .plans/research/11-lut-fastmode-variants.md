# 11 — LUT Fast-mode variants: the approximate nearest-color path

**Question.** What is the best APPROXIMATE "Fast mode" nearest-color path — a
precomputed lookup table (LUT) keyed on quantized RGB — measured on the
wall-clock star, and what accuracy does it cost? This is an **opt-in** approximate
path; the exact default (`flat-Pix16` linear, kd-tree for large P) must remain
exact and is the bar.

**Headline.** A LUT query is astonishingly cheap and **palette-size-independent**
(~0.3 ms at 512², ~7 ms at 2048², ~30–58 ms at 8K — one table lookup per pixel,
no search). But **build time dominates Fast mode**, because Fast mode is one-shot
(build + query for a single image). Build is `O(2^(3·bits) · P)` and explodes with
depth: a 7-bit table at P=256 costs **0.69–2.2 s just to build**. Once you weigh
build+query against the exact bars:

- **5-bit LUT is the only depth that wins on total wall-clock for one-shot use**,
  and only in a clear regime: **large images (≥2048²) where the table build is
  amortized over millions of pixels.** At 8K it beats both exact bars at every P
  (e.g. P=256: **78 ms** vs exact-kd **2277 ms**, exact-linear **16096 ms**).
- **6-bit and 7-bit LUTs LOSE on one-shot total time** almost everywhere their
  build cost is too high to amortize, and 7-bit is additionally **slower to query**
  than 5/6-bit (cache thrash on the 8 MiB table — the report-07 bandwidth warning,
  confirmed).
- **At small images (512²) no LUT depth reliably beats exact-kd**: kd is 5.5–16.8 ms,
  and the only LUT that undercuts it (5-bit) saves single-digit ms while costing
  3.7–11.9% accuracy. Not worth it.
- **Accuracy cost is steep and grows with P:** 5-bit mismatches **3.7% (P16) /
  7.6% (P64) / 10.6% (P256)** of pixels on the painting; 6-bit roughly halves that
  (1.9 / 4.3 / 5.8%); 7-bit halves again (1.1 / 2.3 / 3.3%).

So the depth that wins on time (5-bit) is the **least accurate**, and the depth
that is accurate (7-bit) **never wins on one-shot time.** That tension is the whole
story. **Verdict: ship a Fast mode only as a narrow, opt-in option for large-image
+ high-P (the exact-linear pain point), at 6-bit; do not make it general.** Details
and the honest "or don't ship it" case in §6.

All numbers are real, measured on this box (4 logical CPUs, Go 1.24.7, container —
noisy). Work was in `/tmp/eval-lut/`; pixelize source not modified. Raw output +
full standalone Go source: `11-lut-fastmode-variants-data.txt`.

---

## 1. The variants

| depth | cells/chan | entries | table RAM (int32) | role |
|---|---|---|---|---|
| **5-bit** | 32 | 32 768 | **0.12 MiB** | smallest table, coarsest |
| **6-bit** | 64 | 262 144 | **1.00 MiB** | report-07's LUT6 |
| **7-bit** | 128 | 2 097 152 | **8.00 MiB** | finest; bandwidth-suspect |

Build: per cell, run the **exact `indexRaw`** on the cell-CENTER color
(`base + half-cell`); store the index as `int32`. Query: `(r>>shift,g>>shift,
b>>shift)` flat index, row-band parallel (exactly like `applyNearest`). Build
measured **serial** and **parallel** (across the outer R cell index, GOMAXPROCS
bands). Exact bars: `flat-Pix16` linear and the bit-identical kd-tree, both
row-band parallel, mirrored from the shipped source.

---

## 2. ACCURACY — the cost of Fast mode (this is the headline cost)

`mismatch_pct` counts only pixels where the LUT color is **genuinely farther from
the original pixel** than the exact color (equidistant ties excluded — a different
but equally-near color is not an error). `meanErr`/`maxErr` = RGB-Euclidean
distance between the LUT color and the exact color, over the genuinely-wrong pixels.

### Painting (starry) — content-dependent, near-identical at 512² and 2048²

| P | bits | mismatch % | meanErr | maxErr |
|---:|---:|---:|---:|---:|
| 16 | 5 | **3.71** | 102 | 182 |
| 16 | 6 | **1.86** | 103 | 182 |
| 16 | 7 | **1.13** | 104 | 182 |
| 64 | 5 | **7.60** | 60 | 153 |
| 64 | 6 | **4.27** | 61 | 153 |
| 64 | 7 | **2.33** | 61 | 153 |
| 256 | 5 | **10.55** | 44 | 84 |
| 256 | 6 | **5.77** | 42 | 84 |
| 256 | 7 | **3.34** | 42 | 83 |

### Noise (defeats spatial coherence — worst case for any matcher)

| P | bits | mismatch % | meanErr | maxErr |
|---:|---:|---:|---:|---:|
| 16 | 5 / 6 / 7 | 3.73 / 1.90 / 1.11 | ~105 | ~211 |
| 64 | 5 / 6 / 7 | 6.90 / 3.58 / 2.04 | ~69 | ~160 |
| 256 | 5 / 6 / 7 | 11.86 / 6.26 / 3.46 | ~42 | ~115 |

**Findings.**
- **Mismatch grows with P** (more palette candidates fall inside each coarse cell,
  so the cell-center representative is more often a tie-loser) and **halves with
  each extra bit** (each bit quarters cell volume). My 6-bit P256 figure (5.8%
  painting / 6.3% noise) is in line with report 07's ~8.2% — slightly lower here
  because I exclude equidistant ties (the stricter, more honest definition of
  "wrong"). The prompt's cited "~8% for a coarse table" is real for 6-bit/P256.
- **maxErr is large** even when mismatch is small: a wrong pixel can land up to
  ~180 RGB-units off at P16 (sparse palette, big Voronoi cells). Mean error is
  more moderate (40–105). Fast mode is "mostly right, occasionally visibly wrong,"
  not "uniformly slightly off."
- Accuracy is **content- and P-dependent, essentially size-independent** (512² and
  2048² agree to ~0.1%), so the per-depth %s above hold at 8K too.

---

## 3. SPEED — build_ms / query_ms / total_ms vs the exact bars (best-of-N)

Query is **P-independent** (one lookup) — the small P-to-P wobble below is
container noise. Build is `O(2^(3·bits)·P)`. LUT rows use the **parallel** build
(the better number); serial build is in the data file (3–4× slower, e.g. 7-bit
P256 @512²: 2194 ms serial vs 687 ms parallel). `total = build + query` — the
relevant quantity for one-shot Fast mode.

### 512² (262 k px)  — total_ms (build+query)

| P | linear | kd | LUT5 | LUT6 | LUT7 |
|---:|---:|---:|---:|---:|---:|
| 16 | 5.73 | **5.48** | 1.23 | 5.27 | 37.34 |
| 64 | 17.48 | **8.32** | **2.64** | 19.85 | 210.23 |
| 256 | 111.45 | **16.76** | **13.82** | 103.44 | 686.64 |

(LUT query alone ~0.3–0.5 ms; LUT total is essentially its build cost.)

### 2048² (4.19 M px) — total_ms

| P | linear | kd | LUT5 | LUT6 | LUT7 |
|---:|---:|---:|---:|---:|---:|
| 16 | 116.16 | 92.23 | **7.99** | 15.71 | 59.97 |
| 64 | 432.78 | 158.42 | **11.21** | 34.39 | 201.29 |
| 256 | 1454.24 | 232.04 | **20.44** | 117.82 | 756.36 |

(LUT query alone ~6.6–8.0 ms.)

### 8K (33.2 M px) — total_ms

| P | linear | kd | LUT5 | LUT6 | LUT7 |
|---:|---:|---:|---:|---:|---:|
| 16 | 834.21 | 692.19 | **43.65** | 51.64 | 103.49 |
| 64 | 3124.70 | 910.89 | **30.85** | 61.13 | 342.41 |
| 256 | 16095.58 | 2277.04 | **78.05** | 191.22 | 1127.27 |

(LUT query alone ~28–58 ms; the 7-bit query is the slowest — see §4.)

**Winner per cell (lowest total, lower is better) is bolded.** LUT5 wins every
2048² and 8K cell outright; at 512² kd wins P16/P64 and LUT5 only sneaks the P256
cell (13.8 vs kd 16.8 — a few ms, inside noise).

---

## 4. The 7-bit table is memory-bandwidth-bound (report-07 warning confirmed)

Query time per pixel, by depth, at 8K (table fully resident, P-independent):

| depth | table RAM | 8K query_ms (P16 / P64 / P256) |
|---|---|---|
| 5-bit | 0.12 MiB | 41.9 / 28.5 / 57.0 |
| 6-bit | 1.00 MiB | 44.0 / 42.6 / 57.9 |
| **7-bit** | **8.00 MiB** | **48.1 / 57.2 / 58.3** |

The **bigger table is the slower one to query** despite fewer collisions: the
0.12/1.0 MiB tables sit in L2/L3, the 8 MiB 7-bit table thrashes L2 and forces
LLC/DRAM traffic on the random-access lookup pattern. So 7-bit pays **twice**: a
huge build *and* a slower query, to buy accuracy you could instead get exactly,
for free on the time axis, from the kd-tree. **7-bit is dominated — drop it.**

---

## 5. RAM and the 8K-OOM check (rubric exception #1)

| item | size |
|---|---|
| 5-bit table | 0.12 MiB |
| 6-bit table | 1.00 MiB |
| 7-bit table | 8.00 MiB |
| 8K src Pix (RGBA) | 126.6 MiB |
| 8K out index buffer (int32) | 126.6 MiB |
| **8K end-to-end peak (7-bit, P256), VmHWM** | **360 MiB** |

The LUT table is **negligible** at every depth (≤8 MiB). Peak 8K RSS (360 MiB) is
dominated by the source image and the output index buffer, **identical to the
exact paths' footprint**, and is comfortably below the report-07 full-frame exact
peak (329 MiB compute-only there; same order). **No OOM risk at 8K for any LUT
depth.** If 8K RSS ever matters, the same BAND row-streaming lever from report 07
applies unchanged (the LUT is built once and shared read-only across bands).
Parallelism (build and query) passes the rubric gate automatically — it spends
cores already present, total work roughly flat.

---

## 6. Verdict (apply the rubric)

### (a) Best speed/accuracy point: there is no single sweet spot — it's a fork

- **5-bit** is the time winner (smallest build, cache-resident query) but the
  **least accurate** (3.7 / 7.6 / 10.6% mismatch).
- **7-bit** is the accurate one (1.1 / 2.3 / 3.3%) but **never wins on one-shot
  time** and is even slower to query (§4). Dominated.
- **6-bit** is the compromise: 1.9 / 4.3 / 5.8% mismatch, ~1 MiB cache-resident
  table. It loses to 5-bit on time but is the **accuracy floor you'd actually want
  if you ship one depth**, and it still crushes exact-linear at large P (8K/P256:
  191 ms vs 16 096 ms).

### (b) Does it beat the EXACT paths on TOTAL wall-clock? — only in one regime

The exact bars are strong. **kd-tree already makes large-P cheap** (8K/P256:
2277 ms; 2048²/P256: 232 ms), so the LUT's job is only to beat *kd*, not the slow
linear scan. Where Fast-LUT's total beats the BEST exact option:

| regime | does any LUT beat best-exact on total? |
|---|---|
| **512², any P** | **No (practically).** kd is 5.5–16.8 ms; 5-bit only ties/edges it at P256 (13.8 vs 16.8), saving <3 ms for 10.6% error. Not worth it. Build cost dominates the tiny image. |
| **2048², P16/P64** | **Yes, 5-bit** (8–11 ms vs kd 92–158 ms) — but the absolute saving is small and accuracy cost is 3.7–7.6%. |
| **2048²/8K, P256** | **Yes, decisively.** 8K/P256: LUT5 **78 ms** / LUT6 **191 ms** vs kd **2277 ms** (12–29× faster). This is the real win. |
| **8K, P16/P64** | **Yes**, LUT5 31–44 ms vs kd 692–911 ms (16–22×). |

So the **clean win is large images (≥2048²) at high P (≥64, biggest at 256)** —
exactly the regime where even exact-kd takes hundreds of ms to seconds. At small
images **the build cost makes the LUT a wash-or-loss against kd**, confirming the
prompt's worry.

### (c) If you only do exact-once, kd is usually enough — the LUT's true home is REPEATED remaps

Because build dominates one-shot Fast mode, the LUT's strongest use case is
**repeated remaps against a fixed palette** (batch processing many images, a
video/preview pipeline, a watch-mode re-render): build the table **once**, then
every subsequent image is just the ~30–58 ms query (P-independent) regardless of
how slow exact would be. Amortized over K images, total ≈ `build + K·query`, and
the per-image cost collapses to the query — which beats every exact path at every
P and size. **If pixelize has a batch/watch path, that is where Fast-LUT pays for
itself unambiguously.** (pixelize does have `batch.go` and `watch.go`.)

### Recommendation

**Ship a Fast mode, opt-in only, at 6-bit, gated to large-image / high-P (and to
batch/watch).** Concretely:

1. **Opt-in flag** (e.g. `--fast` / `Options.Fast`), never the default; it must
   **declare its accuracy** ("approximate: ~2–6% of pixels may differ by a mean
   ~40–60 RGB units, max ~180; grows with palette size"). The exact default
   (`flat-Pix16` for P<128, kd for P≥128) is untouched.
2. **6-bit depth.** It is the honest accuracy/footprint point (1 MiB, cache-
   resident, ~2–6% mismatch). 5-bit is faster but its 7.6–10.6% mismatch at P≥64
   is too visible for a quantizer; 7-bit is dominated (huge build, slower query).
3. **Gate the build/use on regime.** Only engage Fast-LUT when it can win:
   `pixels ≥ 2048²` AND `P ≥ 64` for one-shot; **always** for batch/watch against
   a fixed palette (build once, reuse). For small images or small P, Fast mode
   should silently fall back to the exact path — the LUT build would make it
   *slower* and less accurate for no benefit.
4. **Parallel build** (the serial build is 3–4× slower and pointless to ship).
5. Reuse the existing **BAND** lever for 8K memory if needed; the table is shared
   read-only.

**Plain honesty:** for the *one-shot single image* case the exact kd-tree is
already fast enough (≤2.3 s even at 8K/P256) that a Fast mode buys speed only by
spending real accuracy, and only on big high-P images. The unambiguous, no-
caveats win is **repeated remaps against a fixed palette**, where build amortizes
to zero. If pixelize's product surface is "one image at a time," a Fast mode is a
marginal, accuracy-costing convenience and **could reasonably be skipped**; if it
has a batch/preview/watch surface (it does), ship the 6-bit opt-in LUT for that.

---

## 7. Honesty caveats

- **Container noise.** Shared 4-core box; absolute ms are ±10–15% and query_ms
  wobbles P-to-P (it is genuinely P-independent — one lookup). The 8K/P256
  exact-linear 16.1 s and kd 2.28 s are best-of-3 and dominate their cells'
  variance is small relative to the gap. Trust the *ratios and crossovers*, not
  1-ms precision.
- **Accuracy definition.** I count a mismatch only when the LUT color is strictly
  farther from the original pixel than the exact color, so equidistant-tie
  differences are excluded (the same property reports 07/09 cared about). This is
  stricter than "index differs" and yields slightly lower %s than report 07's
  raw differ-count; both are reported honestly.
- **Representative color.** I used the **cell-center** color as each cell's
  representative (base + half-cell), the standard inverse-colormap choice. A
  finer build that tests cell corners or stores a second-nearest would cut
  mismatch but multiply build cost — not worth it given build already dominates.
- **Alpha / opaque.** Like the exact paths, this assumes opaque 8-bit RGB; the LUT
  is keyed on RGB only. A non-opaque source would need the alpha term and is out
  of scope (same caveat as reports 08/09).
- **Custom DistanceFunc.** The LUT is built with the default stdlib metric. A
  non-decomposable metric (CIEDE2000) would require rebuilding the LUT with that
  metric in the build step — the query stays O(1), so a LUT is actually *more*
  attractive for an expensive metric (the cost moves entirely into the one-time
  build) — but accuracy vs that metric was not measured here.
- **Build parallelism** scales the build ~3–4× on 4 cores (e.g. 7-bit/P256/512²:
  2194 ms serial → 687 ms parallel); it does not change the verdict because even
  the parallel build is too large to amortize on one small image.
