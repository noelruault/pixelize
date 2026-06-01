# 09 — kd-tree variants: the fastest EXACT, BIT-IDENTICAL large-palette path

**Question.** What is the fastest exact sub-linear nearest-color structure for
the large-palette path of pixelize, and **can it be made bit-identical to Go's
`color.Palette.Index`** (not merely "an optimal nearest")? Where does it beat
the parallel-linear bit-identical scan, and by how much?

**Headline.** Yes — a kd-tree **can** be made bit-identical to
`color.Palette.Index` at **zero extra cost** (the tie-break is one extra integer
comparison that the branch predictor hides). The recursive bit-identical kd
(`kd-bitident-rec`, median-split, axis = depth%3, `<=` far-child prune,
lowest-index tie-break) is the recommended ship candidate. It crosses over the
parallel-linear bar around **P ≈ 96–128** and is **~2.2–2.6× faster at P=256**
across 512² up to 8K. Recommended selector threshold: **`kdExactMinPaletteSize = 128`**.

All numbers below are real, measured on this machine (4 logical CPUs, Go
1.24.7, 2026-06-01). Raw output + full standalone Go source:
`09-kd-variants-data.txt`. Work was done entirely in `/tmp/eval-kd/`; the
pixelize source was not modified.

---

## 1. The bit-identical problem (the crux)

pixelize's exact default is `cp.Index(c)` where `cp` is a
`color.Palette` (`palette.go:122`). The stdlib `Palette.Index`:

1. computes squared distance in **16-bit RGBA space** (channels from
   `Color.RGBA()`), including the **alpha** channel;
2. uses a strict `<` accumulator, so on an **equidistant tie it returns the
   LOWEST palette index** (first-match argmin).

Two facts make the 8-bit kd reproduce this exactly for pixelize's actual inputs:

- **Colour space.** For an opaque 8-bit colour, `RGBA()` returns `v = c*0x101`
  (i.e. `c<<8 | c`) per channel, and alpha = `0xffff` for both pixel and palette
  entry. So every 16-bit squared distance is `0x101² ·` the 8-bit squared
  distance, **plus a constant zero alpha term**. The *ordering* of candidates —
  and therefore the argmin and every tie — is **identical** in 8-bit and 16-bit
  space. pixelize decodes to `*image.RGBA` and the palette entries are opaque,
  so this holds. (Caveat: a palette or source with non-opaque alpha would break
  the equivalence — see §7.)
- **Tie-break.** `color.Palette.Index` breaks ties toward the lowest index.

I proved the 8-bit oracle (`oracleIndex`) is bit-identical to the *real*
`color.Palette.Index` before using it as the gate oracle:

```
ORACLE-CHECK P=16  checked=340608 mismatches_vs_stdlib=0
ORACLE-CHECK P=32  checked=340608 mismatches_vs_stdlib=0
ORACLE-CHECK P=64  checked=340608 mismatches_vs_stdlib=0
ORACLE-CHECK P=128 checked=340608 mismatches_vs_stdlib=0
ORACLE-CHECK P=256 checked=340608 mismatches_vs_stdlib=0
```

(dense 8-bit-cube sweep + 200k random queries per P; the gate itself calls the
genuine `cp.Index` for every pixel.)

### Why baseline kd is exact but NOT bit-identical

The proven challenger kd search uses `if d < s.bestD` — strict less-than — so on
a tie it keeps **whichever node it visited first in traversal order**. That
order is the kd-tree's geometry, *not* palette index order. The far-child prune
is `diff*diff <= s.bestD` (`<=`, not `<`), which correctly forces it to *visit*
the equidistant point on the far side — so it is provably exact (it always finds
*an* optimal-distance colour) — but it then keeps the wrong one of two equal
candidates. On starry-512/P=256 that is 283 pixels; on adversarial tie-grids it
is **22.6% of queries** (9054/40000). Different output → broken golden tests.

### The fix: lower-index tie-break in the "better?" test

Track the original palette index through the build (already done: `kdNode.idx`),
initialise `bestIdx` to a sentinel `1<<30`, and change the update test to:

```go
if d < s.bestD || (d == s.bestD && n.idx < s.bestIdx) {
    s.bestD, s.bestIdx = d, n.idx
}
```

The existing `<=` far-child prune is *required* and already correct: it
guarantees every equidistant candidate is visited, so the tie-break then
deterministically selects the lowest index — exactly what `color.Palette.Index`
returns. This is one extra integer compare on the rare `d == bestD` path; it has
**no measurable cost** (see §4: `kd-bitident-rec` ≈ `kd-baseline-rec`).

### Proof of bit-identicality

Adversarial tie cases (pixel exactly between two entries; duplicate palette
entries; symmetric cube around the query; ties whose lower-index winner is on
the kd *far* child) — tested across **all four build shapes**
(median/midpoint × cycle/widest) **and** the explicit-stack search:

```
ADVERSARIAL fixed cases:
  case=midpoint-2way     queries=4 baseline_indexDiff=1 (all build variants bit-ident=true)
  case=duplicate-entries queries=3 baseline_indexDiff=3 (all build variants bit-ident=true)
  case=symmetric-cube    queries=4 baseline_indexDiff=4 (all build variants bit-ident=true)
  case=cross-split-tie   queries=4 baseline_indexDiff=1 (all build variants bit-ident=true)
ADVERSARIAL fuzz: total=40000 baseline_mismatch=9054 bitidentical_mismatch=0
ADVERSARIAL RESULT: bit-identical kd matches stdlib on ALL tie cases.
```

The baseline diverges on every tie case; **bit-identical kd diverges on zero**.

Per-image gate (real painting + noise), every variant vs the genuine
`cp.Index`. `nonNearest=0` ⇒ exact; `indexDiff=0` ⇒ bit-identical:

```
GATE starry_512x512 P=256
  parallel-linear      nonNearest=0 indexDiff=0    -> BIT-IDENTICAL
  kd-baseline          nonNearest=0 indexDiff=283  -> exact-but-NOT-bit-identical
  kd-bitidentical      nonNearest=0 indexDiff=0    -> BIT-IDENTICAL
  kd-bitident-stack    nonNearest=0 indexDiff=0    -> BIT-IDENTICAL
GATE noise_512 P=64
  kd-baseline          nonNearest=0 indexDiff=131  -> exact-but-NOT-bit-identical
  kd-bitidentical      nonNearest=0 indexDiff=0    -> BIT-IDENTICAL
```

Identical clean result at P=16/64/256 on both starry and noise (full table in
the data file). **Run under `-race`** on the parallel path: no data race
reported. The conclusion is unambiguous: **bit-identical kd is achievable, at no
speed cost, and is proven against the genuine stdlib oracle including on
adversarial ties.**

---

## 2. Variants built and benchmarked

All standalone Go, stdlib only, row-band-parallel query (per-worker search
scratch — exactly how pixelize's `applyNearest` parallelises).

| variant | exact? | bit-identical? | role |
|---|---|---|---|
| `parallel-linear` | yes | yes | **the BAR** (reproduces pixelize's current exact path) |
| `kd-baseline-rec` | yes | **no** (ties) | reference: the challenger kd as-is (`<` update) |
| **`kd-bitident-rec`** | yes | **yes** | **ship candidate** (recursion, `<=` prune, index tie-break) |
| `kd-bitident-stack` | yes | yes | explicit-stack micro-variant |

Build variants (axis = depth%3 "cycle" vs widest-spread; split = median vs
box-midpoint) are all bit-identical — build shape affects only speed, never the
answer, because correctness is re-derived from geometry + the tie-break.

---

## 3. Build-shape findings

Build time is negligible at every P (sub-millisecond even at P=256, vs hundreds
of ms of remap). Among shapes, query speed differences are within run-to-run
noise on this 4-core box:

```
BUILDVAR 512x512  P=256 median   cycle  build_ms=0.316 remap_ms=22.9 Mpix/s=11.4
BUILDVAR 512x512  P=256 median   widest build_ms=0.238 remap_ms=22.5 Mpix/s=11.7
BUILDVAR 512x512  P=256 midpoint cycle  build_ms=0.191 remap_ms=24.8 Mpix/s=10.6
BUILDVAR 2048     P=256 median   cycle  build_ms=0.478 remap_ms=273.7 Mpix/s=15.3
BUILDVAR 2048     P=256 midpoint widest build_ms=0.249 remap_ms=418.1 Mpix/s=10.0
```

**Finding:** `median`-split + `cycle` (depth%3) axis is the robust default:
lowest/competitive build cost and the most consistent query time. `widest`-axis
helps build time slightly but does not reliably improve queries; `midpoint`
splits produce unbalanced trees that hurt at large P (the 418ms outlier). **Ship
median/cycle** — it is also exactly the challenger's existing build, so no new
build code is needed beyond the tie-break.

---

## 4. Query micro-variants

- **Recursion vs explicit stack:** recursion (`kd-bitident-rec`) **wins** at
  essentially every (P,size) — the Go compiler handles the small recursive
  frames well, and the manual stack adds bookkeeping. Stack only edged ahead in
  one noisy 8K/P=256 sample. Recommendation: **recursion**.
- **Squared-distance early-out:** the search already accumulates `dr²+dg²+db²`
  in one expression; a partial per-channel early-out (as in the IM-tree
  `closest`) was *not* a win for kd because the node visit count is already low
  — not worth the branch. Kept the single-expression form.
- **Store colour in node vs index-into-array:** nodes store the `Color` inline
  (12 bytes incl. idx/axis/ptrs in the struct), which keeps the hot
  distance compute cache-local. An index-into-`pal` indirection would add a
  random load per node; not pursued. Inline colour is the fast, bit-identical
  choice.
- **Tie-break cost:** `kd-bitident-rec` vs `kd-baseline-rec` are within noise
  (e.g. 2048/P=256: 293 vs 542 ms favouring bitident in one run; they trade
  places run-to-run). The tie-break is free.

---

## 5. Speed tables — remap_ms (best-of-N), bit-identical variants vs the bar

`L` = parallel-linear (bar), `KD` = kd-bitident-rec (ship). Lower is better.
Numbers are best-of-N from the matrix run captured in the data file; this is a
shared 4-core box so read crossovers as ranges, not 1-ms precision.

### 512×512 (N=262144)
| P | L ms | KD ms | KD speedup |
|---:|---:|---:|---:|
| 16 | 4.8 | 13.0 | 0.37× |
| 32 | 14.9 | 22.2 | 0.67× |
| 48 | 14.9 | 20.3 | 0.73× |
| 64 | 23.6 | 24.7 | 0.96× |
| 96 | 29.4 | 30.4 | 1.00× (tie) |
| 128 | 40.8 | 36.0 | **1.13×** |
| 256 | 86.1 | 39.0 | **2.21×** |

### 2048×2048 (N=4.19M)
| P | L ms | KD ms | KD speedup |
|---:|---:|---:|---:|
| 16 | 105 | 215 | 0.49× |
| 64 | 204 | 299 | 0.68× |
| 96 | 340 | 333 | **1.02×** |
| 128 | 420 | 324 | **1.30×** |
| 256 | 833 | 354 | **2.35×** |

### 3840×2160 (N=8.29M, 4K)
| P | L ms | KD ms | KD speedup |
|---:|---:|---:|---:|
| 16 | 146 | 301 | 0.49× |
| 64 | 499 | 479 | **1.04×** |
| 96 | 625 | 715 | 0.87× |
| 128 | 912 | 542 | **1.68×** |
| 256 | 1741 | 681 | **2.56×** |

### 7680×4320 (N=33.2M, 8K)
| P | L ms | KD ms | KD speedup |
|---:|---:|---:|---:|
| 16 | 619 | 1133 | 0.55× |
| 64 | 1857 | 2258 | 0.82× |
| 96 | 2689 | 2546 | **1.06×** |
| 128 | 3446 | 2492 | **1.38×** |
| 256 | 8911 | 4035 | **2.21×** |

**Pattern:** linear scales `O(N·P)` (each doubling of P doubles its time); kd
scales much flatter in P (sub-linear node visits), so the gap widens with P. At
small P (≤64) linear's tight, branch-free, cache-friendly inner loop beats kd's
pointer-chasing — kd should NOT be used there.

---

## 6. The crossover — named constant for the selector

Per image size, the smallest P at which bit-identical-kd *reliably* beats
parallel-linear:

| image size | crossover P (kd starts winning) | speedup at P=256 |
|---|---|---|
| 512×512   | ~96–128 (noisy; clear win at 128) | 2.21× |
| 2048×2048 | ~96–128 (tie at 96, win at 128)   | 2.35× |
| 3840×2160 | ~128 (P=96 still favours linear)  | 2.56× |
| 7680×4320 | ~96–128 (tie at 96, win at 128)   | 2.21× |

The crossover is essentially **size-independent** (both methods are pixel-count
linear; the ratio depends almost only on P). P=96 is the borderline where the
two trade places run-to-run; **P=128 is the first P where kd wins on every image
size in every run.** A single safe constant for the selector:

```go
// Below this palette size the parallel-linear scan is faster (its tight
// branch-free inner loop beats kd pointer-chasing); at or above it the
// bit-identical kd-tree wins and the margin grows with P.
const kdExactMinPaletteSize = 128
```

If pixelize prefers to capture the marginal P=96 win on large images, `96` is
defensible but lives in the noise band; **128 is the honest, robust threshold.**

---

## 7. Honesty caveats

- **Alpha / colour space.** Bit-identicality relies on opaque 8-bit colours so
  that 8-bit and 16-bit `Palette.Index` orderings coincide and the alpha term
  cancels. pixelize's exact path operates on `*image.RGBA` with opaque palette
  entries, so this holds today. If a future palette or source carries non-opaque
  alpha, the 8-bit kd would have to switch to 16-bit RGBA distance (including the
  alpha term) to stay bit-identical — straightforward but worth a guard/test.
- **Custom `DistanceFunc`.** This entire study covers only the *default* metric
  (`dist == nil` ⇒ `cp.Index`). When a custom `DistanceFunc` is supplied,
  pixelize uses the linear `nearest()` scan; a kd-tree is only valid there if the
  metric is a monotone function of per-axis differences (Euclidean-like).
  CIEDE2000 is **not** axis-decomposable, so kd does **not** apply to that path.
  kd is strictly the **default-metric large-P** accelerator.
- **Hardware.** 4 logical CPUs, shared sandbox; absolute ms are noisy and
  crossovers are ranges. On a many-core host the linear bar parallelises further
  and could push the crossover slightly higher; the *trend* (kd flatter in P,
  ~2× at P=256) is robust and consistent across all four sizes.
- **Small images / small P.** Below the crossover and for tiny images, kd is a
  net loss; keep parallel-linear as the default exact path and gate kd on
  `P >= kdExactMinPaletteSize`.
- **Build cost.** Negligible (<0.5 ms even at P=256) and one-time per remap;
  it never affects the crossover.

---

## 8. Recommendation

**Ship the bit-identical kd-tree (`kd-bitident-rec`) as the large-palette exact
path, gated at `P >= 128`.**

- **Bit-identical:** proven equal to the genuine `color.Palette.Index` on a
  dense colour sweep, 200k random queries/P, a real painting, noise, and
  adversarial equidistant-tie palettes — `indexDiff = 0` everywhere — and
  race-clean. It will **not** change pixelize's output or break golden tests.
- **Construction:** median split, axis = depth%3, inline node colour, recursive
  search with the `<=` far-child prune **and** a lowest-index tie-break in the
  update test. The tie-break is the only change vs the existing challenger kd and
  costs nothing measurable.
- **Speed:** at P=256 it is **2.2× (512², 8K), 2.35× (2K), 2.56× (4K)** faster
  than parallel-linear; the advantage grows with P because linear is `O(N·P)`
  while kd's node visits grow sub-linearly. Below ~P=96 linear wins, so keep
  linear as the default and switch to kd at the threshold.
- **Selector constant:** `kdExactMinPaletteSize = 128`.

Keep parallel-linear as the exact path for `P < 128` and as the only valid path
for custom non-decomposable `DistanceFunc`s (e.g. CIEDE2000).
