# Nearest-Color Palette Mapping: Algorithms and Libraries Survey

**Scope.** This report surveys the best-known ways to map *every pixel* of an image to its
nearest color in a *fixed* palette (the "inverse colormap" / palette-remap / dithering-assignment
problem), optimized for speed across palettes of **P = 4 to several hundred** colors and images
from tiny up to **4K and 8K**. It covers search structures, the 3D-LUT idea in depth, SIMD,
what real libraries do, distance metrics, parallelism, and huge-image memory strategy. It ends
with a ranked shortlist of concrete designs for a **Go** implementation.

> Problem framing: we are given a *fixed* palette (already chosen — by median-cut, k-means,
> octree, a brand palette, or a fixed ramp). We only solve the **assignment / remap** step:
> for each pixel color `c`, find `argmin_k dist(c, palette[k])`. This is a 3D (RGB) or 4D
> (RGBA) nearest-neighbor problem repeated N times, where N is the pixel count.

Key numeric anchors used throughout:

| Image | Pixels (N) | RGBA bytes |
|---|---|---|
| 256×256 | 65,536 | 256 KB |
| 1080p (1920×1080) | ~2.07 M | ~8.3 MB |
| 4K (3840×2160) | ~8.29 M | ~33 MB |
| 8K (7680×4320) | ~33.2 M | ~133 MB |

So per-pixel constant factors matter enormously: at 8K, even **1 ns/pixel** is ~33 ms; **20 ns/pixel**
is ~0.66 s single-threaded.

---

## 1. Nearest-color search structures

For each structure: **build cost**, **per-pixel query cost**, **memory**, **accuracy** (exact vs
approximate), and scaling with palette size **P** and pixel count **N**.

### 1.1 Brute-force linear scan

Compute squared distance to all P palette entries, keep the min.

- **Build:** none (O(P) to copy palette into a tight array).
- **Per-pixel:** O(P) distance evals. Total **O(N·P)**.
- **Memory:** O(P), trivially small; ideally Structure-of-Arrays (separate R/G/B arrays).
- **Accuracy:** **exact**.
- **When it is actually fine:** small P and/or when the inner loop vectorizes. With SoA layout and
  integer squared-distance, the loop is branchless, cache-resident (P≤256 palette = <1 KB), and
  auto-vectorizes. It is the *latency floor* against which trees must justify themselves. For
  **P ≤ ~32–64** a SIMD linear scan typically *beats* any tree because tree traversal has
  data-dependent branches and pointer chasing that defeat the branch predictor and prefetcher
  (see §3). Brute force also has zero build latency, which matters for one-shot small images.
- **Where it loses:** large P (256+) on large N without a cache — e.g. 8K × 256 ≈ 8.5 billion
  distance evals.

### 1.2 k-d tree (3D, axis-aligned BSP)

Binary tree splitting on one of R/G/B per level (usually widest-spread axis at the median).

- **Build:** O(P log P).
- **Per-pixel:** average **O(log P)** but with **backtracking**: after descending to a leaf you must
  revisit sibling subtrees whose splitting plane is closer than the current best radius. In low
  dimensions (3D) backtracking is cheap and NN is *exact*. Worst case O(P), but rare for P in the
  hundreds.
- **Memory:** O(P) nodes (each node: color, split axis, two child indices/pointers).
- **Accuracy:** **exact** (with proper backtracking). FFmpeg's `paletteuse` offers both recursive
  and iterative exact kd-tree search.
- **Scaling:** good for medium/large P; the log factor only helps once P is big enough to overcome
  the per-node overhead. For P=4..64 the constant factors usually lose to SIMD brute force.

### 1.3 Octree (RGB-cube 8-way subdivision)

Each level consumes one bit from each of R,G,B to pick one of 8 children; max depth 8 (full 24-bit).
Classic Gervautz–Purgathofer structure; primarily a *palette-building* structure, but it also serves
as an inverse colormap.

- **Build:** O(P · depth) to insert palette colors (depth ≤ 8). Building from the *image* (for palette
  generation) is O(N · depth).
- **Per-pixel (naive descent):** O(depth) ≤ 8 to find the containing cube — but the cube's
  representative is **not** guaranteed nearest near cube boundaries, so an *exact* answer needs a
  bounded backtracking search (ImageMagick's `ClosestColor()` recursively visits candidate
  subtrees and computes true Euclidean distance).
- **Memory:** can be large at full depth (many interior nodes); bounded if depth is capped.
- **Accuracy:** **approximate** if you stop at the containing leaf; **exact** with backtracking.
- **Scaling:** depth is independent of P (≤8), so descent cost is ~constant; but exactness costs the
  same backtracking pain as a kd-tree. Best when you *also* want the octree for quantization.

> **Note on FFmpeg's cache (version skew — verified in source).** There are *two* implementations
> across FFmpeg versions, and both are **exact memo caches** (a miss runs the kd-tree and the result
> is cached), differing only in how the 32,768-bucket hash key is formed:
> - **Older/widely-mirrored:** `#define NBITS 5`, `#define CACHE_SIZE (1<<(3*NBITS)) = 2^15`. Key =
>   `(r & 31)<<10 | (g & 31)<<5 | (b & 31)` — i.e. the **low 5 bits** of each channel concatenated.
> - **Current trunk:** `#define CACHE_SIZE (1<<15)`, key = `ff_lowbias32(color) & (CACHE_SIZE-1)` —
>   a proper 32-bit hash of the **full color**.
> In *both*, each bucket (`struct cache_node`) holds a dynamic list of `struct cached_color {uint32_t
> color; uint8_t pal_entry;}` so multiple distinct colors sharing a bucket coexist and the exact match
> is found by a short linear scan. The bucketing is a pure speed structure, **not** a lossy
> 5-bit-per-channel LUT — lookups return the true nearest color. (Source: `libavfilter/vf_paletteuse.c`,
> `color_get`. The older mirror: https://github.com/yangchaojiang/yjPlay/blob/master/ffmpeg/src/main/jni/ffmpeg/libavfilter/vf_paletteuse.c ;
> trunk doxygen: https://ffmpeg.org/doxygen/trunk/vf__paletteuse_8c_source.html )

### 1.4 Vantage-point tree (VP-tree, metric-space tree)

Binary tree where each node stores a *vantage point* and a *radius* = median distance; points nearer
than the radius go left ("near"), farther go right ("far"). Pruning uses the triangle inequality.

- **Build:** O(P log P).
- **Per-pixel:** average **O(log P)**, exact, with triangle-inequality pruning. Works for *any*
  metric (not just axis-aligned), so it generalizes to weighted/Lab distances.
- **Memory:** O(P) nodes (vantage point color, radius, two children).
- **Accuracy:** **exact**.
- **This is exactly what `libimagequant` uses** (`src/nearest.rs`: `vp_create_node` / `vp_search_node`),
  with two notable accelerators: (a) a precomputed per-color array `nearest_other_color_dist[i] =
  ¼·(dist to nearest other palette color)²` used as an early-exit ("if you're inside half the
  distance to the next color, you're done"), and (b) a `likely_colormap_index` **guess** seeded from
  the previous pixel to tighten the initial best radius and prune harder — a spatial-coherence trick.

### 1.5 Precomputed 3D lookup table (LUT) — the "inverse colormap"

Quantize the RGB key to `b` bits/channel (e.g. 5 or 6), giving a `2^b × 2^b × 2^b` grid; precompute,
for *each cell*, the nearest palette index. Then each pixel is **one array index → O(1)**, independent
of P. Detailed in §2.

- **Build:** O(cells · cost-to-fill). Naive = O(cells · P); exact incremental fill (Heckbert/Thomas)
  is much cheaper.
- **Per-pixel:** **O(1)** — shift+mask to form the key, one table read. Total **O(N)**, *independent of P*.
- **Memory:** one byte/short per cell. 5-bit = 32³ = 32,768 entries; 6-bit = 64³ = 262,144; full
  8-bit = 16,777,216 entries (16 MB at 1 byte/index, 32 MB at 2 bytes).
- **Accuracy:** **approximate** — the table's answer is exact *for the cell-center key*, but the key
  itself was quantized, so a pixel near a cell boundary may be assigned the neighbor cell's color
  (see §2.4 for the error budget). Full 24-bit LUT is effectively exact.
- **Scaling:** the *only* structure here whose per-pixel cost is **O(1) regardless of P** — this is
  the headline property and the reason it dominates for large P on large N.

### 1.6 Summary table

| Structure | Build | Per-pixel | Total | Memory | Exact? | Sweet spot |
|---|---|---|---|---|---|---|
| Brute-force (scalar) | O(P) | O(P) | O(N·P) | O(P) | yes | tiny N |
| **Brute-force SIMD** | O(P) | O(P)/lanes | O(N·P/W) | O(P) | yes | **small P (≤~32–64), any N** |
| k-d tree | O(P log P) | ~O(log P)+bt | O(N log P) | O(P) | yes | medium/large P, no cache |
| Octree | O(P·8) | O(8)+bt | O(N) | med–large | bt=yes | when you also quantize |
| VP-tree | O(P log P) | ~O(log P) | O(N log P) | O(P) | yes | large P, metric ≠ Euclid |
| **3D LUT (reduced)** | O(cells) | **O(1)** | **O(N)** | 32K–256K | approx | **large P, large N, repeats** |
| 3D LUT (24-bit) | O(16.7M) | O(1) | O(N) | 16–32 MB | ~exact | huge N, fixed palette reused |

`bt` = backtracking; W = SIMD lane count.

---

## 2. The 3D LUT / inverse colormap in depth

This is the most promising route to **O(1) per pixel regardless of P**, so it deserves detail.

### 2.1 Why it wins

Once the table is built, remapping is: `key = (r>>s)<<(2b) | (g>>s)<<b | (b>>s); idx = lut[key]`.
That is a couple of shifts, ORs, and a single (ideally L1/L2-resident) load — **no distance math, no
branching, no P-dependence at all.** For P in the hundreds and N at 4K/8K, this crushes any tree.

### 2.2 Full 24-bit vs reduced-bit LUT

- **Full 24-bit:** key = the exact RGB → answer is the true nearest color (exact). Table is 16 MB
  (uint8 index) / 32 MB (uint16). Building all 16.7M entries naively is O(16.7M · P); use the exact
  incremental algorithm (§2.3) or lazy fill (§2.5). Worth it only when N is huge and the same palette
  is reused across many images/frames (amortize the build).
- **Reduced-bit (5 or 6):** key = top bits of each channel. 5-bit table = 32 KB (fits in L1/L2);
  6-bit = 256 KB. Tiny, cache-friendly, fast to build. The cost is **quantization error at the key**
  (§2.4). 6-bit is the usual sweet spot: ~256 KB, error rarely visible.

### 2.3 Exact inverse-colormap algorithms (Heckbert; Thomas)

The naive fill is O(cells · P): for every cell, scan all palette entries. The classic optimizations
fill the table *exactly* but far faster:

- **Heckbert, "Color Image Quantization for Frame Buffer Display," SIGGRAPH 1982** — introduced
  median-cut *and* the inverse-colormap idea: precompute a table mapping subsampled RGB to the nearest
  representative. https://dl.acm.org/doi/10.1145/965145.801294 (also widely mirrored, e.g.
  https://www.cs.cmu.edu/~ph/ — Paul Heckbert's page).
- **Spencer W. Thomas, "Efficient Inverse Color Map Computation," Graphics Gems II (1991), pp. 116–125.**
  The standard exact algorithm. For each palette color it computes the Voronoi region within the
  *discretized* RGB cube by walking cells and using the fact that squared Euclidean distance is
  **separable and incrementally updatable** along each axis: stepping one cell in R changes the
  distance term by a closed-form increment, so you fan out from each color's cell and stamp cells it
  "wins," pruning when a competing color must be closer. Result: every cell gets its exact nearest
  palette index in time far below O(cells · P). Original C code lives in the Graphics Gems repo:
  https://github.com/erich666/GraphicsGems (Gems II, `InvColorMap` / `invcmap.c`).
  Overview of the gem: https://github.com/erich666/GraphicsGems (see GemsII directory).

A later refinement worth knowing: **Brun & Mokrzycki, "A Fast Algorithm for Inverse Colormap
Computation," Computer Graphics Forum 17(4), 1998** — approximates the implicit 3D Voronoi diagram via
a Karhunen–Loève (PCA) transform of the palette plus a correction step, building the inverse map faster
than exact cell-walking at the cost of a small, bounded approximation. Useful when the *build* time of
the LUT itself is on the critical path. https://onlinelibrary.wiley.com/doi/10.1111/1467-8659.00289

These (Heckbert, Thomas) are the canonical references the question asks for. The practical upshot: **build a
reduced-bit LUT with Thomas's incremental algorithm**, get exact-per-cell answers cheaply, then do
O(1) lookups.

### 2.4 Accuracy cost of quantizing the lookup key

A reduced-bit LUT answers exactly for the *cell representative* but the pixel was snapped to that
cell. The worst case: a pixel sits at a cell corner near a Voronoi boundary between two palette
colors; the cell's chosen color may differ from the pixel's true nearest. The maximum positional
error per channel is the cell width = `256 / 2^b`:

- 5-bit: cell width 8 → a pixel can be mis-binned by up to ~8 RGB units near a boundary.
- 6-bit: cell width 4 → up to ~4 units.
- 8-bit: cell width 1 → effectively exact.

This only causes a *visible* wrong choice when two palette colors are closer together than ~2×
cell-width near that pixel, i.e. dense palettes + coarse keys. Mitigations: (a) use 6-bit; (b) build
the LUT using **cell-center** keys (`(q<<s) | (1<<(s-1))`) rather than the truncated corner, halving
average bias; (c) bump to full 24-bit if the palette is dense and reused enough to amortize. Note:
the LUT key error is *orthogonal* to dither — dithering perturbs inputs and largely masks LUT
banding.

### 2.5 Lazy / memoized caching of seen colors (the "cache" approach)

Instead of (or in addition to) precomputing a full grid, **memoize per *unique color seen***. This is
what FFmpeg's `paletteuse` does: a hash table of `CACHE_SIZE = 2^15 = 32,768` buckets (key = either a
full-color hash `ff_lowbias32(color)` in trunk, or `low-5-bits-per-channel` concatenation in older
versions — see §1.4 note); each bucket holds a small list of *exact* colors paired with their resolved
palette index (the list resolves hash collisions).
Lookup hashes to a bucket, linear-scans the (short) list for the exact color; on a **miss** it runs the
kd-tree NN search and *inserts* the result. So the cache is **exact** — the bucketing is a pure speed
structure, not a quantization. (`libavfilter/vf_paletteuse.c`, `color_get`/`colormap_nearest`.)
Source: https://github.com/FFmpeg/FFmpeg/blob/master/libavfilter/vf_paletteuse.c ; filter docs:
https://ffmpeg.org/ffmpeg-filters.html#paletteuse

Why this is great in practice: real images have **far fewer distinct colors than pixels** (photos:
tens–hundreds of thousands distinct; UI/illustration: dozens–thousands). The expensive NN search runs
once per distinct color (or per distinct hashed prefix), and every repeat is an O(1) hash hit. This
gives **exact** results (it caches the *true* NN, computed by the tree) while paying the tree cost
only `O(distinct_colors · log P)` instead of `O(N · log P)`. The hybrid "cache in front of an exact
tree" is the most robust general design.

> Two flavors:
> - **Exact memo cache** (FFmpeg): hash the full color into buckets, store exact colors + indices,
>   exact NN on miss. Exact, but build cost depends on # distinct colors and lookups have a (tiny)
>   bucket scan. (A *prefix*-keyed grid variant trades exactness for a pure O(1) array index.)
> - **Reduced-bit grid LUT** (Thomas): every cell precomputed, pure O(1) array index, but approximate
>   at cell granularity. Pick based on whether exactness or absolute fastest per-pixel matters.

---

## 3. SIMD linear scan: when vectorized brute force beats a tree

For small P, a vectorized brute-force scan is the fastest approach, beating all trees and even
competing with a LUT (no build cost, no cache pollution). Reasons: trees have **data-dependent
branches** (mispredicts), **pointer chasing** (cache misses), and backtracking overhead — all of which
SIMD brute force avoids with a straight-line, prefetcher-friendly loop.

Two vectorization layouts:

1. **One pixel vs many palette colors:** broadcast the pixel's R,G,B; compute distance to W palette
   entries per instruction; horizontal-min to get the index. Good when P fits in a few registers.
2. **Many pixels vs one palette color (preferred):** Structure-of-Arrays — deinterleave pixels into
   separate R,G,B lane arrays; loop palette colors in the *outer* loop, updating a per-pixel running
   `(min_dist, best_idx)` via SIMD compare + blend. Better palette reuse, and the index-tracking blend
   is cheap.

Implementation notes that matter: color values are 0–255, so deltas fit in 16-bit and squared
distances (max 3·255² = 195,075) fit in 32-bit — use **16-bit lanes** where possible to double
throughput, and integer arithmetic (no float). Use **fused multiply-add** for `dr·dr+dg·dg+db·db`.

**Crossover:** the literature and library practice put the break-even where a tree starts to win at
roughly **P ≈ 32–256**, hardware-dependent. Below that, SIMD brute force wins; above it, tree or LUT
wins. In **Go specifically**, hand-SIMD requires assembly (Plan9 `.s`) or a package like
`klauspost`-style intrinsics wrappers; otherwise rely on the compiler's *limited* auto-vectorization
(Go's compiler vectorizes far less aggressively than clang/gcc), which is a strong argument for the
**LUT/cache** route in pure Go (it needs no SIMD to be fast).

---

## 4. What real fast libraries do (data structure for the nearest-color step)

| Library | Palette build | **Nearest-color step** | Notes / source |
|---|---|---|---|
| **libimagequant / pngquant** | median-cut variant + k-means/Voronoi refine | **VP-tree** (`vp_create_node`/`vp_search_node`), exact, with per-color half-distance early-exit + previous-pixel index guess | Rust `src/nearest.rs`. https://github.com/ImageOptim/libimagequant |
| **FFmpeg `paletteuse`** | (palette given by `palettegen`) | **k-d tree in Lab** (recursive *and* iterative variants) + **brute-force** option, fronted by an **exact hash cache** (`CACHE_SIZE=2^15` buckets, key = `ff_lowbias32(full color)`, each bucket a list of exact colors+indices) | `libavfilter/vf_paletteuse.c`. https://github.com/FFmpeg/FFmpeg/blob/master/libavfilter/vf_paletteuse.c |
| **ImageMagick** | **octree** (Gervautz–Purgathofer): Classify→Reduce(least-squares prune)→Assign | octree descent to containing cube, then **`ClosestColor()`** recursive exact-Euclidean search; results **color-cached** | `MagickCore/quantize.c` (MaxTreeDepth=8). https://github.com/ImageMagick/ImageMagick/blob/main/MagickCore/quantize.c |
| **Leptonica** | octree (`pixOctreeColorQuant`, popularity, fixed-octcube, median-cut) | octree as **inverse colormap** — index by interleaved upper bits, O(depth) descent; fixed-octcube variants are pure table lookups | `colorquant1.c`/`colorquant2.c`. http://www.leptonica.org/color-quantization.html |
| **stb (`stb_image_resize` / Image Optim's quantizers)** | simple median-cut-style | simple loops the C compiler **auto-vectorizes** (effectively SIMD brute force) | https://github.com/nothings/stb |
| **exoquant** | histogram → quant → **k-means (Voronoi) refine** | **k-d tree** for the assignment/map phase | https://github.com/exoticorn/exoquant |

Reading across them, the design space splits cleanly:
- **Exact tree** (vp-tree / kd-tree) — libimagequant, exoquant, FFmpeg.
- **Octree** — when the same structure also builds the palette (ImageMagick, Leptonica).
- **Cache/LUT in front** — FFmpeg's hash cache; Leptonica's fixed-octcube; the universal accelerator.

Every high-performance system pairs an **exact NN structure** with **memoization keyed on a quantized
color prefix** — that combination is the de-facto best practice.

---

## 5. Distance metrics: Euclidean RGB vs weighted vs Lab/CIEDE2000

| Metric | Cost/eval | Perceptual quality | Notes |
|---|---|---|---|
| **Squared Euclidean RGB** | cheapest (3 sub, 3 mul, 2 add) | mediocre | what FFmpeg/most fast paths use; *squared* avoids sqrt — monotonic so fine for argmin |
| **Weighted RGB** (e.g. 2,4,3 or luma weights) | ~same | better | cheap perceptual nudge |
| **"redmean"** approximation | a few extra ops | noticeably better than plain RGB | `ΔC² = (2+r̄/256)ΔR² + 4ΔG² + (2+(255-r̄)/256)ΔB²`, r̄ = mean red. https://www.compuphase.com/cmetric.htm |
| **CIE76 (ΔE*ab)** = Euclid in Lab | cheap *after* RGB→Lab conversion | good | needs Lab conversion (cube roots) per color |
| **CIEDE2000 (ΔE00)** | very expensive (trig, weighting, hue-rotation term) | best | ~10–50× slower than CIE76; impractical per-pixel×per-palette without caching |

**Speed/quality strategy:**
1. **Precompute the palette in the working metric once.** Convert the P palette colors to Lab (or
   weighted space) at build time — negligible cost.
2. **Do per-pixel conversion at most once per *distinct* color**, memoized (the §2.5 cache makes this
   cheap). Never convert per (pixel × palette-entry).
3. With a **3D LUT**, the metric only affects *table construction*, not lookup — so you can afford
   **CIEDE2000 at build time** and still get O(1) Euclidean-free lookups at runtime. This is the
   killer combination: best-quality metric, fastest runtime.
4. For squared Euclidean, never take the sqrt — argmin is invariant under the monotonic square.

References for the metrics: Bruce Lindbloom's color math (Lab conversions, ΔE formulas)
http://www.brucelindbloom.com/ ; CIEDE2000 formulation
https://en.wikipedia.org/wiki/Color_difference#CIEDE2000 ; "redmean"
https://www.compuphase.com/cmetric.htm

---

## 6. Parallelism (and how it maps to Go)

The remap is **embarrassingly parallel and read-mostly**: input pixels and the search structure
(tree/LUT) are read-only; each output index is written exactly once at its own offset.

- **Data-parallel over rows or tiles.** Split the image into horizontal **strips** (contiguous row
  ranges) — simplest and cache-friendly because each strip is a contiguous memory region. Tiles help
  only when you need 2D locality (we don't, per-pixel mapping has no neighbor dependency), *except*
  for error-diffusion dither, which has a serial dependency along the scan and needs care (per-row
  ordered dither parallelizes freely; Floyd–Steinberg does not without tricks).
- **False sharing.** Writers touch disjoint, large, contiguous output regions, so the only contention
  is at strip boundaries sharing a 64-byte cache line — negligible because rows ≫ 64 bytes. The real
  false-sharing trap is **shared accumulators**: if workers build a histogram, a color cache, or
  stats, give each worker a **local accumulator** and merge at the end. A *shared* lazy color cache
  (§2.5) would need a mutex/atomic and can false-share; prefer **per-worker caches** merged after, or
  a **fully precomputed LUT** (read-only → zero contention, the cleanest parallel story).
- **Go mapping.**
  - Prefer **static strip partitioning** over a per-row channel worker pool: launch
    `GOMAXPROCS`/`runtime.NumCPU()` goroutines, each handling `height/W` contiguous rows, joined by a
    `sync.WaitGroup`. This avoids channel overhead per row (channels dominate for cheap per-item work).
  - Use a **worker pool with a row/tile-index channel** only if work per row is uneven (it isn't for
    plain remap) or for dynamic load balancing.
  - Read-only LUT/tree shared across goroutines needs **no synchronization** (pure reads). A shared
    *mutable* cache needs `sync.Mutex`/`sync.Map`/atomics — usually not worth it vs per-worker maps.
  - Watch allocations in the hot loop (`b.ReportAllocs()`); operate on `[]uint8`/`[]byte` slices, not
    `image.At()/Set()` (interface calls kill throughput). Index `img.Pix` directly.
  - The job is **memory-bandwidth-bound** at 4K/8K, so speedup saturates below core count; measure.

---

## 7. Huge images (4K, 8K): bounding memory

**Memory math.** 8K RGBA input = 7680×4320×4 ≈ **133 MB**; the paletted output (1 byte/pixel) ≈ **33 MB**.
Holding both simultaneously ≈ 166 MB — fine on a desktop, but if you also keep a decoded `image.RGBA`
plus working copies it multiplies. The structures themselves are tiny by comparison: a 6-bit LUT is
256 KB, a P=256 vp-tree/kd-tree is a few KB.

**Strategies:**
- **Streaming / tiling to bound memory.** Process in **horizontal bands** of, say, 64–256 rows: read
  band → remap → write band → reuse the buffer. Peak memory = `band_rows × width × 4` (input) +
  output band, plus the read-only LUT. An 8K band of 128 rows is 7680×128×4 ≈ 3.9 MB — two orders of
  magnitude under the full frame. This also keeps the working set in L2/L3.
- **The search structure is shared and immutable**, so streaming costs nothing extra there — build the
  LUT/tree once, stream all bands against it. This is the decisive advantage of the **LUT approach for
  huge images**: O(1) per pixel, read-only, trivially tile-able, no per-tile rebuild.
- **When to parallelize.** Below ~1080p, single-threaded with a LUT is already sub-50 ms; the
  goroutine/scheduling overhead can dominate tiny images — gate parallelism on `N > threshold` (e.g.
  ≥ ~1–2 MP). For 4K/8K, parallelize across bands. Combine both: **stream bands, and within/across
  bands fan out to goroutines.** Because it's memory-bound, prefer enough bands in flight to cover
  cores without exhausting RAM.
- **Decode/encode streaming.** Go's `image/png`/`jpeg` decode whole frames into memory; for truly
  memory-tight 8K, decode once but immediately convert to a compact form and free the source, or use
  row-wise codecs where available. The remap itself never needs the whole image resident.

---

## 8. Ranked shortlist of candidate designs for a Go implementation

Ranked by overall recommendation for a general-purpose, fast Go palette remapper across the stated
regimes. "Per-pixel" cost is the steady-state hot path.

### #1 — Reduced-bit 3D LUT (6-bit) built once, O(1) lookups, strip-parallel  ★ default
- **Build:** fill a 64³ = 262,144-entry `[]uint8` (or `uint16` if P>256) table. Use Thomas's exact
  incremental fill, or just brute-force each cell against P (262K×P is cheap and one-time; even
  P=256 ⇒ 67M ops ≈ tens of ms). Build the metric (Lab/CIEDE2000 or weighted RGB) here — it's free
  at runtime.
- **Per-pixel:** shifts+mask+one load → **O(1), independent of P**. ~1–3 ns/pixel.
- **Parallel:** read-only table shared across goroutines; static row strips; zero contention.
- **Memory:** 256 KB table; stream bands for 8K.
- **Wins when:** **almost everywhere** — large P, large N (4K/8K), and pure-Go (no SIMD needed).
  Slight quality loss at cell boundaries (§2.4), masked by dither. **This is the recommended default.**

### #2 — Exact NN tree (vp-tree) + per-worker memo cache  ★ when exactness required
- **Build:** vp-tree over P colors, O(P log P); supports any metric (weighted/Lab/CIEDE2000-at-build).
- **Per-pixel:** hash-cache lookup (full-color hash → bucket → short exact-color list) → hit = O(1);
  miss = exact vp-tree search
  O(log P), then insert. Net cost ≈ `O(distinct_colors · log P)` plus O(N) hash hits.
- **Parallel:** **per-worker caches** merged or just kept separate; tree is read-only.
- **Memory:** tree O(P) + caches; bounded.
- **Wins when:** you need **provably exact** nearest color with a perceptual metric, palettes up to
  several hundred, photographic images with many repeats. Mirrors libimagequant + FFmpeg-cache best
  practice. Slightly slower steady-state than #1, and cache mutation complicates parallelism.

### #3 — SIMD/auto-vectorized brute force, strip-parallel  ★ small palettes
- **Build:** none (SoA palette arrays).
- **Per-pixel:** O(P) but vectorized; for **P ≤ ~32–64** this beats trees and rivals the LUT with
  *zero* build/cache and perfect accuracy.
- **Parallel:** trivial row strips.
- **Go caveat:** Go's auto-vectorization is weak; to truly win you need Plan9 assembly or an
  intrinsics package. In pure Go the integer SoA loop is still fast and branch-free.
- **Wins when:** **small P (4–64)**, any N, when you want exactness and no build step.

### #4 — Full 24-bit LUT, O(1) exact lookups  ★ fixed palette reused across many frames
- **Build:** 16.7M-entry table (16 MB uint8 / 32 MB uint16) via Thomas's incremental algorithm
  (naive 16.7M×P is too slow) or **lazy fill on first sight** of each 24-bit color.
- **Per-pixel:** O(1), **exact** (no key quantization error).
- **Wins when:** huge N and the **same palette is reused** (video, batch) so the 16 MB build/footprint
  amortizes; e.g. an 8K video pipeline with a fixed palette.

### #5 — k-d tree (exact), strip-parallel  ★ medium/large P, no cache, simple
- **Build:** O(P log P); **Per-pixel:** O(log P) + backtracking, exact.
- **Wins when:** P is in the hundreds, you don't want a LUT's memory or a cache's mutation, and a
  modest per-pixel cost is acceptable. This is FFmpeg's non-cached path. Generally dominated by #1
  (slower per-pixel) or #2 (no cache), but the simplest exact structure to implement correctly.

### #6 — Octree inverse colormap  ★ only if you also build the palette
- **Build:** O(P·8); **Per-pixel:** O(depth)≤8 descent (+ backtracking for exactness, à la ImageMagick
  `ClosestColor`).
- **Wins when:** you're *already* using an octree to **generate** the palette (Leptonica/ImageMagick
  style) and want to reuse it for assignment. As a standalone remap structure it's dominated by the
  LUT (#1) on speed and by the vp-tree (#2) on exact-metric flexibility.

### Decision guide
- **Pure-Go, want fastest general default:** #1 (6-bit LUT).
- **Need exact + perceptual metric:** #2 (vp-tree + cache) — or #1 with the metric baked in if
  cell-boundary error is acceptable.
- **P ≤ 64:** #3 (SIMD brute force).
- **Fixed palette, huge/video, want exact O(1):** #4 (24-bit LUT).
- **Always:** stream in horizontal bands for 4K/8K (§7), keep the search structure read-only and
  shared, gate goroutine fan-out on N, index `img.Pix` directly, and bake the distance metric into
  the build step so the hot loop stays metric-free.

---

## Sources
- Heckbert, "Color Image Quantization for Frame Buffer Display," SIGGRAPH 1982 — https://dl.acm.org/doi/10.1145/965145.801294 (author page https://www.cs.cmu.edu/~ph/)
- Thomas, "Efficient Inverse Color Map Computation," Graphics Gems II (1991), pp. 116–125; code: https://github.com/erich666/GraphicsGems (GemsII directory)
- Brun & Mokrzycki, "A Fast Algorithm for Inverse Colormap Computation," Computer Graphics Forum 17(4), 1998 — https://onlinelibrary.wiley.com/doi/10.1111/1467-8659.00289
- Leptonica octree-as-inverse-colormap paper (Bloomberg) — http://www.leptonica.org/papers/colorquant.pdf
- libimagequant VP-tree — https://github.com/ImageOptim/libimagequant (`src/nearest.rs`)
- FFmpeg `paletteuse` kd-tree + 15-bit hash cache — https://github.com/FFmpeg/FFmpeg/blob/master/libavfilter/vf_paletteuse.c ; docs https://ffmpeg.org/ffmpeg-filters.html#paletteuse
- ImageMagick octree + `ClosestColor` — https://github.com/ImageMagick/ImageMagick/blob/main/MagickCore/quantize.c
- Leptonica color quantization — http://www.leptonica.org/color-quantization.html
- exoquant kd-tree — https://github.com/exoticorn/exoquant
- stb — https://github.com/nothings/stb
- "redmean" low-cost perceptual metric — https://www.compuphase.com/cmetric.htm
- Bruce Lindbloom color math (Lab, ΔE) — http://www.brucelindbloom.com/
- CIEDE2000 — https://en.wikipedia.org/wiki/Color_difference#CIEDE2000
