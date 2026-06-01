# Reverse-engineering scratch — ImageMagick quantize internals

This directory is the **raw working material** behind report
`01-imagemagick-reverse-engineering.md`. It is preserved verbatim so the analysis
can be re-derived from the same evidence it was built on. It is research scratch,
not part of pixelize: nothing here is compiled, imported, or shipped.

## ⚠️ Third-party source — provenance and license

The files prefixed `im-` are **verbatim extracts of ImageMagick's own source
code**, copied during the session from the build that was present on the box:

```
ImageMagick 6.9.12-98  —  MagickCore/quantize.c
(unpacked at /tmp/ImageMagick6-6.9.12-98 during the session)
```

They are **© ImageMagick Studio LLC**, licensed under the **ImageMagick License**
(an Apache-2.0–style license): https://imagemagick.org/script/license.php

They are reproduced here **solely as the analyzed subject** of the
reverse-engineering write-up — to document exactly which code paths the report's
conclusions came from. They are **not pixelize code**, are not built into the
pixelize binary, and carry their own upstream license, not pixelize's. If this
material is moved to `noelruault/research` (see `../MIGRATION.md`), keep this
attribution with it.

## What each file is

Verbatim ImageMagick 6.9.12-98 `MagickCore/quantize.c` extracts:

| file | what it is |
|------|------------|
| `im-quantize-ClosestColor.c.txt` | the `ClosestColor()` leaf search — the actual nearest-color tie-break and squared-distance logic the exact-match reproduction had to match |
| `im-quantize-tree-descent.c.txt` | the octree descent loop (`ColorToNodeId`, `MaxTreeDepth` walk) used during remap |
| `im-quantize-QuantizeImages.c.txt` | the top-level `QuantizeImages()` driver (full function) |
| `im-quantize-CubeInfo-NodeInfo-structs.c.txt` | the `CubeInfo` / `NodeInfo` struct definitions (the octree node layout) |
| `im-quantize-static-declarations.c.txt` | the file's static function declarations (the call graph at a glance) |
| `im-quantize-annotated-extract.c.txt` | a line-numbered extract spanning the structs + key functions, used for cross-referencing |

My own reverse-engineering notes (not IM source):

| file | what it is |
|------|------------|
| `notes-grep-landmark-line-numbers.txt` | grep hits (line numbers) for the key symbols, the map I navigated `quantize.c` by |
| `notes-quantize-landmark-functions.txt` | the landmark function list (`ClassifyImageColors`, `AssignImageColors`, …) marking the remap pipeline stages |

## Why the rest of the scratch is not here

Other intermediate `/tmp` artifacts from the session were checked and deliberately
omitted because they are already fully captured elsewhere, byte-for-byte:

- the report-03 / report-04 drafts and benchmark CSVs → already in
  `03-experiments-data.txt`, `04-informed-challenger.md`,
  `04-informed-challenger-data.txt`;
- the Phase-3 variance harness + raw output → embedded in
  `12-phase3-enhancement-variances-data.txt`;
- trivial shell scratch (liveness probes, path lists, byte counts) — no research
  content.

So this directory plus the numbered reports `01`–`12` (each with its `*-data.txt`)
are the complete reverse-engineering and experimental record.
