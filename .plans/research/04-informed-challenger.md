# 04 — Informed Challenger: porting ImageMagick's color tree to Go, exact-ified

**Mandate.** Given the full reverse-engineering of how ImageMagick 6 remaps to a
fixed palette (report 01) and the blind track's prototype numbers (report 03),
build a Go port that does what IM does — bit-interleaved color tree, descend +
bounded subtree search, squared-Euclidean with partial-distance early-out,
run-length collapse, row-parallel lock-free remap — and then *beat IM on
correctness* with a provably-exact variant, while staying competitive on speed.

---

## ⚠️ HONESTY BANNER — read this first (PARTIAL: ENVIRONMENT FAILURE BLOCKED MEASUREMENT)

I designed and **wrote a complete, self-contained Go prototype** that implements
every required piece (IM-style climb tree, exact kd branch-and-bound, parallel
linear, 6-bit LUT, IM-faithful 6-bit dither cache, row-parallel run-length-collapse
driver, plus `verify`/`bench`/`stress`/`imcompare` subcommands). It is saved at
**`/tmp/challenger/main.go`** and the **full source is also preserved verbatim in
the persistent data file** `04-informed-challenger-data.txt` so it cannot be lost.

**What blocked completion.** Two distinct environment problems hit this session:

1. **`/tmp` instability + a cancelled mega-batch.** My first attempt chained many
   dependent tool calls in one parallel batch; an early command (`base64 | python`)
   errored and the SDK **cancelled the entire rest of the batch**, so the prototype
   and report files from that attempt were never actually written. I recovered from
   this: I rewrote `/tmp/challenger/main.go` cleanly in a single `Write` (which the
   SDK confirmed: "File created successfully").

2. **Tool-output channel failure.** After the rewrite, the Bash tool-output
   channel stopped returning stdout for command after command — including trivial
   standalone `echo` probes that had worked moments earlier in the same session.
   `Write`/`Edit` still confirm success (they returned "File created successfully"
   for main.go), but I **cannot read back the output of `go build`, the `stress`
   run, the `verify` runs, the `bench` matrix, or the ImageMagick comparisons.**

**Consequence, stated plainly per the task's "say so" rule:** I have **NO verified
fresh measurements** to report. I refuse to invent or estimate numbers. Every cell
that would hold one of my measurements is marked **`[NOT MEASURED — output channel
down]`**. The prototype compiles to the best of my static review and is preserved;
a re-run on a healthy environment recovers every number via the commands in §9.

**What remains fully trustworthy** (independent of the lost runs):
- (a) The **algorithm design and its faithfulness to IM's source** — I read
  `magick/quantize.c` in full and the exact `ClosestColor`, `ColorToNodeId`,
  descent loop, and dither-cache code are quoted in report 01 and re-derived below.
- (b) The **correctness arguments by construction** for the exact kd variant and
  for *why IM's plain path is exact while its dither cache is not.*
- (c) The blind track's **already-published numbers from report 03**, reused here.
- (d) The **complexity/qualitative conclusions** that follow from (a)–(c).

I separate "argued / from source / from report 03" (trustworthy) from "my fresh
measurement" (not obtained) everywhere below.

---

## 1. What I built (all in `/tmp/challenger/main.go`, stdlib only)

A single Go binary with subcommands, reading/writing **PPM (P6)** so codec cost is
near-zero and isolable.

### 1a. IM-style bit-interleaved color tree (`imTree`, `imSearch`)

Faithful to `magick/quantize.c`:

- **Node id, MSB-first, one bit per channel.** IM's `ColorToNodeId` (line 451):
  `id = (R>>i&1) | (G>>i&1)<<1 | (B>>i&1)<<2` (`+(A)<<3` with alpha). Mine:
  `nodeId = ((r>>i)&1)<<2 | ((g>>i)&1)<<1 | ((b>>i)&1)`. (I put R in the high bit;
  that is just a consistent relabeling of the 8 child slots — tree shape and search
  identical.) Opaque RGB → 8 children, matching IM's `number_children = 8` when
  `associate_alpha` is false.
- **Build:** insert each palette color with an 8-level descent (`i = 7..0`),
  allocating children on demand, store the palette index at the depth-8 leaf. O(P)
  build, depth fixed at 8 because everything is keyed on 8-bit channels (exactly
  why Q8≡Q16 in report 01 §3).
- **Distance:** squared Euclidean, **no sqrt**, with the **partial-distance
  early-out** cascade — accumulate `dr²`, bail if `> best`; `+dg²`, bail; `+db²`,
  keep if `<`. This mirrors IM's nested `if (distance <= cube_info->distance)`.
- **`closest(node)`** recurses all 8 children then tests this node's stored colors,
  identical to IM's `ClosestColor` (recurse children, then test if `number_unique`).

Two search entry points expose the accuracy question:

1. **`matchSubtreeOnly`** — the *naive strawman* people assume IM does: descend to
   the deepest node, search only that subtree. **Not exact**; demonstrated failing.
2. **`match`** — the *faithful* IM model. IM seeds `cube.distance` huge and calls
   `ClosestColor(node_info->parent)`, and its descent loop is
   `for (index=MaxTreeDepth-1; (ssize_t) index > 0; index--)` — **`> 0`, not
   `>= 0`** — so the deepest reachable node is level 1 and IM then searches that
   node's **parent**, i.e. at/near the **root**. IM deliberately widens the search
   back toward the root. My `match` reproduces this by descending to the deepest
   node then **climbing parent-by-parent to the root**, calling `closest()` on each
   ancestor, accumulating the running best. Because the climb reaches the root,
   **this search visits every palette color and is therefore exact** for the plain
   (non-dithered) path.

### 1b. Provably-exact variant — kd-tree branch-and-bound (`kdTree`, `kdSearch`)

Balanced 3-D kd-tree over RGB (median split, axis = depth%3). Search visits the
near child, then the far child **only if `diff*diff <= bestD`** (splitting-plane
test). This is the exact upgrade report 01 §7.2 recommends and the precise fix for
the blind track's kd bug (report 03 §5a: their prune used `<` and missed
equidistant ties; mine uses **`<=`**). Exactness is **by construction** (standard
kd-NN invariant: a half-space is pruned only when it cannot contain a closer point).

### 1c. IM-faithful 6-bit dither cache (`imCache`)

Replicates IM's *real* approximation source. IM's **dithered** paths memoize
answers in `cache[]` keyed on **truncated** channels (`CacheShift = 2` → top 6 bits
→ 64³ = 262 144 entries; report 01 §6, lines 1441/1605/1610). The **first** query in
a 4×4×4 cell fills it; later queries in that cell **reuse** that answer even if their
true nearest differs. My `imCache` wraps the exact climb-search behind exactly this
6-bit cell cache. (The **plain** `-dither None` remap path does **not** use this
cache — it uses run-length collapse — so plain IM remap is exact; the cache only
governs dithered output.)

### 1d. Blind-track methods re-implemented in the same harness

So all numbers would come from one run: **parallel linear** (`bruteMatcher`, exact,
== report 03 algorithm B) and the **6-bit LUT** (`lut6`, approximate, == report 03
algorithm E). The LUT is filled in parallel from brute-force nearest at each cell
*center* with proper 6→8-bit replication (`r<<2 | r>>4`, not zero-fill).

### 1e. Shared driver (`remapParallel`) — IM's threading model

Row-wise worker pool: a `chan int` of rows, `runtime.NumCPU()` workers (box has
**4 vCPU**), each worker holds its **own** search scratch (IM's `cube = *cube_info`
privatization). Tree/kd/LUT are **read-only and shared** (lock-free), exactly IM's
design (report 01 §4). **Run-length collapse** of consecutive identical pixels is in
the inner loop (IM's `x += count`). Processing is **one row per work item**, so
memory is bounded by image + index buffer regardless of height (§5).

---

## 2. Bit-exactness verification design (the `verify` subcommand)

`verify <palN> <image.ppm>` runs, per pixel: brute-force exact nearest (truth) vs
IM-climb tree, kd-tree, subtree-only, 6-bit LUT, IM-faithful cache. It counts
**non-nearest** pixels (strictly worse squared distance — ties correctly **not**
counted) and summed extra distance per method, printing each so a failure is loud.

**Expected by construction:**
- **kd branch-and-bound: 0 non-nearest** (provably exact).
- **IM-climb tree: 0 non-nearest** (climb reaches root; prune is distance-correct).
- **subtree-only: > 0** (the strawman misses across sibling subtrees).
- **6-bit LUT: > 0** (cell-center key error; report 03 measured ~4.6–7.2%).
- **IM-faithful cache: > 0** (cell-collision error — IM's real approximation).

**Measured result: `[NOT MEASURED — output channel down]`.** The harness is written
to fail loudly; a single `./challenger verify 256 starry_512.ppm` recovers it.

---

## 3. Is IM's remap actually approximate? (the headline correctness question)

Two claims that report 01 §2c conflates must be separated:

**Claim A — "descend-then-search-only-the-deepest-subtree is approximate." TRUE.**
The `stress` subcommand builds a 4-color palette where query `(128,0,0)`'s true
nearest is `(127,0,0)` at squared distance **1**, but `127 = 0111_1111` and
`128 = 1000_0000` differ in the **top bit of R**, so `(127,0,0)` sits in a
*different top-level branch*. A search rooted at the deepest matching node never
sees it and returns `(255,0,0)` at distance `127² = 16129`. So `matchSubtreeOnly`
is genuinely wrong here. This is **proven on paper** (the bit math above is
complete); the `stress` run that demonstrates it numerically is
`[NOT MEASURED — output channel down]`.

**Claim B — "ImageMagick's actual *plain* remap is approximate." FALSE; only the
*dithered/cache* path is.** This is the crucial correction:

- IM's descent stops at `index > 0` (level 1) and then searches `node_info->parent`
  with a **huge seeded distance** — i.e. it searches at/near the **root**. The
  Claim-A pathological color *is* under the root, so IM's real code **does** find
  it. IM widens precisely to avoid the Claim-A failure. Report 01 §2c's "trades
  accuracy for speed" overstates the loss for the plain path: with the search
  rooted that high and a correct distance prune, the plain path returns the true
  nearest. My faithful `match` (climb-to-root) reproduces this and is exact (§2).
- The **real** IM approximation is the **`cache[]` memoization** on the **dithered**
  paths only: truncate to 6 bits/channel, reuse the first answer per 4×4×4 cell. My
  `imCache` replicates it; `verify`'s `IMcache` row quantifies its error.
- To confirm on **real ImageMagick's own output**, the harness includes
  `imcompare`/`imcompare-pal`: feed an original image, the palette, and IM's
  remapped output; count pixels where IM's emitted color is **not** the true
  nearest. Planned runs: `convert img.ppm -dither None -remap palN.ppm out.ppm`
  (expect **~0%** non-nearest) and an adversarial 6-bit-cell-collision case under
  default dithering (expect **>0%**). Both `[NOT MEASURED — output channel down]`.

**Bottom line on correctness:** "beat IM on correctness" is **real but narrow.**
For the **plain `-dither None` remap, IM is already exact**, so our exact kd matches
it (no correctness win, only architecture/speed). The correctness win exists against
IM's **dithered** output, and against any cell-cache/LUT scheme (including the blind
LUTs and `imCache`). Our kd branch-and-bound is exact on **every** pixel by
construction, so it strictly dominates any cell-cache/LUT approach on accuracy.

---

## 4. Memory / huge-image behavior (argued; bench cells not measured)

- **Streaming:** `remapParallel` processes one row per work item; the only large
  allocations are the input RGB buffer (read once) and the `[]int32` index output.
  **No per-pixel/per-row growth of structural memory** — tree/kd/LUT built once,
  shared. At 8K (7680×4320 = 33.2 M px): index buffer 33.2M×4 B ≈ **127 MiB**, RGB
  input ≈ 100 MiB; both fixed, independent of thread count. Confirms report 01 §5's
  bounded-memory claim — in Go for free because the hot loop never allocates.
- **Structural extra memory (tiny):** IM-tree a few MB at 256 colors (sparse
  8-level); kd-tree 48 B × P (≤12 KB at 256); 6-bit LUT and 6-bit cache =
  **262 144 × 4 B = 1 MiB fixed each**. None matter next to the image.
- Per-method `HeapAlloc` would print in `bench`; `[NOT MEASURED — output channel
  down]`. Expectation: flat across methods (index-buffer dominated), matching
  report 03 §4c.

---

## 5. Benchmark methodology + isolating IM's remap from codec I/O

**Apples-to-apples with the blind track:** same upscaled `starry.jpg` at
512/2K/4K/8K, fixed-seed palette at 16/64/256. **Deviation, flagged:** my
`makePalette` uses xorshift; report 03 used `math/rand` seed 1234567. These are
*different* random palettes, so accuracy percentages are magnitude-comparable, not
bit-identical, to report 03. **Timing is palette-content-independent for fixed P, so
the speed comparison is valid.** Strict parity = re-seed `makePalette` to mirror
`math/rand`/1234567 (a ~5-line change).

**My Go timing** (`bench`): image read **once, untimed**; matcher **build timed
separately**; **remap-only** loop timed best-of-N. PPM avoids PNG codec → pure
in-memory remap, directly comparable to report 03's "pure in-memory quantize time."

**IM remap-vs-I/O isolation** (the explicit ask): report 03 found IM's 8K wall is
~18.6 s codec out of ~22.5 s. I isolate by **subtraction on a cheap codec**:
1. Time `convert in.ppm -dither None -remap palN.ppm out.ppm` (full; PPM = raw
   byte I/O, no PNG deflate).
2. Time `convert in.ppm out.ppm` (identity = I/O baseline on the **same** PPM).
3. **Remap-only ≈ full − I/O baseline.**
Both via `/usr/bin/time -v`, best-of-3. Using PPM already slashes the codec term
that dominated report 03's PNG numbers; the identity-subtraction removes the rest.
All IM timings `[NOT MEASURED — output channel down]`.

### 5a. Benchmark matrix (structure; my cells not measured, blind cells from rpt03)

Wall time, ms, remap-only. **B-par** and **LUT6** under "rpt03" are report 03's
*published* numbers (4-core, in-memory, PNG-decode excluded).

**pal = 256** (the regime that separates the methods):

| size | linear-par (mine) | imtree-climb (mine) | imcache (mine) | kd-exact (mine) | lut6 (mine) | B-par (rpt03) | LUT6 (rpt03) | IM remap-only (mine) |
|------|------|------|------|------|------|------|------|------|
| 512² | not meas. | not meas. | not meas. | not meas. | not meas. | 30.4 | 35.2 | not meas. |
| 2K   | not meas. | not meas. | not meas. | not meas. | not meas. | 536.4 | 37.2 | not meas. |
| 4K   | not meas. | not meas. | not meas. | not meas. | not meas. | 1859.5 | 131.5 | not meas. |
| 8K   | not meas. | not meas. | not meas. | not meas. | not meas. | 10648.7* | 128.8 | not meas. |

\* report 03 flagged its 8K/256 B number as contention-noisy (best-of 8.4 s).

---

## 6. The questions, answered (from source + complexity + report 03)

These answers do **not** depend on the lost runs; where a precise multiplier needs
my unobtained measurement, I say so.

### Q1. Does porting IM's tree to Go with goroutines beat ImageMagick itself?

**Very likely yes end-to-end, with the margin dominated by I/O and language
overhead, not the algorithm.** IM's plain remap is the *same algorithm* I ported.
On equal footing (remap-only, no PNG codec) the per-pixel work is comparable; Go
wins on the margins by (i) removing PNG codec cost (report 03: the *majority* of
IM's high-res wall — ~18.6/22.5 s at 8K) and (ii) avoiding IM's pixel-cache/CacheView
indirection in favor of flat slices. Honest framing: **end-to-end Go trounces IM at
high res mostly by avoiding PNG; remap-core-to-remap-core they are within a small
constant factor, Go ahead.** Exact ratio: `[NOT MEASURED]`.

### Q2. Does the IM-style tree beat the blind track's 6-bit LUT and parallel-linear?

The *shape* is unambiguous from complexity even without my cells:

- **vs parallel-linear (B):** tree per-pixel is ~flat in P (O(8) descent + small
  bounded subtree); B is O(P). Report 03's B grows **~15× from pal 16→256**
  (36→536 ms at 2K). So the tree **wins at large P (256)** and **loses/ties at small
  P (16)**, where B's tight cache-friendly scan beats pointer-chasing — exactly what
  report 03 found for its kd-tree (8K/16: tree 1607 ms *slower* than B 575 ms;
  8K/256: tree 3224 ms *faster* than B 10649 ms). The IM-climb tree sits near kd.
- **vs 6-bit LUT (E):** LUT is **O(1)/pixel, palette-independent** (report 03:
  ~flat 37–131 ms at high res) and **beats the tree at every size on speed**,
  because the tree pays an 8-level descent + subtree walk per *distinct* pixel.
  **But LUT is approximate** (~4.6–7.2% non-nearest at 6-bit on a random palette)
  and the climb tree is **exact**.

**Speed-vs-exactness verdict:** on *speed* at large P, **LUT6 > tree ≈ kd > linear**;
on *correctness*, **tree(climb) = kd = linear (all exact) ≫ LUT6**. The tree's only
niche over both is "exact **and** palette-scalable" — large palettes where LUT error
is intolerable and linear is too slow.

### Q3. Is IM's remap approximate, and does branch-and-bound beat it on correctness?

- IM **plain `-dither None`:** **exact** (§3 Claim B) → branch-and-bound matches it,
  no correctness win there.
- IM **dithered** + `cache[]`: **approximate** (cell collisions) → branch-and-bound
  (and our exact climb tree) **beat it, 0 non-nearest vs >0**, by construction.
- vs the blind **LUT:** branch-and-bound **strictly wins** (0% vs 4.6–14%).
- **Competitive on speed?** Per report 03's own kd timings (with the `<=` prune mine
  has), kd is **~3× faster than parallel-linear at 8K/256** while slower at small P.
  Competitive at large P, not at small P. Mine-vs-IM remap-only ratio: `[NOT MEASURED]`.

### Q4. Memory / huge-image behavior

Bounded (§4): row-streaming, no hot-loop allocation, structural memory ≤ 1 MiB; 8K
working set ≈ RGB input (~100 MiB) + index buffer (~127 MiB), flat in thread count.
Matches IM's streaming guarantee.

### Q5. Recommended winner(s) by regime

| Regime | Winner | Why |
|--------|--------|-----|
| Small palette (16), any size, **exact** | **parallel-linear** | tree/kd lose to a tight linear scan at small P (report 03); simplest, exact |
| Large palette (256), **exact** | **kd branch-and-bound** | ~3× faster than parallel-linear at 8K/256 (report 03), provably exact with `<=` prune |
| Huge image, **need speed**, tolerate small bounded error | **6-bit LUT** | O(1)/pixel, palette-independent, tens of ms at 8K; ~5–7% non-nearest |
| Need **exact** AND large palette AND big image | **kd branch-and-bound** | the only method both exact and sub-linear in P |
| Match / beat IM output | plain: **kd/linear** (= IM); dithered: **kd** (beats IM's cache) | |

The **IM-style climb tree** is faithful and exact but **dominated**: at small P by
linear, at large P by kd (geometry prunes better than bit-prefix), and on speed
everywhere by the LUT. Its value is compatibility/pedagogy (it is *what IM does*),
not a ship recommendation.

---

## 7. Recommended design (for the judge to act on)

1. **Default exact path = adaptive:** `if P <= ~32: parallel-linear else: kd-tree
   branch-and-bound.` Both exact, row-parallel, run-length-collapsed, lock-free
   shared structure. Beats IM's *architecture* (flat slices, no pixel-cache
   indirection, no PNG in the hot path), matches IM's *correctness* on the plain
   path, exceeds it on the dithered path. Use squared-Euclidean + partial-distance
   early-out throughout.
2. **kd correctness is non-negotiable:** far-child prune **must** be `diff*diff <=
   bestD` (`<=`, not `<`) to handle equidistant ties — the exact bug the blind
   track hit. Gate on bit-exact-vs-brute-force in CI (the `verify` subcommand).
3. **Opt-in fast/preview path = 6-bit LUT**, behind a flag, documented approximate
   (~5–7% boundary flips on neutral palettes, less on tuned), 1 MiB table,
   palette-independent O(1)/pixel. Never the silent default.
4. **Do NOT port IM's `cache[]` for the non-dithered path** — it buys nothing over
   run-length collapse and *introduces* IM's only real inexactness. Only consider it
   if/when adding dithering, where run coherence breaks.
5. **Threading:** goroutine pool over row-blocks, `GOMAXPROCS` workers, per-worker
   scratch, read-only shared structure. ~3.5× on 4 cores expected (report 03's
   linear-scan figure); tree/kd scale similarly.
6. **I/O:** keep the hot path on flat in-memory buffers; do PNG codec outside the
   timed/parallel region. At high res the codec, not the remap, is the wall-clock
   bottleneck — where Go most decisively beats an external `convert`.

---

## 8. Validity / what to trust

- **Trust:** the algorithm faithfulness to `quantize.c` (read in full); the
  by-construction exactness of kd (`<=` prune) and of the IM climb-to-root model;
  the Claim-A/Claim-B correctness analysis (the bit math is complete and checkable);
  the report-03 numbers reused for comparison; the complexity-driven verdicts.
- **Do NOT trust as "measured":** any wall-time, Mpix/s, memory, or non-nearest
  percentage attributed to *my* prototype — the output channel failed before I could
  read any of it. Marked `[NOT MEASURED — output channel down]` throughout.
- **Recover everything** via §9; the buildable source is preserved in the data file.

---

## 9. Reproduction (recover the unobtained numbers)

```
mkdir -p /tmp/challenger && cd /tmp/challenger && go mod init challenger
# save the source from 04-informed-challenger-data.txt as main.go
go build -o challenger .

# inputs: starry.jpg is persistent at /home/user/pixelize/docs/demo/inputs/starry.jpg
for px in 512 2048 4096 8192; do
  convert /home/user/pixelize/docs/demo/inputs/starry.jpg -resize ${px}x${px}! -depth 8 starry_${px}.ppm
done
# fixed-seed palettes matching makePalette() (or emit pal_N.ppm from the same xorshift)

# correctness (expect kd=0 & imtree-climb=0 non-nearest; subtree-only>0; LUT6>0; imcache>0)
for n in 16 64 256; do ./challenger verify $n starry_512.ppm; done
./challenger stress     # expect subtreeOnly_nonNearest>0, climb=0

# Go remap-only benchmark matrix
for px in 512 2048 4096 8192; do for n in 16 64 256; do ./challenger bench starry_${px}.ppm $n 3; done; done

# IM remap vs I/O isolation
#   full:     /usr/bin/time -v convert in.ppm -dither None -remap pal_N.ppm out.ppm
#   baseline: /usr/bin/time -v convert in.ppm out.ppm   ; remap = full - baseline

# is IM exact on the plain path? (expect ~0%)
for n in 16 64 256; do
  convert starry_512.ppm -dither None -remap pal_${n}.ppm im_${n}.ppm
  ./challenger imcompare $n starry_512.ppm im_${n}.ppm
done
```

Full prototype source and the source-fact crib are in
`04-informed-challenger-data.txt`.
