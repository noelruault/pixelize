# bench/history

Benchmark results, stored alongside the commits that produced them.

## Why this exists

Performance is a feature of this project, and the algorithms that drive it are
changing. To keep the numbers honest over time, every commit that touches the
matcher or quantization code records a benchmark run here. The history is the
record of how speed and correctness moved, commit by commit, and why.

## The rule

A commit that changes the nearest-color matcher or quantization algorithms must:

1. Run the benchmark suite (`bench/compare.sh`) and the exactness check before
   committing.
2. Save the output to a file in this folder.
3. Reference that file, and a one line "what and why", in the commit message.

A commit that does not touch those algorithms does not need a new entry.

## File naming

```
bench/history/YYYY-MM-DD-<short-sha>-<slug>.txt
```

The `<short-sha>` is filled in after the commit exists. Run the benchmark, commit
the code, then rename the result file to include the new short SHA and commit the
history file in a follow up, or save it with a placeholder and amend. Either way
the file must end up named for the commit it measured.

## Required header

Each result file begins with the header `bench/compare.sh` prints: the commit SHA
(short and full), the commit subject, whether the tree was clean or dirty, the
machine, cores, CPU, tool versions, and the date. A dirty tree means the numbers
do not correspond to a committed state and the entry is provisional.

## Optional narrative

After the header, a file may add a short "what changed and why" note: what the
iteration tried, what moved in the numbers, and whether it met the gate. This is
the story of the change, not just its measurement.

## Regression gates

- No path documented as exact may ever produce a non-nearest pixel. The exactness
  check must report 0% non-nearest for exact matchers, verified against a brute
  force scan.
- No performance regression beyond run to run noise versus the previous history
  entry, unless the commit message states the tradeoff and why it is worth it.
- Approximate paths (for example the LUT) must stay inside their stated accuracy
  budget, expressed as percent of pixels differing and maximum color error.
