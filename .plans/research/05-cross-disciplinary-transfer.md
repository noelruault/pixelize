# Cross-Disciplinary Technology Transfer for Nearest-Reference Assignment

**Author:** cross-disciplinary research agent
**Date:** 2026-05-31
**Status:** research report (no code changes)

---

## 0. Restating our problem in domain-free terms

We have **P reference points** (4 .. several hundred) in a **low-dimensional (3D, occasionally 4D with alpha), roughly-Euclidean** space (a color space). We must assign each of up to **~33 million query points** (8K image pixels) its **nearest reference point**, **repeatedly**, **fast**, on **commodity multicore CPUs**, in **pure Go**, with **bounded memory** for huge inputs.

Two sub-problems:

- **(a) per-query nearest-reference search** — given one query point, find the closest of the P references.
- **(b) the index / quantizer** that makes (a) cheap.

Current baseline: **linear scan, O(N·P)**. ImageMagick: bit-interleaved 16-ary octree-like color tree. Blind prototype: a **6-bit 3D LUT** (262 144 cells) ~150–200× faster but **approximate**.

This is a textbook **fixed-radius / nearest-neighbor query against a small, static set of sites in low-dimensional Euclidean space**, run in enormous batches. Crucially:

- **D = 3 (or 4).** This is the single most important fact, and it is what makes most modern "vector search" machinery (designed for D = 128..1536) the **wrong** tool.
- **P is tiny** compared to N. The "database" (references) is small; the "query stream" (pixels) is gigantic. This is the **inverse** of the usual ANN setting (huge database, few queries).
- Queries are **batched and the reference set is static across the whole image**, so heavy per-image precomputation amortizes over tens of millions of lookups.
- The query space is **bounded and discretizable** (8-bit color = 16.7M possible distinct query values; the input has at most that many *distinct* colors regardless of pixel count).

These four facts are the lens through which every borrowed technique below must be judged.

---

## 1. Molecular dynamics / computational chemistry — cell lists (linked-cell)

### The idea
In MD, every particle interacts with neighbors within a **cutoff radius `r_c`**. Naively that is O(N²). The **cell list / linked-cell** method overlays a **uniform grid** on space with **cell edge ≥ `r_c`**. Then any particle's interaction partners can only lie in its **own cell plus the 26 adjacent cells (a 3×3×3 = 27-cell block)**. Binning is O(N); each particle then checks only the ~constant number of particles in 27 cells, giving **O(N) total** when density is bounded. Cell size is chosen as **the smallest multiple of `r_c` that tiles the box**, often exactly `r_c` (smaller cells = fewer particles per cell but more cells to visit; the classic tradeoff). Sources: Wikipedia "Cell lists" (https://en.wikipedia.org/wiki/Cell_lists); Allen & Tildesley *Computer Simulation of Liquids*.

**Verlet neighbor lists** add a "skin" `r_c + Δ`: build an explicit neighbor list once, reuse it for several timesteps until a particle could have moved more than Δ/2. This trades memory for avoiding rebuilds.

### Real projects
- **GROMACS** — uses a cluster-based variant ("Verlet cluster pair list", groups of 4/8 particles for SIMD) but the foundation is the linked-cell grid. https://www.gromacs.org , source https://gitlab.com/gromacs/gromacs
- **LAMMPS** — classic neighbor.cpp binning + Verlet lists. https://github.com/lammps/lammps , docs https://docs.lammps.org/Developer_par_neigh.html
- **OpenMM** — voxel/cell-based neighbor search on CPU and GPU. https://github.com/openmm/openmm

### Transfer to us
**This is one of the strongest transfers.** Our references are the "particles"; our query is a probe point. Build a **uniform 3D grid over the color cube**, bin the P references into cells, and for a query: locate its cell and scan only that cell + neighbors. Because P is tiny, most cells hold 0 references and we expand the search ring outward until we have a candidate, then verify within a guaranteed radius (the cell ring whose inner radius exceeds the current best distance). This is **exact** (unlike a LUT) and **O(1) amortized** per query for bounded P/cell.

**Concrete cell-size guidance borrowed from LAMMPS:** LAMMPS found a bin size of **half the neighbor cutoff** optimal for typical cases — smaller bins waste time looping over many cells, larger bins waste time on distance checks (LAMMPS neighbor docs, https://docs.lammps.org/Developer_par_neigh.html). The standard cell list checks a **3×3×3 = 27-cell block** when cell edge ≥ cutoff. GROMACS goes further with a **cluster-based Verlet pair list** (groups of 4/8 particles for SIMD), achieving ~12 B/particle storage vs ~800 B/particle for classic neighbor lists — evidence that the *layout* of the binned data matters as much as the binning itself (see §7 CSR layout).

- **Regime:** helps most at **medium-to-large P** (where linear scan over P hurts). At very small P (4–16) plain linear scan or a LUT already wins; grid overhead isn't worth it.
- **Exact or approximate:** **exact**, if you correctly expand rings until the ring's minimum distance ≥ best-so-far. This is the key correctness subtlety MD doesn't face (MD only needs within-cutoff, we need the true minimum).
- **Cell-size choice (borrowed directly):** pick cell edge so that the expected references-per-occupied-cell is ~1–4; for P references in a unit cube, edge ≈ P^(-1/3) gives ~1 reference/cell. This is exactly the MD "cell ≈ cutoff" reasoning inverted to target occupancy instead of cutoff.
- **Effort to port to Go:** **Low–medium.** A few hundred lines: grid build, ring-expansion query, bounded-radius termination. No external deps. The main care is the correct exact-termination proof and handling sparse/empty cells.

---

## 2. Machine learning / clustering — triangle-inequality k-means, and the assignment-as-quantizer insight

### The idea
The k-means **assignment step** ("assign each of N points to the nearest of k centroids") is **literally identical** to our problem (centroids = references, points = pixels). Decades of work accelerate it:

- **Lloyd (naive):** O(N·k·D) linear scan = our baseline.
- **Elkan's algorithm (2003):** keeps **k lower bounds + 1 upper bound per point** plus pairwise centroid distances; uses the **triangle inequality** to skip distance computations. **Exact.** Excellent at high D, but stores **O(N·k) bounds** — memory-heavy.
- **Hamerly's algorithm (2010):** keeps only **1 lower + 1 upper bound per point** (O(N) memory). **Exact.** Empirically the **best choice in LOW dimensions** (≈ D ≤ 8–20), which is exactly our regime; Elkan wins in high D. Sources: G. Hamerly, "Making k-means even faster," SDM 2010 (https://www.cs.kent.edu/~jin/Cloud12/KMeansHamerly.pdf style refs); reference implementation **github.com/ghamerly/fast-kmeans** (implements Lloyd, Elkan, Hamerly, Annulus, Drake, Yinyang).
- **Yinyang / Annulus / Drake:** intermediate bound schemes.
- **kd-tree / ball-tree accelerated assignment** ("blacklisting / filtering algorithm," Kanungo et al. 2002; Pelleg & Moore): build a tree over the *data* and prune whole subtrees to a single centroid. Helps when N≫k and D is low.

### Real projects
- **scikit-learn** `KMeans` — Elkan and Lloyd backends, plus `MiniBatchKMeans`. https://scikit-learn.org/stable/modules/clustering.html#k-means
- **faiss `Kmeans`** — uses faiss's own (flat or IVF) NN index for the assignment step. https://github.com/facebookresearch/faiss/wiki/Faiss-building-blocks:-clustering,-PCA,-quantization
- **github.com/ghamerly/fast-kmeans** — canonical exact-acceleration testbed.

### Transfer to us
Two distinct, both valuable, transfers:

1. **Hamerly-style bounds for the per-query search (exact).** Precompute, once per image, **inter-reference distances**; the nearest-other-reference distance `s(c)` for each reference lets us skip the full P-scan: if a query's distance to its current best `< s(best)/2`, no other reference can be closer. Because *bounds carry across queries that land near the same reference*, and our query stream is spatially coherent (adjacent pixels are similar colors), the upper bound is often already tight. **Exact**, modest memory (O(P) or O(P²) for the pairwise table, both trivial for P ≤ few hundred). **Effort: low.**

**Direct prior art for the LUT-as-precomputed-assignment idea:** **LUT-NN** (Microsoft Research, arXiv:2302.03213, https://www.microsoft.com/en-us/research/publication/lut-nn-empower-efficient-neural-network-inference-with-centroid-learning-and-table-lookup/) does *exactly* the abstract operation we propose: learn centroids, **precompute the nearest-centroid result and store it in a lookup table**, then at runtime "the results of the closest centroids with the inputs can be read directly from the table... without computations." This is independent confirmation, from the ML-systems field, that "precompute the nearest-reference answer into a table" is a recognized, published acceleration technique — our 6-bit LUT is a special case of it. (The dimensionality concern that makes LUT-NN approximate is *removed* in our case because D=3 and the input is already discretizable to 8 bits/channel.)

2. **The "assignment IS a quantizer" reframing.** k-means literature treats the assignment as building a **Voronoi partition** of the space by the centroids. Our references define a **Voronoi diagram of the color cube**; assigning pixels = point location in that diagram. This reframing is what motivates **precomputing the answer for every cell of a discretized cube** (our 6-bit LUT) — it is exactly an *inverse color map / quantized Voronoi label volume*. The ML view tells us how to make it **exact instead of approximate**: at LUT-build time, for each LUT cell, compute the nearest reference **for the cell's representative AND check whether the cell straddles a Voronoi boundary** (i.e., two references are within the cell's diagonal). Cells that don't straddle a boundary are *exactly* labeled for every point inside; only straddling cells need a fallback exact scan. This converts the LUT from "approximate everywhere" to "exact except in a small set of boundary cells," which is the single most important accuracy idea in this report (see §A).

- **Regime:** Hamerly bounds help at **medium/large P**. The exact-LUT-with-boundary-cells helps at **all P with huge images** (most pixels hit interior cells = O(1) exact).
- **Effort to port:** Hamerly bounds **low**; exact-boundary-LUT **medium** (need the straddle test and a fallback path).

---

## 3. Film / VFX / print color science — 3D LUT with tetrahedral interpolation

### The idea
This field's entire job is "map any input color to an output color, fast." A **3D LUT (CLUT)** stores outputs on a coarse grid (e.g. 17³, 33³, 65³); for an arbitrary input you **interpolate** between the 8 surrounding grid corners:

- **Trilinear interpolation:** weighted average of all **8 corners** of the enclosing cube. Simple, smooth, but can cross Voronoi/material boundaries and **blends across them** (introduces error near sharp transitions).
- **Tetrahedral interpolation:** split each cube into **6 tetrahedra**; **sort the fractional coordinates** (the ordering of r,g,b fractions selects which of the 6 tetrahedra you're in), then interpolate using only **4 corners** with **barycentric weights**. Fewer multiplies than trilinear, and **more accurate along the gray axis / near sharp boundaries** because it follows the diagonal. This is the industry-standard high-accuracy method. Sources: OpenColorIO `Lut3DOpCPU.cpp` (https://github.com/AcademySoftwareFoundation/OpenColorIO/blob/main/src/OpenColorIO/ops/lut3d/Lut3DOpCPU.cpp), lcms2 interpolation (`cmsintrp.c`, https://github.com/mm2/Little-CMS), Kang, *Computational Color Technology*; the `.cube` format spec.

### Real projects
- **OpenColorIO (OCIO)** — `Lut3DOpCPU` with both trilinear and tetrahedral, SIMD-optimized. ASWF project. https://opencolorio.org
- **littleCMS / lcms2** — `cmsintrp.c`, tetrahedral CLUT eval; the de-facto open ICC engine. https://github.com/mm2/Little-CMS
- **ArgyllCMS** — ICC profile building, inverse lookups. https://www.argyllcms.com
- **GraphicsMagick / ImageMagick** — `-hald-clut`. The **HALD CLUT** is a way to store a 3D LUT as a 2D image.

### Transfer to us — with an important caveat
The crucial distinction: **color-management LUTs interpolate a continuous output (a transformed color). We need a discrete *label* (which reference won).** **Interpolating labels is meaningless** — you cannot average "reference #3" and "reference #7." So **tetrahedral/trilinear interpolation does NOT directly cut our LUT's *labeling* error.**

What **does** transfer:

1. **If our tool's real output is a recolored image** (each pixel replaced by its reference's color), then we *can* interpolate the **output color**, and tetrahedral interpolation would give **smoother, more accurate output with a coarser grid** — but that defeats the purpose of palette quantization (we usually *want* hard snapping to palette entries, not blends). So: **only relevant if a "soft palette" / dithered output mode exists.** Likely **not** a fit. Be skeptical here.

2. **The tetrahedral *point-location* trick transfers cleanly** and is genuinely useful: the "sort the 3 fractional coordinates to pick 1 of 6 tetrahedra" is a fast, branch-light way to **locate a point within a cell** — exactly what cell-list ring expansion (§1) and the boundary-LUT (§2) need for the sub-cell decision. It's a known-good, SIMD-friendly primitive.

3. **The `.cube` / HALD-CLUT storage formats** are battle-tested layouts for a 3D label/output volume; if we ship a precomputed LUT we should mirror their memory layout (linear `idx = (b·S + g)·S + r`) for cache-friendly access. OCIO's SIMD lookup code is a reference for how to make the gather fast.

- **Regime:** soft-output mode only (likely N/A for hard quantization); point-location trick helps everywhere.
- **Exact or approximate:** interpolation is approximate by nature; point-location is exact.
- **Effort:** tetrahedral interpolation port **low** (well-documented ~40 lines), but **probably not worth it** for hard-label output — call this one out as an **attractive-but-non-transferring** idea for our core task.

---

## 4. Point cloud / 3D — voxel grids, kd-trees, nanoflann

### The idea
Point-cloud libraries solve **nearest-neighbor in exactly 3D** at scale — the closest-matching dimensionality to us in the whole survey.

- **Voxel-grid downsampling:** overlay a uniform 3D grid, collapse all points in a voxel to one — identical machinery to MD cell lists (§1) and to our LUT discretization. Confirms the uniform-grid approach is the field-standard 3D primitive.
- **kd-tree NN:** PCL's `pcl::KdTreeFLANN`, Open3D's `KDTreeFlann`, and especially **nanoflann** (a tiny header-only kd-tree, https://github.com/jlblancoc/nanoflann) are the go-to exact 3D NN structures. nanoflann is explicitly tuned for **low-dimensional** data and is the C++ community's default for 3D/2D NN.
- **Octree NN:** PCL `pcl::octree`, Open3D octree — hierarchical, good for adaptive density.

### Real projects
- **PCL (Point Cloud Library)** https://pointclouds.org , https://github.com/PointCloudLibrary/pcl
- **Open3D** https://github.com/isl-org/Open3D
- **nanoflann** https://github.com/jlblancoc/nanoflann (FLANN's low-dim kd-tree, header-only, no deps — easiest to study/port)

### Transfer to us
A **kd-tree over the P references** gives exact NN in O(log P) per query. But: **P is tiny (≤ few hundred)**. A kd-tree over a few hundred points has negligible advantage over a linear scan with early termination (the tree's log factor is swamped by constant overhead and cache misses), and a uniform grid (§1) is simpler and faster for this size. **kd-tree over the references: marginal, skip.**

The genuinely useful transfer is the **voxel-grid = our LUT** equivalence (reinforces §1/§2), and **nanoflann as the cleanest reference implementation** if we ever do want a kd-tree (e.g., if P grows into the thousands). The point-cloud field's strong preference for **uniform voxel grids in 3D** over fancier structures is itself evidence that, in 3D, **simple binning wins** — exactly our situation.

- **Regime:** kd-tree only if P → thousands; voxel-grid validates §1 at all P.
- **Exact or approximate:** both exact.
- **Effort:** kd-tree port medium; not recommended now.

---

## 5. Astronomy — kd-trees, ball trees, HEALPix

### The idea
Astronomers cross-match **billions of low-dimensional points** (RA/Dec on a sphere, ~2D, or 3D positions). Techniques:

- **scipy `cKDTree` / `KDTree`** — batched NN and fixed-radius queries over huge point sets; the workhorse. https://docs.scipy.org/doc/scipy/reference/spatial.html . `query_ball_tree`, `query` with `workers=-1` for multicore.
- **Ball trees** (scikit-learn `BallTree`) — better than kd-trees for non-Euclidean / higher-D metrics.
- **HEALPix** (Hierarchical Equal Area isoLatitude Pixelization) — tessellates the sphere into **equal-area hierarchical pixels**; cross-matching becomes "compare only objects in the same or adjacent HEALPix cells." This is **uniform spatial hashing on a sphere**, hierarchical. Projects: **healpy** (https://github.com/healpy/healpy), **astropy** (https://www.astropy.org), **TOPCAT/STILTS** (http://www.star.bris.ac.uk/~mbt/stilts/) whose cross-match is built on a sky-pixel hashing scheme + small per-cell scans.

### Transfer to us
The astronomy lesson is **methodological more than algorithmic**: when you have a *gigantic* query set against a *static* reference set, the winning pattern is **(coarse hierarchical/uniform pixelization) → (small bounded scan per cell)**. That is precisely cell lists (§1) again, and STILTS' cross-match is essentially the same uniform-grid-bin-then-scan we recommend. **HEALPix's equal-area property doesn't matter to us** (our color cube is flat Euclidean, not a sphere; equal-area is solving a problem we don't have). So HEALPix specifically is an **attractive-but-non-transferring** idea — but it *re-confirms* the uniform-grid + per-cell-scan pattern from a third independent field.

The most directly portable astronomy artifact is **scipy `cKDTree.query` with `workers=-1`** as a **reference oracle for correctness/benchmarking** (run it in Python on the same data to validate our Go exact results), not as production code.

- **Regime:** validates §1; cKDTree useful as test oracle.
- **Exact or approximate:** exact.
- **Effort:** HEALPix N/A; cKDTree oracle trivial (Python harness).

---

## 6. Vector databases / ANN — IVF, PQ, HNSW (mostly does NOT transfer — be honest)

### The idea
- **IVF (inverted file):** a **coarse quantizer** (k-means with ~√N centroids) partitions space; query probes the nearest few cells, then exact-scans the shortlist. https://github.com/facebookresearch/faiss/wiki
- **Product Quantization (PQ):** split a high-D vector into subspaces, quantize each, store compact codes; approximate distances via lookup tables.
- **HNSW:** navigable small-world graph for log-ish ANN. https://github.com/nmslib/hnswlib , also pure-Go ports exist.
- **Annoy** (random projection trees, https://github.com/spotify/annoy), **ScaNN**, **Milvus**.

### Honest assessment — mostly NEGATIVE transfer
These are engineered for **D = 100..1500** where exact NN suffers the curse of dimensionality. **At D = 3, the curse does not apply**, and exact methods (grid, kd-tree, even smart linear scan) beat all of them.

**This is not my opinion — it is FAISS's own documented position.** The FAISS wiki states plainly that **indexing low-dimensional data "is not addressed well in Faiss because these cases are better addressed with tree-based structures like kd-trees: they offer exact search results at logarithmic search time," and that "tree-based methods do not scale well for dimensions above 10"** (FAISS wiki / FAQ, https://github.com/facebookresearch/faiss/wiki/FAQ). FLANN documents the same crossover: `KDTreeSingleIndex` (exact) "is efficient for low dimensional data," while the approximate `KDTreeIndex` is for high dimensions. The premier ANN library itself tells us to **not** use ANN at our dimensionality. Concretely:

- **PQ** decomposes high-D vectors into subspaces — **pointless at D = 3** (you'd have 1–3 trivial subspaces); pure overhead and approximation error for nothing.
- **HNSW / Annoy** graphs/trees pay large constant and memory overheads that only amortize in high D; in 3D a uniform grid is both **exact and faster**. hnswlib and FLANN/Annoy documentation and the broader literature consistently note that **for low dimensions, exact kd-trees/grids outperform approximate ANN**. Building an HNSW over a few hundred references is absurd overkill.
- **IVF's two-level "coarse quantize then shortlist"** is the *one* idea here with a faint echo of value — but it is just **cell lists (§1) under another name**, and at our P it collapses to "one cell = a handful of references."

**Conclusion: skip the entire ANN stack.** Its presence on the task list is a trap; its tricks are tuned for the opposite regime (huge static DB, few high-D queries; we have a tiny low-D DB and a huge query stream). The only transferable kernel — coarse-bin-then-scan — we already get more cleanly from MD cell lists.

- **Regime:** none for us.
- **Effort:** N/A (do not port).

---

## 7. Game physics / real-time — spatial hashing, uniform grids, BVH, sweep-and-prune

### The idea
Real-time broad-phase collision detection must find "what's near what" every frame at 60+ FPS:

- **Uniform grid / spatial hashing:** bucket objects into a grid (or a hash of grid coords for unbounded space); check only same/adjacent buckets. Identical to MD cell lists. The classic ref: Teschner et al., "Optimized Spatial Hashing for Collision Detection of Deformable Objects" (2003).
- **Sweep-and-prune (SAP):** sort AABB endpoints along axes, sweep to find overlaps — exploits temporal coherence (sorted order barely changes frame-to-frame).
- **BVH (bounding volume hierarchy):** tree of bounding boxes; the standard for ray tracing and broad-phase. Projects: **Bullet** (https://github.com/bulletphysics/bullet3), **Box2D**, **Jolt** (https://github.com/jrouwe/JoltPhysics).

### Transfer to us
- **Spatial hashing** is the same uniform-grid transfer as §1 — its game-dev framing emphasizes **cache-friendly flat arrays and integer cell hashing**, which is good engineering guidance for the Go grid implementation (store references in a flat `[]int` with per-cell offset table, CSR-style, rather than slices-of-slices — avoids pointer chasing and GC pressure, important for "bounded memory").
- **Sweep-and-prune's temporal-coherence insight** is the *one* novel idea here: adjacent pixels in an image are **highly coherent** (similar colors), like objects that barely move between frames. We can exploit this: **cache the last query's nearest reference and its distance; for the next (adjacent, similar) pixel, test that reference first** as the Hamerly upper bound (§2). With image-scanline coherence this makes the per-pixel scan terminate almost immediately. This is essentially **temporal coherence applied to spatial coherence in the pixel stream** — cheap, exact, and a real win.
- **BVH:** over a few hundred static references, no advantage over a grid; skip.

- **Regime:** spatial hashing helps medium/large P; coherence caching helps **all P, all image sizes** and is nearly free.
- **Exact or approximate:** exact.
- **Effort:** coherence cache **trivial** (a few lines in the per-pixel loop); CSR grid layout **low**.

---

## 8. GIS / geospatial — R-trees, S2, H3, geohash

### The idea
- **R-tree:** hierarchical bounding-rectangle index for spatial range/NN queries on disk-resident data. PostGIS (GiST), libspatialindex.
- **Google S2** (https://github.com/google/s2geometry) — projects the sphere to a cube, **Hilbert-curve-orders** cells into 64-bit IDs; hierarchical, locality-preserving.
- **Uber H3** (https://github.com/uber/h3) — **hexagonal** hierarchical index; hexagons have uniform adjacency (every neighbor equidistant), nice for "k-ring" neighborhood queries.
- **Geohash** — interleave lat/lon bits into a string prefix; **bit-interleaving is exactly what ImageMagick's color tree does** (Z-order / Morton curve).

### Transfer to us
- **R-trees** target overlapping extents and disk pages — **not** our case (points, in-RAM, tiny set). Skip.
- **S2 / Hilbert and geohash / Morton (Z-order) curves** are genuinely relevant as **the linearization trick**: mapping a 3D cell coordinate to a **single integer via bit-interleaving (Morton)** turns the LUT/grid index into one cache-friendly array offset and makes "cells near in space ≈ near in memory," improving cache hit rate for the coherent pixel stream. This is **the same Z-order idea ImageMagick already uses** in its color tree (per the reverse-engineering note) — so it's a *confirmed-in-our-own-domain* technique, independently validated by GIS. Hilbert order (S2) has even better locality than Morton but is costlier to compute; Morton is the pragmatic choice.
- **H3's hexagons** are a 2D-sphere optimization (uniform neighbor distance) that **does not generalize to a 3D Euclidean cube** (no space-filling regular hex honeycomb in 3D). Attractive-but-non-transferring; the "k-ring expanding neighborhood query" *concept*, however, is just the cell-list ring expansion of §1 by another name.

- **Regime:** Morton ordering helps cache behavior at **all P / huge images**.
- **Exact or approximate:** exact (it's just memory layout).
- **Effort:** Morton encode/decode **trivial** (bit tricks); reordering the LUT array **low**.

---

## 9. GPU / graphics — texture sampling, and the CPU-SIMD analogue in Go

### The idea
GPUs do **hardware 3D-texture sampling**: a `texture3D` lookup with `GL_LINEAR` does **trilinear interpolation across 8 texels in ~1 cycle** via dedicated texture units. Color grading (DaVinci Resolve, OBS LUT filter, every game's color-grade pass) loads a 3D LUT as a 3D texture and samples it per pixel — this is the **fastest known way to apply a 3D LUT**, the GPU analogue of our 6-bit LUT. Refs: NVIDIA GPU Gems 2 ch. on color, OBS `lut_filter`, any `texture(lut3d, color)` shader.

### Transfer to us — what's real vs. wishful
- **Pure-Go GPU compute is not realistic** for a "pure Go, commodity CPU" tool (no portable Go GPGPU; cgo/Vulkan breaks the pure-Go constraint). So **direct GPU transfer: no.**
- **The CPU-SIMD analogue is real and useful.** Go does not expose intrinsics in the language, but:
  - `math/bits` and the compiler autovectorize simple loops poorly; for real SIMD you write **Go assembly** (`.s` files) or generate it with **avo** (https://github.com/mmcloughlin/avo). Libraries like **MinIO's simd-checksum**, **gonum**, and **klauspost/cpuid** + hand-written AVX2 kernels prove production pure-ish-Go SIMD is viable (assembly is still "Go toolchain," no cgo).
  - A **SIMD distance kernel** computing the squared Euclidean distance from one query to **8 references at once** (AVX2, 8×float32) would accelerate the linear scan / cell-scan ~4–8×, **exact**.
  - The **LUT gather** (look up 8 corners) maps to AVX2 `VGATHER` — but gather is slow on many microarchitectures; for a hard-label LUT we only need **one** lookup per pixel (no interpolation), so a plain indexed load is fine and SIMD's role is mainly **batching many pixels' index computation**.
- **The most practical GPU-derived idea:** treat the per-pixel work as **data-parallel SoA (structure-of-arrays)** — process R, G, B planes as separate `[]float32`/`[]uint8` slices so the inner loop is a tight vectorizable kernel. This "think like a shader" SoA restructuring helps the Go compiler and any hand SIMD, and parallelizes trivially across goroutines (one per scanline block) for the multicore requirement.

- **Regime:** SIMD distance kernel helps **medium/large P**; SoA + goroutine tiling helps **all** (it's how we hit "fast on multicore" + "bounded memory" by streaming tiles).
- **Exact or approximate:** exact.
- **Effort:** SoA + goroutine tiling **low**; hand-written AVX2 via avo **medium-high** (and not pure-source-Go, though still cgo-free).

---

## A. Ranked shortlist — the 4–6 most promising transfers

Ranked by expected payoff × low effort × fit to our exact-vs-approximate and pure-Go constraints.

| # | Technique | Source field / project | Helps regime | Exact? | Effort |
|---|-----------|------------------------|--------------|--------|--------|
| **1** | **Exact "boundary-aware" inverse LUT** (interior cells = O(1) exact label, only Voronoi-straddling cells fall back to scan) | ML/k-means Voronoi view (§2) + color-science CLUT layout (§3) | all P, **huge images** | **EXACT** | medium |
| **2** | **Uniform cell-list grid over references + exact ring expansion** | Molecular dynamics (GROMACS/LAMMPS, §1); reconfirmed by point clouds (§4), astronomy (§5), game physics (§7) | **medium/large P** | EXACT | low-med |
| **3** | **Coherence-cached Hamerly bounds** (last pixel's reference as upper bound; pairwise-reference table to prune) | Hamerly k-means (§2) + sweep-and-prune temporal coherence (§7) | all P, especially **medium/large P** | EXACT | **low** |
| **4** | **SoA + goroutine-tiled streaming + optional AVX2 distance kernel** | GPU "think-like-a-shader" (§9) + avo/Go-asm | all; satisfies multicore + bounded memory | EXACT | low (SoA) / med (SIMD) |
| **5** | **Morton (Z-order) cell linearization** for cache-friendly LUT/grid layout | GIS S2/geohash (§8); already used by ImageMagick | all P, huge images | EXACT | low |
| 6 | (Conditional) **Tetrahedral interpolation** — ONLY if a soft/dithered palette-blend output mode is wanted | OpenColorIO/lcms2 (§3) | soft-output only | approximate | low |

### The single best "not directly related" idea — highlighted

> **Exploit Voronoi-boundary structure to make the LUT EXACT (transfer #1), built from the molecular-dynamics cell-list view of space.**

**Why it's the best, and why it's "not directly related":** Image quantization folklore treats a 3D LUT as inherently approximate (the blind prototype accepted ~150–200× speed for accuracy loss). The **k-means / computational-geometry** literature reframes our references as a **Voronoi diagram** and the LUT as a **discretized label volume of that diagram**. The decisive insight, borrowed from *outside* image processing, is: **a LUT cell is wrong only if a Voronoi boundary passes through it.** For P references in a 3D cube with a fine-enough grid, the *vast majority* of cells lie entirely inside one Voronoi region and are therefore **provably exact** — they can be labeled once at build time and trusted for every pixel inside. Only the thin shell of **boundary-straddling cells** (detectable at build time: a cell straddles iff ≥2 references are within the cell's diagonal of the cell, a cheap test borrowed directly from the MD cell-list "check 27 neighbors within cutoff" logic) needs a fallback exact scan or sub-cell tetrahedral point-location (§3).

This **fuses three foreign fields** — MD cell sizing (§1), k-means Voronoi/triangle-inequality reasoning (§2), and color-science CLUT layout + tetrahedral point-location (§3) — into something neither field ships on its own: a **LUT that is as fast as the approximate prototype on ~95%+ of pixels yet returns the same answer as the linear scan**. It directly attacks the project's central tension (the 150–200× LUT being approximate) and turns "fast OR exact" into "fast AND exact almost everywhere, with a tunable exact-everywhere guarantee by shrinking cells in boundary regions." Nothing in image-processing literature framed it this way; the framing is entirely imported.

**Skeptic's honest caveats on the shortlist:**
- At very small P (4–16), **plain linear scan with the coherence cache (#3) may already beat everything**; the grid/LUT machinery's build cost and memory may not pay off. Measure before adding complexity.
- The boundary-cell fraction grows with P and shrinks with grid resolution; there's a real **memory vs. exactness vs. build-time** tradeoff to characterize empirically. With a 6-bit (262k-cell) grid and a few hundred references, boundary cells could be a non-trivial fraction — needs measurement.
- SIMD (#4) is **not pure-source Go** (needs `.s`/avo); keep it optional behind a build tag so the pure-Go fallback always exists.

**Explicitly-rejected attractive-but-non-transferring ideas:** HNSW/PQ/IVF/Annoy and the whole ANN stack (§6, wrong dimensionality); HEALPix equal-area pixelization (§5, we're flat not spherical); H3 hexagons (§8, no 3D honeycomb); R-trees (§8, disk/extent-oriented); kd-tree/ball-tree/BVH over the references (§4,§7, P too small to beat a grid); and tetrahedral *output-color* interpolation for hard-label quantization (§3, you can't average labels).

---

## B. Autoresearch methodology — mining fields and validating a borrowed technique

A repeatable pipeline that plugs into the existing agent-orchestration pattern (one agent per discipline/track, writing numbered reports into `.plans/research/`, a synthesis agent ranking them, a prototype agent validating).

### Phase 0 — Frame the problem domain-free (done once, reused)
Maintain a **canonical problem statement** (the §0 of this report): dimensionality, P range, N range, batch structure, exact-vs-approx tolerance, language/memory constraints. Every mining agent gets this verbatim so techniques are judged against the *real* regime, not the source field's regime. **This is the firewall against the §6 trap** (importing high-D tricks).

### Phase 1 — Fan-out mining (per discipline, parallelizable agents)
For each field, an agent runs a fixed checklist:
1. **Identify the canonical algorithm** and its **complexity** as a function of N, P, D.
2. **Name 2–3 real OSS projects** that prove it works, with **URLs to the actual source file** implementing the kernel (not just the homepage).
3. **State the source field's regime** (their N, P, D) and **explicitly diff it against ours.** A technique only advances if the diff is favorable or neutral on the dimensionality and DB-size axes.
4. Classify: **exact / approximate**, **build cost**, **query cost**, **memory**, **pure-Go portability**.
5. Flag **negative transfers** loudly (the report must list what *doesn't* work and why — this is as valuable as positives).

Output: one numbered markdown file per field, uniform schema, so the synthesis agent can rank mechanically.

### Phase 2 — Synthesis & ranking
A synthesis agent merges the per-field reports into a single table keyed by the **underlying technique** (deduplicating: "uniform grid" appears in §1/§4/§5/§7/§8 — collapse to one row with five independent confirmations, which *raises* confidence). Rank by `payoff × (1/effort) × fit`. Produce the shortlist (§A). **Convergence across unrelated fields is the strongest signal** — when MD, point clouds, astronomy, and game physics all independently arrive at "uniform grid + small per-cell scan," that technique is robust.

### Phase 3 — Prototype in `/tmp` (one agent per shortlisted technique)
For each top technique, a prototype agent:
1. Creates `/tmp/pixelize-proto-<technique>/` with a **minimal standalone Go program** (no dependency on the main tree) implementing *just* the kernel.
2. Generates **representative inputs**: synthetic palettes at P ∈ {4, 16, 64, 256}; query sets at N ∈ {1e6, 8e6, 33e6}; plus at least one **real 8K image's pixel stream** to capture spatial coherence (synthetic random pixels would unfairly penalize coherence-exploiting methods like #3).
3. Implements the **linear-scan baseline** in the same harness as the ground-truth oracle, and optionally a **scipy `cKDTree`** Python oracle (§5) to cross-check correctness independently.

### Phase 4 — Benchmark (what to measure)
For each (technique × P × N):
- **Throughput:** pixels/second (Go `testing.B` with `b.ResetTimer()` after index build; report build time separately since it amortizes over the image).
- **Memory:** peak RSS and allocations (`-benchmem`, `runtime.MemStats`) — must verify "bounded memory for huge inputs" by streaming tiles, not loading 33M results at once.
- **Correctness (the exact-vs-approx judgment):**
  - **% of pixels with identical label** vs. the linear-scan oracle (must be 100% for anything claimed "exact").
  - For approximate methods: **distribution of the distance error** (query-to-assigned vs. query-to-true-nearest), max and 99.9th percentile, and **fraction of pixels assigned a wrong-but-equidistant-tie** (acceptable) vs. **genuinely-farther** (counts as error).
  - **Visual delta** (mean ΔE in a perceptual color space) for the recolored output, since label errors near Voronoi boundaries are often perceptually invisible — this is the *right* metric for whether an approximation is acceptable in practice.
- **Scaling curves:** plot throughput vs. P and vs. N to find **crossover points** (e.g., "grid beats linear scan above P ≈ 32") — these crossovers become **runtime strategy-selection thresholds** in the shipped tool.

### Phase 5 — Judge & decide
A technique is **accepted** if, in its target regime, it gives a **meaningful speedup over the current baseline** (target: within striking distance of the 150–200× LUT) **while either being provably exact or having a perceptual error below a fixed ΔE threshold** the project sets (e.g., max ΔE < 1.0, invisible). Record the **crossover thresholds** so the tool can pick the strategy per (P, N) automatically (small P → coherence-cached scan; medium P → grid; all P + huge image → boundary-aware exact LUT).

### Phase 6 — Feed back / iterate
- Accepted prototypes graduate from `/tmp` into the main tree behind interfaces + the strategy selector; the benchmark harness becomes a **regression guard** in CI.
- Rejected techniques are documented (with *why*) so future mining agents don't re-litigate them.
- New disciplines discovered mid-mining (e.g., this survey could add **computational geometry's Voronoi/Fortune's-algorithm and Delaunay** literature, and **database spatial-join** literature) spawn new Phase-1 agents — the loop is open-ended.

### How it plugs into the existing agent pattern
This mirrors what the project already does (numbered per-track research files in `.plans/research/`): **Phase 1 = the parallel research agents** (this very report is one), **Phase 2 = a synthesis/ranking agent**, **Phase 3–5 = prototype agents writing to `/tmp` and benchmark agents**, **Phase 6 = an integration agent + CI**. The only added discipline is the **uniform per-field schema** (regime-diff, exact/approx, pure-Go portability) and the **shared oracle + benchmark harness**, which make outputs mechanically comparable and turn "interesting idea" into "validated, threshold-gated improvement."

### What success looks like
A shipped tool that, for any (P, N), **auto-selects** among {coherence-cached exact scan, exact cell-list grid, boundary-aware exact LUT}, matches the linear-scan baseline's labels **exactly** (or within a fixed invisible ΔE if an approximate fast path is explicitly opted into), and reaches a large fraction of the 150–200× LUT speedup **without** giving up correctness — with every threshold backed by a reproducible benchmark in the harness.

---

## Sources (selected, primary)
- Cell lists / linked-cell: https://en.wikipedia.org/wiki/Cell_lists ; LAMMPS neighbor docs https://docs.lammps.org/Developer_par_neigh.html ; GROMACS https://gitlab.com/gromacs/gromacs ; OpenMM https://github.com/openmm/openmm
- k-means acceleration: G. Hamerly, "Making k-means even faster," SDM 2010; reference impl https://github.com/ghamerly/fast-kmeans ; scikit-learn https://scikit-learn.org/stable/modules/clustering.html#k-means ; faiss clustering https://github.com/facebookresearch/faiss/wiki/Faiss-building-blocks:-clustering,-PCA,-quantization
- Color science LUTs: OpenColorIO `Lut3DOpCPU.cpp` https://github.com/AcademySoftwareFoundation/OpenColorIO ; lcms2 https://github.com/mm2/Little-CMS ; ArgyllCMS https://www.argyllcms.com
- Point clouds: nanoflann https://github.com/jlblancoc/nanoflann ; PCL https://github.com/PointCloudLibrary/pcl ; Open3D https://github.com/isl-org/Open3D
- Astronomy: scipy spatial https://docs.scipy.org/doc/scipy/reference/spatial.html ; healpy https://github.com/healpy/healpy ; STILTS http://www.star.bris.ac.uk/~mbt/stilts/
- ANN (assessed, mostly rejected): faiss https://github.com/facebookresearch/faiss ; hnswlib https://github.com/nmslib/hnswlib ; Annoy https://github.com/spotify/annoy
- Game physics: Teschner et al. 2003 (spatial hashing); Bullet https://github.com/bulletphysics/bullet3 ; Jolt https://github.com/jrouwe/JoltPhysics
- GIS: Google S2 https://github.com/google/s2geometry ; Uber H3 https://github.com/uber/h3
- Go SIMD: avo https://github.com/mmcloughlin/avo ; klauspost/cpuid https://github.com/klauspost/cpuid
