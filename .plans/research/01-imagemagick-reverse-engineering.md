# ImageMagick 6 Palette Remapping & Nearest-Color Assignment — Reverse-Engineering Report

Target command: `convert input.png +dither -remap swatch.png out.png`
Source: ImageMagick 6.9.12-98. Key file: **`magick/quantize.c`** (3431 lines).
Note: this IM6 tree lays the core out under `magick/`, not `MagickCore/`. The
quoted file is real (md5 `0c055af4f23078b76833d1d2bdef7190`, 3431 lines).
Supporting files cited: `magick/magick-type.h`, `magick/thread-private.h`.

All line numbers below are `magick/quantize.c` unless noted. Quotes are verbatim
(whitespace normalized in a few places for readability).

---

## 0. End-to-end trace of `-remap swatch.png`

`-remap` calls **`RemapImage(quantize_info, image, remap_image)`** (def line
3100), where `remap_image` is the swatch. The body does exactly three things:

```c
cube_info=GetCubeInfo(quantize_info,MaxTreeDepth,quantize_info->number_colors); /* 3118 */
status=ClassifyImageColors(cube_info,remap_image,&image->exception);            /* 3124 */
if (status != MagickFalse)
  {
    cube_info->quantize_info->number_colors=cube_info->colors;
    status=AssignImageColors(image,cube_info);                                  /* 3131 */
  }
DestroyCubeInfo(cube_info);
```

So the pipeline is:

1. **`GetCubeInfo(depth=MaxTreeDepth=8, number_colors)`** (1981) — allocate the
   color "cube" (an 8-level, 16-ary color tree) and its scratch.
2. **`ClassifyImageColors(cube_info, swatch)`** (733) — **build the tree from the
   swatch's pixels**: this is what turns the swatch into the palette.
3. **`AssignImageColors(input_image, cube_info)`** (480) — **walk every pixel of
   the input**, find the nearest swatch color via the tree, write the colormap
   index. `+dither` turns dithering off so it takes the plain, OpenMP-parallel
   per-row loop (line 514 `else` branch). With dithering on it would call
   `DitherImage` (513).

`RemapImages` (3165) is the multi-frame version: build the cube once from the
swatch, then loop `AssignImageColors` over each frame (lines ~3198-3211).

The leading header comment (lines ~44-170) documents the classic 3-phase design:
*Classification → Reduction → Assignment*, with the tree described as the RGB
cube `(0,0,0)..(Cmax,Cmax,Cmax)` recursively subdivided into 8 sub-cubes,
"Cmax = 255". For `-remap`, Reduction is skipped (the swatch already defines the
palette); only Classification + Assignment run.

---

## 1. The data structure: the color "cube" (8-level, 16-ary color tree)

### 1a. Node and cube structs (lines 221-316) and constants (208-216)

```c
#define CacheShift  2            /* 3 on Apple */
#define MaxNodes    266817
#define MaxTreeDepth 8
#define NodesInAList 1920

typedef struct _NodeInfo {
  struct _NodeInfo *parent, *child[16];     /* up to 16 children */
  MagickSizeType    number_unique;          /* #palette colors terminating at/under here */
  DoublePixelPacket total_color;            /* running sum of colors under this node */
  MagickRealType    quantize_error;
  size_t            color_number, id, level;
} NodeInfo;

typedef struct _CubeInfo {
  NodeInfo         *root;
  size_t            colors, maximum_colors;
  ...
  DoublePixelPacket target;                 /* the query pixel during search */
  MagickRealType    distance,               /* best squared distance so far */
                    pruning_threshold, next_threshold;
  size_t            nodes, free_nodes, color_number;
  NodeInfo         *next_node;
  Nodes            *node_queue;             /* bump-allocator pages */
  MemoryInfo       *memory_info;
  ssize_t          *cache;                  /* dither memoization table (see §6) */
  DoublePixelPacket error[ErrorQueueLength];
  MagickRealType    diffusion, weights[ErrorQueueLength];
  QuantizeInfo     *quantize_info;
  MagickBooleanType associate_alpha;
  ...
  size_t            depth;
} CubeInfo;
```

### 1b. The node id — one bit per channel per level (inline `ColorToNodeId`, 451)

```c
static inline size_t ColorToNodeId(const CubeInfo *cube_info,
  const DoublePixelPacket *pixel,size_t index)
{
  size_t id;
  id=(size_t) (
    ((ScaleQuantumToChar(ClampPixel(pixel->red))   >> index) & 0x01)      |
    ((ScaleQuantumToChar(ClampPixel(pixel->green)) >> index) & 0x01) << 1 |
    ((ScaleQuantumToChar(ClampPixel(pixel->blue))  >> index) & 0x01) << 2);
  if (cube_info->associate_alpha != MagickFalse)
    id|=((ScaleQuantumToChar(ClampPixel(pixel->opacity)) >> index) & 0x01) << 3;
  return(id);
}
```

**What the tree is.** Not a classic 8-way octree; it is a **16-ary tree keyed on
R,G,B(,A)**. At level with bit position `index` (descent runs `index = 7,6,...`,
MSB first), it extracts **one bit** from each of red→bit0, green→bit1, blue→bit2,
opacity→bit3 and packs them into a 4-bit child index `0..15`. The path from root
to a depth-8 leaf spells out the full 8-bit value of every channel, MSB-first; a
depth-8 leaf is one exact 8-bit RGBA color. (With the classic octree framing in
the header comment, each of the 3 RGB bits halves the cube along one axis — so a
node really does represent a sub-cube of RGB space.)

When the image has no alpha, `associate_alpha` is false, only 3 bits are used, so
only 8 of the 16 child slots are ever populated — which is why the searches loop
`number_children = associate_alpha ? 16 : 8` (lines 1084, 1224, 2498).

Crucially, **everything is reduced to 8 bits via `ScaleQuantumToChar` before
indexing**, so the tree is always ≤ 8 levels regardless of build-time quantum
depth (see §3).

### 1c. How the tree is built from the swatch — `ClassifyImageColors` (733)

For each swatch pixel it collapses identical runs then descends from the root,
creating nodes on demand (lines 810-877):

```c
for (count=1; (x+count) < image->columns; count++)        /* run-length collapse */
  if (IsSameColor(image,p,p+count) == MagickFalse) break;
AssociateAlphaPixel(cube_info,p,&pixel);
index=MaxTreeDepth-1;                                      /* start MSB, index=7 */
for (level=1; level <= MaxTreeDepth; level++)
{
  id=ColorToNodeId(cube_info,&pixel,index);
  if (node_info->child[id] == (NodeInfo *) NULL)
    {
      node_info->child[id]=GetNodeInfo(cube_info,id,level,node_info); /* allocate */
      ...
    }
  node_info=node_info->child[id];
  index--;
}
node_info->number_unique+=count;                          /* this leaf is a palette color */
node_info->total_color.red   += count*QuantumScale*(MagickRealType) ...;  /* +green,blue,opacity */
```

A node is a **palette leaf** iff `number_unique != 0` (checked in `ClosestColor`,
1088, and `DefineImageColormap`, 1228). `total_color` holds the running *sum* of
the colors that fall under a node. **`DefineImageColormap`** (1212), called from
`AssignImageColors` (line 506), walks the tree and, for every leaf with
`number_unique != 0`, emits one colormap entry equal to the **mean** color under
that node — `colormap = total_color * (1/number_unique) * QuantumRange` (lines
1240-1290) — and records the entry's index in the leaf's `color_number` (1290).
For `-remap` against a swatch, a depth-8 leaf almost always holds a single exact
swatch color, so the mean equals that swatch color; thus the palette ends up
being (effectively) the distinct swatch colors. (When this same routine is reused
for *quantization* after pruning, the mean genuinely averages several merged
colors — that is the general case the averaging is there for.)

Nodes are bump-allocated in pages of `NodesInAList = 1920` (`GetNodeInfo`, 2072),
keeping allocation cheap and cache-friendly.

**Build complexity:** each swatch pixel (or run) is an O(`MaxTreeDepth`)=O(8)=O(1)
descent. Build is O(S) in swatch pixels (or fewer, thanks to run collapse),
independent of how many distinct palette colors P emerge.

### 1d. Pruning (reduction) — NOT on the remap hot path

`PruneChild` (2481), `PruneLevel` (2540), `PruneToCubeDepth` (2585), `Reduce`
(2922), `ReduceImageColors` (3021) implement the Gervautz-Purgathofer reduction:
repeatedly merge the lowest-`quantize_error` leaves into their parents
(`parent->number_unique += node->number_unique; parent->total_color += ...;
parent->child[id]=NULL; cube_info->nodes--`, lines 2505-2511) until
`nodes <= maximum_colors`, advancing `pruning_threshold`/`next_threshold` each
pass. This drives `-colors N` quantization, **not** `-remap` against a fixed
swatch. The portable nugget here is the **error-weighted merge bound**, not
needed for plain remap.

---

## 2. The nearest-color search — tree-pruned, NOT linear

This is the crux. Two routines cooperate, inside the plain (no-dither) loop of
`AssignImageColors` (480).

### 2a. Per-pixel descent + bounded search (lines 561-615)

```c
for (x=0; x < image->columns; x+=count)
{
  for (count=1; (x+count) < image->columns; count++)      /* (i) run-length skip */
  { r=q+count; if (IsSameColor(image,q,r)==MagickFalse) break; }

  node_info=cube.root;                                      /* (ii) descent */
  for (index=MaxTreeDepth-1; (ssize_t) index > 0; index--)
  {
    id=ColorToNodeId(&cube,&pixel,index);
    if (node_info->child[id] == (NodeInfo *) NULL) break;   /* deepest existing node */
    node_info=node_info->child[id];
  }
  AssociateAlphaPixel(&cube,q,&pixel);
  cube.target=pixel;
  cube.distance=(MagickRealType)(4.0*(QuantumRange+1.0)*(QuantumRange+1.0)+1.0); /* seed huge */
  ClosestColor(image,&cube,node_info->parent);              /* (iii) search subtree */
  index=cube.color_number;

  for (i=0; i < count; i++) { ...write index / colormap color...; q++; }  /* (iv) bulk write */
}
```

Three cost-savers around the actual comparison:

- **(i) Run-length skip** (575-583): adjacent identical pixels (`IsSameColor`,
  fuzz-aware, def 467) collapse into a `count`; a run of N identical pixels costs
  **one** lookup then `count` cheap writes (604-615). Flat / pixel-art regions
  become nearly free.
- **(ii) Locality descent**: walk to the *deepest existing node* whose path
  matches the query's high bits — O(8), independent of P.
- **(iii)** The search is rooted at **`node_info->parent`** (one level above the
  deepest match), seeded with a distance larger than any possible
  (`4·(QuantumRange+1)²+1`), so the first candidate always wins initially.

### 2b. The comparison — `ClosestColor` (1072)

```c
static void ClosestColor(const Image *image,CubeInfo *cube_info,
  const NodeInfo *node_info)
{
  size_t number_children = cube_info->associate_alpha==MagickFalse ? 8UL : 16UL;
  for (i=0; i < number_children; i++)
    if (node_info->child[i] != NULL)
      ClosestColor(image,cube_info,node_info->child[i]);     /* recurse subtree */
  if (node_info->number_unique != 0)
    {
      p=image->colormap+node_info->color_number;
      alpha=1.0; beta=1.0;                                    /* or opacity weights */
      q=&cube_info->target;
      pixel=alpha*GetPixelRed(p)-beta*q->red;  distance=pixel*pixel;
      if (distance <= cube_info->distance) {
        pixel=alpha*GetPixelGreen(p)-beta*q->green; distance+=pixel*pixel;
        if (distance <= cube_info->distance) {
          pixel=alpha*GetPixelBlue(p)-beta*q->blue; distance+=pixel*pixel;
          if (distance <= cube_info->distance) {
            pixel=alpha*GetPixelOpacity(p)-beta*q->opacity; distance+=pixel*pixel;
            if (distance <= cube_info->distance) {
              cube_info->distance=distance;                  /* new best */
              cube_info->color_number=node_info->color_number;
            }}}}
    }
}
```

- **Distance metric: squared Euclidean in RGBA**, `Σ (alpha·p − beta·q)²`.
  `alpha`/`beta` are opacity weights (`QuantumScale*(QuantumRange-opacity)`) only
  when `associate_alpha` is set (1110-1116); otherwise both are 1.0 and it is
  plain squared RGBA distance. **No `sqrt`** — ordering is preserved on squared
  distance.
- **Partial-distance early-out**: the nested `if (distance <= cube_info->distance)`
  cascade abandons a candidate the instant its running sum exceeds the current
  best, skipping the remaining channels.

### 2c. Why this beats a linear scan as P grows (the complexity argument)

A naive remap is **O(N·P)** (each of N pixels compared to all P palette colors).
ImageMagick is effectively **O(N · small constant)** because:

1. The **descent** (585-591) is O(MaxTreeDepth)=O(8)=**O(1)**, independent of P.
   It lands in the subtree of palette colors that share the query's high bits.
2. `ClosestColor` recurses over **only `node_info->parent`'s subtree**, not all P
   leaves. Because the tree is keyed by color bits MSB-first, geometrically
   nearby colors cluster into the same subtree, so the candidate set is a small,
   bounded neighborhood of the query rather than the whole palette.
3. **Partial-distance early-out** prunes most per-candidate arithmetic.
4. **Run-length collapse** divides total work by the average run length.

So as P grows 4 → 162+, the descent cost is unchanged and the searched subtree
grows only weakly with palette density, while a linear scan grows linearly in P.
That is exactly the flat-vs-linear scaling we measured.

> Accuracy caveat for the port: searching `node_info->parent`'s subtree is a
> heuristic, not a guaranteed k-NN. The true nearest color can occasionally live
> in a sibling subtree and be missed; IM trades a little accuracy for the big
> speed win. Searching the *parent* (not the deepest node) and seeding `distance`
> huge is what makes it "good enough." A correct-but-still-fast port can add a
> branch-and-bound walk with a real bounding-box pruning test (see §7).

---

## 3. Quantum depth (Q8 vs Q16) — why it barely moved the benchmark

`magick-type.h` (lines 67-112), keyed on `MAGICKCORE_QUANTUM_DEPTH` (default 16):

- **Q8**: `Quantum = unsigned char`, `#define QuantumRange ((Quantum) 255)`.
- **Q16**: `Quantum = unsigned short`, `#define QuantumRange ((Quantum) 65535)`.
- `#define QuantumScale ((double)1.0/(double)QuantumRange)` (124).
- (HDRI variants make Quantum a float; Q32/Q64 also exist.)

Decisive fact: **the tree is always keyed on 8-bit-reduced channels.** Both the
build (`ColorToNodeId(...,pixel,index)` with `ScaleQuantumToChar` inside, 458-462)
and the descent (587) feed `ScaleQuantumToChar` into the id. Consequences:

- Tree **depth is always ≤ 8** and tree **shape is identical** for Q8 and Q16;
  node count, descent length, and searched-subtree size are unchanged by depth.
- Only the per-channel **arithmetic operand width** changes (`MagickRealType`
  math on `unsigned short` vs `unsigned char`; `QuantumRange` differs). The
  squared distances are computed in real/float type either way, so the *amount*
  of comparison work per candidate is the same; only operand size differs.
- Memory: Q16 doubles pixel-buffer and `image->colormap` entry size (2 B/channel
  vs 1), but the **cube tree is the same size**, and the hot data (the small
  searched subtree) fits in cache in both cases.

Net: Q8↔Q16 changes operand width and raw pixel bandwidth slightly, not the
algorithmic structure or candidate count — hence the negligible benchmark delta.

---

## 4. Threading / OpenMP

`grep "pragma omp"` hits lines **528, 2361, 2390, 2437, 3314, 3347, 3396**. Map
to enclosing function:

- **528-531** — inside **`AssignImageColors`** (480). THIS is the `-remap` hot
  path:
  ```c
  #pragma omp parallel for schedule(static) shared(status) \
    magick_number_threads(image,image,image->rows,1)
  for (y=0; y < image->rows; y++)
  {
    CubeInfo cube;            /* per-thread private */
    ...
    cube=(*cube_info);        /* line 560: copy cube into thread-local scratch */
    for (x=0; x < image->columns; x+=count) { ...descend; ClosestColor... }
  }
  ```
- **2361/2390/2437** — inside **`PosterizeImageChannel`** (not remap).
- **3314/3347/3396** — inside **`SetGrayscaleImage`** (grayscale fast path).

Strategy on the remap path:

- **Granularity = the image row** (`parallel for` over `y`). `schedule(static)`
  gives each thread a contiguous block of rows: good locality, no scheduling
  overhead.
- **Thread count** from `magick_number_threads(source,dest,chunk,multithreaded)`
  (`thread-private.h:30`), which expands to
  `num_threads(GetMagickNumberThreads(image,image,image->rows,1))` — clamps the
  team to the row count and resource limits (small images use fewer threads).
- **Lock-free hot path**: the cube tree is **read-only** during search and is
  **privatized** via `cube=(*cube_info)` (560), so each thread mutates only its
  own `cube.distance`/`cube.color_number` scratch. Tree nodes are shared but only
  read.
- The only synchronization is `#pragma omp atomic` on the progress counter
  (analogous to 2437 in Posterize) — not on pixel work.

This embarrassingly-parallel, lock-free row decomposition is exactly why we
measured a high CPU-to-wall ratio: near-linear core scaling because rows are
independent and the shared tree is read-only.

(Note: the **dithered** paths do NOT get this row parallelism — see §6 — because
error diffusion is inherently sequential along the scan order.)

---

## 5. Memory behavior / streaming / tiling

- Pixels are accessed through the **pixel cache** via a `CacheView`
  (`AcquireAuthenticCacheView`, `GetCacheViewAuthenticPixels`), **one row at a
  time** (`GetCacheViewAuthenticPixels(image_view,0,y,image->columns,1,...)`,
  552). The pixel cache is RAM-backed when the image fits and transparently
  memory-mapped / disk-backed when it does not. So IM **streams rows** and does
  not require the whole image resident; very large images spill to a mmapped
  pixel cache automatically. This is IM's general tiling/streaming mechanism, not
  specific to quantize.c.
- The **cube tree** is small and held wholly in RAM: bounded by the distinct
  palette colors (≤ `MaxColormapSize`, 256 in IM6 colormap paths), nodes
  bump-allocated in 1920-node pages, built once and shared read-only across
  threads.
- The dither paths additionally allocate the `cube->cache[]` memoization table
  via `AcquireVirtualMemory` (2023-2030), of length `1UL << (4*(8-CacheShift))`
  (= `1<<24` ssize_t ≈ 128 MB at Q non-Apple, indexed by truncated RGBA). It is
  initialized to −1 and lives only for the dithered assignment. Floyd-Steinberg
  also allocates per-thread error rows via `AcquirePixelTLS` (1482).

---

## 6. Dithering path — what `+dither` vs default changes

Dispatch in `AssignImageColors` (511-514):

```c
if ((cube_info->quantize_info->dither != MagickFalse) &&
    (cube_info->quantize_info->dither_method != NoDitherMethod))
  (void) DitherImage(image,cube_info);     /* dithered */
else
  { ...plain per-row OpenMP loop with ClosestColor... }   /* the +dither path */
```

- **`+dither` sets `dither = MagickFalse`**, so we take the **plain branch** (§2):
  each pixel → its single nearest palette color, no error diffusion. This is the
  benchmarked, OpenMP-parallel path and the one to port first.
- Default/`-dither` runs **`DitherImage`** (1905). Important: the **default
  method is Floyd-Steinberg** — `DitherImage` calls `FloydSteinbergDither` unless
  `dither_method == RiemersmaDitherMethod` (1923-1924). (`GetQuantizeInfo` does
  default `dither_method` to Riemersma, but the `convert` CLI's `-dither`
  selection drives this.)
  - **`FloydSteinbergDither`** (1460): classic 2-D error diffusion, **serpentine
    scan** (`v` flips direction by `y & 0x01`, line 1525; `u` reverses on odd
    rows, 1538). Errors pushed to neighbors with the 7/16, 3/16, 5/16, 1/16
    weights (e.g. `pixel.red += 7.0*diffusion*current[u-v].red/16`, 1542). It
    loops `for (y...)` **without** an omp pragma (1488) — serial in scan order,
    with per-thread error buffers only because the helper is structured for reuse.
  - **`RiemersmaDither`** (1776): diffuses error along a **Hilbert space-filling
    curve** with a short exponential-decay error window (`ErrorQueueLength = 16`,
    `weights[]`), giving O(1) state per step.
  - Both reuse the **same tree search** but wrap it in the **`cache[]`
    memoization**: `i = CacheOffset(&cube,&pixel)` (1578/1836); on miss
    (`cache[i] < 0`) run descent + `ClosestColor(node_info->parent)` and store
    `cache[i] = color_number` (1605/1863); on hit reuse it (1610/1868).
    `CacheOffset` (1441) truncates each channel by `CacheShift` and packs RGBA
    into the index. Dithering perturbs colors and breaks run coherence, so this
    color→index cache replaces the plain path's run-length skip.

---

## 7. What pixelize could steal (most portable ideas)

1. **The bit-interleaved 16-ary color tree as a nearest-color index.** Build a
   tree keyed by extracting one bit per channel per level, MSB-first
   (`ColorToNodeId`: red→bit0, green→bit1, blue→bit2, opacity→bit3). Insert each
   palette color once → O(P) build. Lookup = O(8) descent to the deepest matching
   node, then a bounded subtree search. This is the single biggest win and is
   trivial in Go (`type node struct { child [16]*node; ...}`, or `[8]` when no
   alpha). Reduce channels to 8 bits before indexing so depth is fixed at 8.

2. **Search the parent subtree, seeded with a huge best-distance** — replicate
   `cube.distance = big; ClosestColor(node_info->parent)`. Cheap, and it is what
   keeps IM flat as P grows. If you want *guaranteed* nearest (IM's heuristic can
   miss across sibling subtrees), upgrade to a **best-first / branch-and-bound**
   walk: keep the current best squared distance and prune any subtree whose
   bounding box's minimum squared distance to the query already exceeds it. Same
   tree, provably correct, still sub-linear.

3. **Squared-Euclidean distance with partial-distance early-out.** Accumulate
   `dr²+dg²+db²(+da²)` and bail the moment the partial sum exceeds the current
   best. No `sqrt`. Cheap, vectorizable, real constant-factor win.

4. **Run-length collapse of identical adjacent pixels** (`x += count`,
   `IsSameColor`). One lookup per run, then bulk-write the index. Pixel-art /
   swatch-mapped images have long flat runs, so this can dominate the speedup. In
   Go: compare the packed pixel to the previous; extend `count` while equal.

5. **Row-parallel, lock-free assignment with privatized search scratch.**
   Parallelize over rows (goroutine per row-block), share the tree read-only, and
   give each worker its own best-distance/best-index scratch (IM's
   `cube = *cube_info`). No locks on the hot path; near-linear core scaling.

Secondary, only if you add dithering: a **color→index memoization table**
(`CacheOffset`/`cache[]`) amortizes repeated colors when run-length coherence is
broken; and a Hilbert-curve traversal + short error window (Riemersma) is a
compact, low-state alternative to full 2-D Floyd-Steinberg. Note dithering is
inherently serial along the scan order, so it forgoes the row parallelism.
