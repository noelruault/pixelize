# 12 — Phase-3 §4.2 enhancement variances: close-out (benchmark-first)

**Question.** Phase 3 §4.2 listed three "cheap, proven" enhancements to layer
under the exact scan, each gated on *measured* benefit: **Hamerly + previous-pixel
coherence pruning**, **Morton (Z-order) LUT/grid layout**, and **SoA + an optional
AVX2 distance kernel**. They were never measured against the *current* shipped code
(parallel flat-Pix16 + exact kd + run-length + 6-bit LUT). This report measures all
three and formally closes them, so Phase 3 reads as deliberately finished rather
than open.

**Answer (TL;DR).**

- **Hamerly + previous-pixel coherence — REJECT.** On real images the strict
  coherence shortcut fires only **5.4 % (P64) / 13.6 % (P188)** of the time (adjacent
  photo pixels are rarely close enough to clear the `s(c)` bound), and the per-pixel
  bound check makes the scan **net slower** than the same scan without it
  (P64: 150 vs 132 ns/px; P188: 381 vs 389 ≈ tie). The identical-pixel case it would
  help is **already captured by the shipped run-length short-circuit** (report 10),
  which is strictly cheaper. No regime where Hamerly wins.
- **Morton / Z-order LUT layout — REJECT.** The Morton query is **~2× slower** than
  the shipped row-major LUT (8.3 vs 4.2 ns/px). The 6-bit table is only 1 MiB and
  already cache-resident under spatially-coherent queries; the per-pixel bit-spreading
  (`part1By2`) cost dominates any locality gain. Pure loss.
- **SoA layout — REJECT (no effect).** At equal footing (3-channel, inlined) **SoA
  is indistinguishable from AoS** at every palette size (P4 10.9 vs 10.8; P16 35.3 vs
  36.6; P64 132.7 vs 140.4 ns/px). The matcher is not bottlenecked on the palette's
  memory layout. Splitting channels into separate slices buys nothing and costs
  clarity.
- **AVX2 / SIMD distance kernel — REJECT (not justified).** The scan *is*
  compute-bound (P64 ≈ 281 ns/px vs the 0.6 ns/px memory floor), so SIMD *could*
  speed a linear scan in principle — but the shipped design already routes
  **opaque large-P to the kd-tree** (P188: kd 156 vs linear 389 ns/px), and small-P
  linear is cheap in absolute terms. There is no regime where a build-tagged,
  hand-written-assembly SIMD kernel would be the shipped winner, and Go has no
  portable SIMD intrinsics. The maintenance cost is not repaid.

**Bonus — the one thing that DID ship.** Probing SoA surfaced a real, *bit-identical*
win that is not on the §4.2 list: the shipped opaque linear scan carries the alpha
term and re-derives it for every palette entry, but for an **opaque** source the
alpha term is zero for every entry and cannot change the argmin or any tie.
Dropping it (`indexRawOpaque`, 16-bit promote + `>>2` floor preserved) is **~25 %
less distance work per entry** and measured **1.08–1.29× faster** on the shipped
parallel `applyNearest` (grows with P; nes/P64 = 1.29×), **bit-identical** on both
the real image and adversarial random noise, with no extra memory. Per the
efficiency rubric (faster + strictly less work + bit-identical → take it), it
shipped, gated on `src.Opaque()` exactly like the kd path. It only affects the
opaque small-P branch, since opaque large-P already routes to kd.

> **Method caveat — the "3×" mirage.** An early naive prototype dropped alpha *and*
> the per-channel `>>2` floor (8-bit int32 distance). It looked ~3× faster but is
> **not bit-identical**: the floor can reorder the argmin on adversarial input. It
> only *happened* to match on starry.jpg. The honest, exactness-preserving win is
> ~1.3×, ≈ exactly the 4→3 channel work ratio. Decisions rest on the bit-identical
> form, verified on random noise — not on the naive form.

All numbers measured here: `go version go1.25.0 linux/amd64`, 4 cores, GOMAXPROCS=4,
shared container (noisy — absolute ms swings run-to-run; conclusions rest on ratios
within a single run). Throwaway in-package benchmark deleted after measurement so no
rejected code ships. Raw output + full Go source: `12-phase3-enhancement-variances-data.txt`.

---

## Variants and results

ns/px on starry.jpg (~207k px), best-of-6, this (noisy, `-benchmem`) run. Read
the **ratios**, not the absolute ms.

| variant | P4 | P16 | P64 | P188 | verdict |
|---|---|---|---|---|---|
| `AoS4ch` (BAR: shipped `indexRaw`, exact) | 28.5 | 96.3 | 356.8 | 1027 (linear) | baseline |
| **`Pal16_3ch` (alpha-drop, exact, bit-identical)** | **20.6** | **74.2** | **280.7** | — | **SHIP** |
| `AoS3ch` (naive 8-bit, NOT bit-identical) | 10.8 | 36.6 | 140.4 | — | reject (inexact) |
| `SoA3ch` (naive 8-bit, separate slices) | 10.9 | 35.3 | 132.7 | 389.5 | reject (= AoS) |
| `Hamerly` (exact, coherence shortcut) | — | — | 150.2 | 381.3 | reject (net slower) |
| `KD` (shipped, exact) | — | — | — | 155.6 | (large-P incumbent) |
| `LUTRowMajor` (shipped Fast) | — | — | — | 4.2 | (incumbent) |
| `LUTMorton` (Z-order) | — | — | — | 8.3 | reject (2× slower) |
| `Null` (memory floor) | — | — | — | 0.6 | reference |

Exactness gate (vs the `color.Palette.Index` oracle): `Pal16_3ch` = **0 mismatches**
on the real image *and* on random noise (seeds 1/7/99, P64 & P188). `AoS3ch`/`SoA3ch`
match on the image but are not floor-exact and were never eligible.

## Why each is closed

**Hamerly.** The safe coherence shortcut assigns the previous pixel's center `c`
without scanning iff `d²(x,c) < ¼·minⱼd²(c,cⱼ)` (triangle inequality; strict-less
keeps stdlib's lowest-index tie rule). On photographs adjacent pixels drift just
enough that this fires rarely (5–14 %), so almost every pixel pays the bound check
*and then* the full scan. The truly-identical-neighbour case — where a coherence
trick pays — is already handled, more cheaply, by the gated run-length path
(report 10). Hamerly is redundant where it would help and a tax where it would not.

**Morton.** A 6-bit LUT is 2¹⁸ × 4 B = 1 MiB. Spatially-coherent image queries hit a
small working set of cells regardless of layout, so the table is effectively
cache-resident either way; Z-order's only effect here is to add `part1By2` bit math
to every query. Measured 2× slower. (Morton would matter for a table far larger than
cache with random access — not this one.)

**SoA / AVX2.** SoA is a no-op at the matcher level (layout-insensitive; 3ch AoS = 3ch
SoA within noise). AVX2 is gated on the scan being compute-bound *and* on the linear
scan being the path that matters at scale — but at scale (large P) the kd-tree, not
the linear scan, is the shipped exact path, and it already beats linear by ~2.5×.
A SIMD linear kernel would need to beat kd to matter, in a regime kd owns, at the
cost of assembly + a build tag + a pure-Go fallback. Not worth it. Closed.

## What shipped instead

`indexRawOpaque` on the opaque small-P linear branch — see `palette.go`. Bit-identity
is guarded permanently by `TestParallelMatchesSerial` (opaque, stdlib, P=16/64/127
on random noise vs the `cp.Index` oracle). Matcher microbenchmarks for the shipped
exact and Fast paths live in `bench_test.go` (`go test -bench Matcher`).

**Phase 3 §4.2 is now closed:** the proven enhancements already in the tree are
run-length collapse (report 10) and flat-Pix16 (report 08); the three remaining
candidates (Hamerly, Morton, SoA/AVX2) are measured and rejected; and the incidental
alpha-drop win is shipped. No §4.2 item remains open.
