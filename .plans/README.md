# .plans

Planning and design artifacts for pixelize — design notes, judged syntheses, and
phased execution plans. **Documentation, not code:** nothing here is compiled,
imported, or run by pixelize. Files are written before or alongside work to think a
problem through and record what was decided.

Each major investigation has its own folder, mirroring its research record in
[`noelruault/research`](https://github.com/noelruault/research).

## [`nearest-color-scaling/`](nearest-color-scaling/) — the palette matcher

Mapping every pixel to its nearest palette color, as fast as possible, without
giving up exactness (the kd-tree branch-and-bound, parallel scan, run-length
collapse, and 6-bit fast-mode LUT that ship in pixelize).

- [`00-overview.md`](nearest-color-scaling/00-overview.md) — the judged synthesis:
  verdict per regime, why exact beats ImageMagick's approximate remap.
- [`01-execution-plan.md`](nearest-color-scaling/01-execution-plan.md) — the phased
  build plan.
- [`MOVED.md`](nearest-color-scaling/MOVED.md) — breadcrumb: the raw research record
  (reports 01–12 + data) was extracted to
  [`noelruault/research/nearest-color-scaling`](https://github.com/noelruault/research/tree/main/nearest-color-scaling).

## [`quantize/`](quantize/) — deriving a palette from an image

Workflow B ("turn any image into N colors"). **Shipped** as the
[`quantize`](../quantize) package and `-palette auto:N`.

- [`00-execution-plan.md`](quantize/00-execution-plan.md) — the phased,
  benchmark-gated plan with its Definition of Done and a live status table.
- Research record:
  [`noelruault/research/quantization`](https://github.com/noelruault/research/tree/main/quantization).

## [`EVALUATION-RUBRIC.md`](EVALUATION-RUBRIC.md) — the shared scoring rule

The standing rubric every matcher/quantizer recommendation is judged against
(wall-time is the star; resources are spendable but the trade must be efficient;
correctness on the default path is non-negotiable). Referenced by both plans above.

---

The generalized method behind these — *fan-out, measure, keep-or-discard-with-a-
number* — is written up portably in [`../docs/methodology.md`](../docs/methodology.md).
