# Plan B — the `quantize` module (palette derivation)

**Goal.** Give pixelize a *palette-generation* feature ("turn any image into N
colors / merge similar colors", workflow B) that is **deterministic, fast, and
measurably more perceptually accurate than the incumbents** (libimagequant /
pngquant, ImageMagick, GIMP, Aseprite's octree). It ships as a `quantize` package
inside pixelize and feeds the existing `Apply` pipeline unchanged.

**This document is planning only** — no source is changed by writing it. It turns
the research record into a phased, benchmark-gated build plan.

## Grounding

- Research: [`noelruault/research/quantization/`](https://github.com/noelruault/research/tree/main/quantization)
  — methodology (`00`), cross-disciplinary transfer (`01`), selection baselines
  (`04`, measured), and the Go harness (`bench/`).
- The crux (from the [nearest-color-scaling](https://github.com/noelruault/research/tree/main/nearest-color-scaling)
  record): "reduce to N colors" = **(A) pick the palette + (B) assign each pixel**.
  **(B) is the exact kd-tree matcher pixelize already ships** (`kdtree.go`,
  `distance.go`). It is k-means' inner loop *and* the final mapping pass of every
  divisive method. So this plan only builds **A**; B is a reused dependency. That is
  why `quantize` is a package here, not a separate project.
- Derived palettes are `Palette[struct{}]`, so dither, build-map, pieces, GIF,
  preview, batch, and watch all work on them with zero new plumbing.

## The puzzle method (how every phase works)

Each pipeline piece (P1 color space, P2 histogram, P3 selection, P4 seeding,
P5 refinement) has multiple candidate implementations. We **implement a candidate
as a harness `Quantizer`, benchmark it in isolation against the median-cut floor,
record the delta + raw data**, and only then decide whether it earns a place in
the stack. No piece ships on intuition; it ships on a measured ΔE2000/speed
delta. Integration phases re-benchmark the *combination* to catch interactions.

Baseline to beat (report 04, six paintings, exact assignment):

| N | median-cut mean ΔE2000 | p95 ΔE2000 | MSE |
|---|---|---|---|
| 8 | 6.189 | 14.093 | 189.46 |
| 16 | 4.857 | 11.197 | 98.62 |
| 32 | 3.925 | 9.098 | 53.15 |

## Status (updated 2026-06-03)

Legend: ✅ done · 🟡 partial · ⬜ pending.

| Phase | State | Evidence / what's left |
|---|---|---|
| 0 — baseline harness | 🟡 | Harness + metrics (CIEDE2000 self-tested vs Sharma) + median-cut/popularity floor done (`research/quantization/04`). **Left:** add **Wu** + **octree** as named baselines. |
| 1 — lead pieces | 🟡 | Maximin-seeded k-means **and** PCA-divisive implemented + measured (`05`); gate met — PCA-divisive beats median cut, maximin ruled out. **Left:** the `Ckmeans.1d.dp` optimal-1D split variant. |
| 2 — color space | ✅ | **Matched-assignment rematch won (`02`): OKLab cluster+assign beats pngquant at every N.** D3 was an assignment mismatch, now fixed. **Left (optional):** HyAB centroid metric. |
| 3 — refinement accel | ⬜ | Plain Lloyd used; **weighted sort-means / Hamerly** not yet benchmarked (`07`). |
| 4 — stack | ✅ | Champion confirmed via fan-outs: OKLab-matched refine, **+ space-filling-curve (Morton) init at N=256** (new best, beats pngquant 6/6 — `09`). Interdisciplinary (`08`) and cross-domain MST/annealing (`09`) shortlists measured & discarded. Stack: RGB→OKLab→OKLab+curve-init by N. |
| 5 — promote to pixelize | ⬜ | No engine code yet; `quantize` pkg + CLI flags + golden/determinism tests pending. |
| 6 — competition shootout | 🟡 | Harness built; on six paintings **ours/refine-oklab beats pngquant at EVERY N** (incl. N=256, 6/6) and ImageMagick everywhere (`10`,`02`). **Left:** CQ100/Kodak scale-up + GIMP. |

**Reports:** `01`, `02`, `04`, `05`, `06`, `07`, `08`, `09`, `10` ✅ · `03` ⬜.
**Cross-cutting finding (from `05`):** seeded k-means is non-deterministic because Go
map order randomizes the histogram → **the engine must sort the histogram
canonically** (carry into Phase 5 correctness).

## Phases

### Phase 0 — Close the baseline (harness) — 🟡 PARTIAL
Add **Wu** (variance-min cut with cumulative moments) and **octree** as `Quantizer`s
in `bench/`. These are the honest "classic best non-iterative" and "what
ImageMagick/Aseprite actually do" baselines. *Measure:* ΔE2000/MSE/time vs median
cut at N∈{4,8,16,32,64,256}. *Gate:* Wu should beat median cut at ~equal cost
(expected from the literature); if it doesn't, the harness has a bug — fix before
proceeding. *Output:* report `04` updated, `02-pieces-color-space.md` stub.

### Phase 1 — The two lead cross-disciplinary pieces (P3) — 🟡 PARTIAL
Implement, each as a `Quantizer`, measured as a delta vs Wu:
1. **Maximin-seeded k-means** — deterministic Gonzalez farthest-point seeding (seed
   1 = most-frequent color) on a frequency-weighted histogram, then a few Lloyd
   passes (assignment = kd-tree). (Research 01, piece #1; Celebi.)
2. **PCA-axis median cut with `Ckmeans.1d.dp`-optimal splits** — project each box
   onto its principal axis, split where the exact 1-D k-means DP says, weighted by
   pixel count. Deterministic, non-iterative. (Research 01, piece #2.)

*Gate:* at least one must beat Wu on mean ΔE2000 at N=16 and N=64 without losing
determinism. *Output:* `05-pieces-selection-exotic.md` (+data).

### Phase 2 — Color space as a variable (P1) — ✅ DONE
Add an **OKLab** selection mode (cluster/seed in OKLab; evaluation stays CIEDE2000)
and **A/B HyAB** as the centroid metric. Re-run Phase-0/1 pieces in each space.
*Watch:* the documented trap — CIELAB does **not** automatically beat RGB for
axis-aligned cuts; OKLab is better-shaped but slightly weaker for ΔE; HyAB is
non-Euclidean so it may not reuse a plain Euclidean kd-tree for *exact* assignment
(decide: custom bound, or HyAB-for-centroids / Euclidean-OKLab-for-assignment).
*Gate:* keep a space only if it lowers mean ΔE2000 at equal determinism, net of the
assignment caveat. *Output:* `02-pieces-color-space.md` (+data), `06-seeding.md`.

### Phase 3 — Refinement accelerators (P5) — ⬜ PENDING
Benchmark **weighted sort-means / Hamerly bounds** for the Lloyd passes against
repeated kd-tree queries. Exact, deterministic, zero quality change — pure speed.
*Gate:* the standing pixelize [efficiency rubric](../EVALUATION-RUBRIC.md) (faster
and not heavier → take it; equal time → leaner). *Output:* `07-refinement.md`
(+data).

### Phase 4 — Stack the winners (integration) — 🟡 PARTIAL
Assemble the best `(P1 space × P3 selection/seed × P5 refine)` into one pipeline and
benchmark the **integration**, not just the pieces, to catch interactions (e.g. a
histogram precision fine alone that costs ΔE after refinement). Decide the two
shipped configs:
- a **deterministic default** (likely Wu- or PCA/Ckmeans-init, no randomness), and
- a **`-quantize kmeans` quality mode** (maximin-seeded + refined).
*Output:* `09-integration.md` (+data) — the chosen stack with its number.

### Phase 5 — Promote into pixelize — ⬜ PENDING
Create the `quantize` package (not the harness): `Quantizer` interface, `Generate()
→ Palette[struct{}]`, and `draw.Quantizer` impl so it drops into `image/gif`. Wire
the CLI:
- `-palette auto:N` (derive N colors), `-quantize wu|kmeans|median|octree`,
  `-merge DIST` (agglomerative merge-by-threshold; works on derived *and* loaded
  palettes), reusing the existing `-distance`/`-dither`.
*Tests:* golden + a determinism test (byte-identical palette across runs for the
default); unit tests per algorithm. *Gate:* no regression to existing `Apply`
paths. *Output:* shipped code + `pkg.go.dev` docs.

### Phase 6 — Competition shootout — 🟡 PARTIAL
Add `bench/compare-quant.sh` mirroring the existing `bench/compare.sh`. Run on
**CQ100** (100 images + 8,400 precomputed reference quantizations) at N∈{4,16,64,256}
vs pngquant/libimagequant, ImageMagick, GIMP, Aseprite CLI. Report mean & p95
ΔE2000, RGB-MSE/PSNR, per-image **win-rate**, with pinned versions. *Output:*
`10-vs-competition.md` (+data) and a new README "Quantization benchmark" section,
in the same evidence style as the ImageMagick comparison.

## Definition of Done — Plan B

Done when **all** of:

1. ⬜ **Correctness.** Derived palette + exact nearest-color assignment (reuses the
   shipped kd-tree); the default algorithm is **deterministic** — a golden test in
   pixelize asserts byte-identical palette + output across runs and platforms.
2. 🟡 **Quality, measured.** *(On six paintings, OKLab-matched refine beats pngquant AND ImageMagick at every N (report 02/10); deterministic ours/pca beats ImageMagick at N≤64. CQ100 scale-up still pending before the claim is final.)* On CQ100 at N∈{4,16,64,256}: the default **beats median
   cut and ImageMagick/Aseprite octree** on mean ΔE2000 at every N; the `kmeans`
   quality mode is **≤ libimagequant's mean ΔE2000** (or within a stated small
   margin) with a published per-image win-rate. Numbers and harness are committed
   and reproducible.
3. ⬜ **Speed.** Single-image derive at 512² is within a stated budget (target:
   default ≤ ~2× median-cut time; `kmeans` mode bounded by iteration count), and
   passes the [efficiency rubric](../EVALUATION-RUBRIC.md) on any
   speed/resource trade.
4. ⬜ **Integration.** `-palette auto:N`, `-quantize`, `-merge DIST` all work and feed
   dither / build-map / pieces / GIF / batch / watch unchanged; the Aseprite plugin's
   "Auto (N colors)" path works through the binary with no plugin-side algorithm.
5. 🟡 **Determinism & alpha decided.** Seeding strategy pinned; transparency either
   handled or explicitly documented as straight-alpha-only for v1.
6. 🟡 **Documentation.** Research reports `02,03,05,06,07,09,10` filled with data
   companions; pixelize README has the quantization benchmark; package has GoDoc and
   examples.

**Non-goals for v1 (explicitly deferred):** spatial/joint dither-aware quantization
(scolorq), SLIC pre-pass, CAM16/ICtCp spaces, learned/neural palettes. Kept on the
radar in research `01 §6`, off the critical path.
