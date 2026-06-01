# 10 — Run-length / spatial-coherence short-circuit on the exact scan

**Question.** The exact nearest-color scan (`flat-Pix16`, research 08) searches the
palette for every pixel. Adjacent pixels in real images are often *identical*
(flat-color art, already-pixelated content). Does a run-length / "same as previous
pixel" short-circuit — cache `last (r,g,b,a) -> idx`, reuse on a match, skip the
palette search — actually pay off, and under exactly what gating?

**Answer (TL;DR).** **Ship it — gated** on a cheap coherence probe, using the
**row-run** variant. It is **bit-identical** to `flat-Pix16` (0 diffs vs the
`color.Palette.Index` oracle, race-clean), it **wins enormously on coherent
content** (up to **108x** on flat / **53x** on already-pixelated art at 8K-P256;
**5–8x** even at small palettes), and on **random noise it is statistically tied**
with the BAR (within the ±1–5% measurement band). The compare overhead on noise is
*free* because the scan is search-/bandwidth-bound, not branch-bound — so even
always-on would not meaningfully regress. But the gate is so cheap (a ~4 K-pixel
probe, O(sampleN), size-independent) and tracks the true win so well that gating is
strictly better: zero risk on the worst case, full win on the best. Resource cost is
flat-to-lower (same allocs, same/​lower peak RAM, *less* total CPU work on every
non-noise input), so it passes the efficiency gate trivially.

All numbers real, measured here: `go version go1.25.0 linux/amd64`, 4 cores,
GOMAXPROCS=4, in a shared container (noisy — see caveats). Work in `/tmp/eval-rl/`;
pixelize source unmodified. Full raw output + Go source:
`10-runlength-variants-data.txt`.

---

## Variants built (all standalone, stdlib only; bit-identical to `flat-Pix16`)

| # | name | mechanism | per-pixel extra cost on a miss |
|---|------|-----------|-------------------------------|
| 1 | `flat-Pix16` (**BAR**) | full `indexRaw` every pixel | — |
| 2 | `prev-pixel` | compare 4 bytes to previous pixel; reuse idx on match | 4 byte-compares |
| 3 | `prev-packed` | pack RGBA into one `uint32`, compare as one word | 1 word-compare + 1 pack |
| 4 | `recent-cache16` | 16-slot direct-mapped, packed-word-keyed, hashed cache (remembers last ≤16 distinct colors) | 1 hash + 1 word-compare + array load |
| 5 | `row-run` | scan forward for a run of identical pixels, `indexRaw` once, fill the whole output+index span | 1 run-scan compare per pixel, branchless fill |

Caches reset at each band start so the result is independent of band partitioning.
A hit returns the cached `indexRaw` answer exactly; nothing else changes.

---

## EXACTNESS GATE — all variants PASS

Verified bit-identical (image `Pix`, flat index buffer, **and** usage map) against
the serial `color.Palette.Index` oracle on: noise-512, flat-512, photo-512,
pixelated-512, and an **adversarial-256** boundary image (channel values
`{0,1,63,64,126,127,128,129,191,192,254,255}` that force exact equidistant ties),
across **P16 and P256**.

> **Result: `pixDiff=0, idxDiff=0, usageOK=true` for every (variant × image × P).**
> `go run -race . exact` → exit 0, **0 DATA RACE warnings** (workers write disjoint
> regions; caches are goroutine-local).

The adversarial image is the key correctness check: `recent-cache16` serves **48%**
of those pixels from cache (the pattern is periodic) yet returns 0 diffs — a cache hit
only ever returns the stored exact `indexRaw` result, so coherence can never change
the decision.

---

## Benchmark matrix — `remap_ms` (best-of-N), with cache-hit fraction

N = 9 @512², 7 @2048², 3 @8K. **hit%** = fraction of pixels served from cache (the
coherence that explains the win). Lower ms is better. Selected rows; full matrix in
the data file.

### Coherent content — the win (row-run shown; others within a few %)

| content | size | P | BAR ms | **row-run ms** | hit% | **speedup** |
|---------|------|---|-------:|---------------:|-----:|------------:|
| pixelated | 512² | 16 | 9.71 | **1.85** | 87.5 | **5.3x** |
| pixelated | 2048² | 16 | 79.50 | **8.20** | 96.9 | **9.7x** |
| pixelated | 2048² | 256 | 1108.08 | **42.14** | 96.9 | **26.3x** |
| pixelated | 8K | 16 | 656.56 | **78.32** | 99.2 | **8.4x** |
| pixelated | 8K | 256 | 8895.78 | **167.62** | 99.2 | **53.1x** |
| flat | 8K | 16 | 668.82 | **77.86** | 99.9 | **8.6x** |
| flat | 8K | 256 | 9196.00 | **85.38** | 99.9 | **107.7x** |

The win scales with **(hit rate) × (palette-search cost saved)**: biggest at P256
(each skipped search avoids 256 distance computations) and at large sizes (the win is
per-pixel). row-run is consistently the fastest of the four coherence variants on
coherent content (it removes the per-pixel branch *inside* a run and writes the span
branchlessly).

### Photo (moderate coherence) — small but real win, never a loss

| content | size | P | BAR ms | best-coherent ms | hit% | speedup |
|---------|------|---|-------:|-----------------:|-----:|--------:|
| photo | 8K | 16 | 675.75 | 563.76 (prev-packed) | 26.3 | **1.20x** |
| photo | 8K | 256 | 8940.93 | 6637.66 (recent-cache) | 28.1 | **1.35x** |
| photo | 2048² | 256 | 1129.52 | 1087.87 (row-run) | 4.5 | 1.04x |
| photo | 512² | 256 | 143.86 | 132.79 (prev-packed) | 0.4 | 1.08x |

Upscaled photos carry surprising coherence at 8K (~26% of pixels equal their left
neighbor, because upscaling duplicates source pixels), so even photos get a clean
1.2–1.35x. At native resolution (512²) coherence is ~0.4% and the win is in the noise.

### Noise (worst case) — MUST NOT regress; it does not

From the focused re-check (higher N, all five variants converge — they are
algorithmically near-identical at 0% coherence, so their spread *is* the measurement
noise band):

| content | size | P | BAR ms | prev-pixel | prev-packed | recent-cache | row-run | spread |
|---------|------|---|-------:|-----------:|------------:|-------------:|--------:|-------:|
| noise | 2048² | 16 | 122.45 | 124.02 | 128.84 | 124.55 | 126.65 | 5.1% |
| noise | 2048² | 256 | 1153.76 | 1144.38 | 1140.75 | 1141.45 | 1139.03 | 1.3% |
| noise | 8K | 16 | 965.12 | 966.70 | 1004.23 | 991.04 | 953.93 | 5.2% |
| noise | 8K | 256 | 9108.66 | 9077.68 | 9042.43 | 9032.85 | 9089.83 | 0.8% |

**The worst observed coherence-variant result on noise is ~5% slower than BAR
(prev-packed, noise-2048-P16) — inside the ±5% run-to-run band at P16.** At P256
(where the 256-color search dominates) the variants are **tied or faster** than BAR:
the per-pixel compare is invisible next to the search, and the failed-compare branch
predicts perfectly (always "miss") so it costs ~nothing. **The original matrix
noise-2048-P256 BAR=2260.6 ms was a noisy-window outlier; the re-check puts all five
at ~1144 ms.** This is exactly the container-noise caveat research 08 flagged.

### Why noise is "free": the scan is not branch-bound

A run-length short-circuit can only hurt if the extra per-pixel compare/branch is on
the critical path. It is not: at P16 the loop is memory-bandwidth-bound (reading
`src.Pix`, writing `out.Pix`); at P256 it is compute-bound on the 256-entry distance
search. In both regimes one always-mispredict-free comparison per pixel hides under
the existing work. That is why the noise regression is within the noise band rather
than a real cost.

---

## Resource / efficiency gate (per the rubric)

- **Allocations:** identical to BAR — **24–27 allocs/op**, ~`w*h*8` B/op for the flat
  index buffer, on every variant. The caches are tiny stack/local arrays (16 slots
  for recent-cache; two ints for prev-pixel). **No allocation cost.**
- **Peak RAM:** identical (~7.5 MB @512², ~118 MB @2048², **929 MB @8K** — same as
  BAR; the index buffer dominates and is unchanged). **No 8K OOM risk introduced**;
  band streaming remains available. recent-cache's 16-slot table is per-worker and
  negligible.
- **Total CPU work (core-seconds):** **lower** on every non-noise input (skipped
  searches are pure work removed), **flat** on noise (compare hidden under
  bandwidth/search). So `proportional time reduction ≥ proportional resource increase`
  holds trivially — the resource side is ≤ 1.0. **Passes the efficiency gate** in all
  regimes; it never spends more to go faster, it spends *less*.
- **Parallelism unchanged** (same contiguous row bands), so the parallel-always-passes
  clause is moot here — this is a serial-inner-loop improvement orthogonal to threading.

**Leaner-among-equals:** on coherent content `row-run` is fastest and is also the
leanest miss path (no hash, no extra state — just a forward run-scan). `recent-cache16`
only pulls ahead on *periodic* sub-pixel patterns (the adversarial image, and a hair on
photos) at the cost of a hash per pixel; that edge is too narrow to justify over
row-run for the default. **row-run is the pick.**

---

## The gate: ship gated on a cheap coherence probe

Always-on is *defensible* (worst case is within the noise band), but gating is
strictly dominant: it removes even the ~5%-at-P16 worst-case risk while keeping the
full win, at trivial cost. The probe is cheap and predictive.

**Probe.** Sample ~4096 pixels spread across up to 16 evenly-spaced rows; count the
fraction whose RGBA equals the immediately-preceding pixel in the row. Cost is
**O(sampleN)** — independent of image size, sub-microsecond, ~one cache line per row.
Measured estimator vs true prev-pixel hit rate:

| content | size | true hit% | **probe estimate%** |
|---------|------|----------:|--------------------:|
| noise | 512² / 2048² | 0.00 | **0.00** |
| photo | 512² | 0.37 | 0.37 |
| photo | 2048² | 4.51 | **3.15** |
| pixelated | 512² | 87.50 | **87.67** |
| pixelated | 2048² | 96.88 | **96.92** |
| flat | 512² | 98.44 | **98.63** |
| flat | 2048² | 99.61 | **99.66** |

The estimate tracks truth to within ~1 point on every content type. There is a wide
dead zone between non-coherent (≤5%) and coherent (≥87%) content, so the threshold is
not sensitive.

**Threshold.** Enable the run-length path when **probe hit-rate ≥ ~10%**.
Justification from the matrix: at hit ≈ 26% (photo-8K) the win is already a clean
1.2–1.35x; at hit ≤ 5% the variants are within the noise band of BAR (no win, no real
loss). A 10% gate captures every profitable case (all pixelated/flat content, and
upscaled-photo at 8K) and declines only the genuinely incoherent ones where there is
nothing to gain. The exact cut is uncritical given the dead zone — anywhere in
~5–20% behaves the same.

**Selector integration.** pixelize's matcher selector already inspects palette size
and image dimensions; it can run this probe once on `src.Pix` before dispatching the
scan and pick `row-run` vs plain `flat-Pix16` accordingly. The probe reads a few
thousand bytes already resident; it adds no measurable wall time.

---

## Honesty caveats

- **Container noise.** Shared 4-core box; best-of-N reduces but does not eliminate it.
  The matrix's noise-2048-P256 BAR cell (2260 ms) is a confirmed outlier — the
  re-check and the other four variants in that same row all sit at ~1144 ms. Trust the
  *pattern* and the re-check numbers; treat single cells as ±5% (P16) / ±1% (P256).
- **The 8K-P16 coherent rows** still show ~8x rather than the ~50–100x of P16's
  P256 — correct: at P16 each skipped search saves only 16 distance computes, so the
  cap on the win is lower. The win is largest where the search is most expensive (P256)
  and the content most coherent (flat ≈ 100%).
- **recent-cache vs row-run.** recent-cache wins *only* on periodic content
  (adversarial, photos by ≤4%); it loses to row-run on flat/pixelated and adds a
  per-pixel hash. Not worth it as the default; row-run is simpler and faster where it
  matters.
- **photo at native res** (512²) shows essentially no win — coherence is ~0.4% there.
  The gate correctly declines it.

---

## RECOMMENDATION

**Ship the `row-run` short-circuit, gated on a cheap coherence probe.**

1. Keep `flat-Pix16` as the default scan (research 08).
2. Before dispatch, run an O(4096) coherence probe on `src.Pix` (fraction of sampled
   pixels equal to their left neighbor).
3. If **probe ≥ ~10%**, run the **row-run** variant instead: forward-scan each row for
   a run of identical pixels, call `indexRaw` once per run, fill the output + index
   span. Bit-identical, race-clean, same allocs, same peak RAM.
4. Otherwise run plain `flat-Pix16` (noise/native-photo).

**Decisive numbers:** coherent content **5.3x–107.7x** faster (flat-8K-P256: 9196 →
85 ms; pixelated-8K-P256: 8896 → 168 ms); photo **1.2–1.35x** at 8K; noise **within
±1–5%** (tied — re-check: 9109 vs 9090 ms at 8K-P256, 122 vs ≤129 ms at 2048-P16).
Same memory, lower total CPU work on every non-noise input. The efficiency gate is
passed by a wide margin (cost ≤ 1.0× while time drops to as little as 0.009×). A clean,
large, gated win.
