# Fan-out, Measure, Keep-or-Discard

A reusable pattern for **research-driven engineering**: making a quality or
performance decision by *measuring many candidates against a fixed bar* instead of
arguing about them. Portable across domains — it was used to build pixelize's
nearest-color matcher and its `quantize` palette generator, but nothing here is
specific to images.

> **Intended home:** this is a general method. It lives in pixelize for now; move it
> to your patterns/skills library (e.g. `.aitelier`).

## TL;DR — the loop

1. **Frame** the question as a single number to optimize (the *metric*), and make
   that metric trustworthy.
2. **Fix** a baseline (the floor to beat) and a corpus (the fixed test set).
3. **Decompose** the problem into independent *pieces* (steps you can vary one at a
   time).
4. **Fan out**: for each piece, enumerate many candidate implementations — the
   popular ones *and* ideas transferred from other disciplines.
5. **Measure** each candidate in isolation against the baseline, on the corpus, with
   the metric.
6. **Keep or discard — with a number.** Record every result, including (especially)
   the losers, with the measured reason.
7. **Stack** the winners and re-measure the *integration* (interactions hide here).
8. **Validate at scale**, then **ship the winner** and keep the record.

The output is not "a solution." It is *the best solution assembled from the best
piece at each step*, plus a durable record of why every other option lost.

## When to use it (and when not)

**Use it** when: the space of approaches is large; "obviously best" is contested or
intuition-driven; the cost of the wrong choice compounds; or you'll need to defend
the decision later. Quantization, compression, ranking, retrieval, caching, codec
selection, prompt/agent design — anywhere candidates can be scored.

**Don't** when: there's one obvious approach and the metric is trivial; or you can't
define a trustworthy metric (fix that first — see below — or this pattern can't help).

## The non-negotiable principles

These are what separate this from "we tried some stuff."

1. **A trustworthy metric, self-tested.** Before measuring anything, prove the metric
   is correct (golden values, reference vectors, a known-answer test). A benchmark on
   a wrong metric is worse than no benchmark — it launders a wrong conclusion. *(We
   self-tested CIEDE2000 against the Sharma reference pairs before trusting a single
   number.)*
2. **A fixed baseline and a fixed corpus.** Every candidate is measured against the
   same floor on the same inputs. Changing either mid-stream invalidates comparisons.
3. **Discards are first-class results.** A measured "no, because X (number)" is a
   permanent asset — it stops the team relitigating it. Write the loser down *with its
   number and its reason*, not just the winner.
4. **A claim ships only with a measured delta.** "Should be faster / better" is a
   hypothesis to test, never a conclusion. No number, no claim.
5. **Determinism / reproducibility.** Same inputs → same result, or your benchmark is
   measuring noise. If a step is randomized, pin the seed *and* the input order. *(We
   discovered seeded k-means drifted run-to-run purely from map-iteration order, and
   fixed it with a canonical sort — a determinism bug the benchmark surfaced.)*
6. **Honest calibration.** State the result at the strength the evidence supports.
   "Matches/edges the incumbent, clearly beats the weaker one" earns more trust than
   "destroys everything," and survives scrutiny. Under-claim before you over-claim.
7. **Separate the objective from the proxy.** Know what you're *really* optimizing
   vs the cheap thing you measure. A trick that improves the proxy can hurt the true
   goal. *(Importance-weighting lowered perceived error but raised our unweighted
   metric — correct for a different objective, so we scoped it out, not adopted it.)*

## Roles

The loop runs cleanly when three roles are distinct:

- **The harness** — owns the metric (self-tested), the corpus, the baseline, and the
  runner that prints reproducible numbers (with machine, versions, date, config). It
  is independent of the product code so its numbers transfer.
- **Fan-out workers** — explore breadth in parallel. Each takes one angle (one piece,
  or one family of candidates) and reports findings + sources. Parallelism is how you
  cover "popular *and* cross-disciplinary" without serializing weeks of reading.
- **The judge** — sees *all* results, winners and losers together in one place, and
  makes the keep/discard/stack call. The judge needs the discards too: ruling things
  out is half the decision.

## Where the good ideas come from: deliberate cross-disciplinary fan-out

The highest-leverage move is to **raid other fields on purpose.** The same abstract
problem ("reduce N things to K representatives," "find nearest," "allocate budget")
recurs under different names across disciplines, and a transfer often beats the
in-field canon. Make a literal list — *"what does crypto / astrophysics / statistical
mechanics / cartography / finance do with this shape of problem?"* — and test the
plausible ones. Most will be discards (record them), but the occasional transfer is
where the real edge comes from. *(A space-filling-curve initializer from databases
gave our best large-palette result; the canon hadn't tried it.)*

## Anti-patterns it kills

- **HiPPO** (highest-paid-person's-opinion) decisions — replaced by a number.
- **Cargo-culting the incumbent** — you measure *which* of its tricks actually help,
  and find the ones that don't.
- **Survivorship docs** — recording only what shipped, so the team re-tries dead ends.
- **Benchmark theater** — a number with no self-tested metric, no fixed baseline, or
  no reproducibility behind it.
- **Over-claiming** — the credibility tax that comes due under scrutiny.

## Artifacts: the record outlives the build

Structure the work so it's re-derivable:

- **Numbered reports** (`00`, `01`, …), each paired with a **raw-data companion**
  (`NN-data.txt`) containing the verbatim runner output.
- A **methodology/overview** doc up front so every later number has a home, and a
  **judged synthesis** that stacks the winners.
- A **status table** (done / partial / pending) per phase so the live state is legible.
- The record lives *next to the evidence*, separate from the shipped code: the binary
  imports none of it; planning that drives the build stays with the build.

## Copy-paste checklist

```
[ ] Metric defined and SELF-TESTED (golden/reference values pass)
[ ] Baseline (floor) chosen and measured
[ ] Corpus fixed (and representative of the real regime that matters)
[ ] Problem decomposed into independent pieces
[ ] Per piece: candidates enumerated — popular AND cross-disciplinary
[ ] Each candidate implemented as a benchmarkable unit
[ ] Each measured in isolation vs baseline; result recorded (win OR discard + reason)
[ ] Determinism verified (pin seeds AND input order)
[ ] Winners stacked; INTEGRATION re-benchmarked for interactions
[ ] Validated at scale (bigger/standard corpus)
[ ] Claim calibrated to the evidence; caveats stated
[ ] Record published: numbered reports + raw data + judged synthesis
```

## Worked example (illustration)

Deriving an N-color palette from an image. *Metric:* mean CIEDE2000 (self-tested vs
Sharma pairs). *Baseline:* median cut. *Corpus:* six images, then the 18-image Kodak
set. *Pieces:* color space, histogram, selection, seeding, refinement, assignment.
*Fan-out:* median-cut/Wu/octree/k-means, plus transfers — vector quantization,
cartography (Jenks/1-D DP), product quantization, deterministic annealing
(stat-mech), Friends-of-Friends (astrophysics), space-filling curves (crypto/DBs),
HyAB/OKLab (color science). *Result:* the winner was "cluster *and* assign in OKLab,
space chosen by palette size"; a space-filling-curve initializer added the best
large-palette result. *Discards (kept, with numbers):* maximin seeding, PNN,
multi-restart, HyAB, deterministic annealing, MST/Friends-of-Friends, and
importance-weighting (wrong objective). *Calibration:* "beats ImageMagick decisively,
matches/edges libimagequant" — not a rout, and said so. Full record:
`noelruault/research/quantization`.

---

*Provenance: distilled from the pixelize research records
(`nearest-color-scaling`, `quantization`) in `noelruault/research`, 2026.*
