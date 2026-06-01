# Evaluation rubric (standing, for every research and judge agent)

Read this before evaluating or recommending any matcher, enhancement, or
algorithm variant for pixelize. It is the scoring rule. It does not change per
task.

## The star: wall-clock time

Minimize per-image elapsed (wall-clock) time. That is the primary quantity every
recommendation optimizes. Report it as best-of-N on the quietest box available,
and state the run-to-run noise band.

## CPU and RAM are spendable, but the trade must be efficient

CPU and RAM may be traded for wall-time. They are not free in unlimited amounts.

**Efficiency gate.** Prefer a faster-but-heavier variant over the current best
only when:

```
proportional wall-time reduction  >=  proportional resource-cost increase
```

- +20% CPU work for only -10% time -> REJECT (cost 20 > gain 10).
- Faster and not heavier -> always take it.
- Equal time -> take the leaner one (less CPU work, less RAM).

**"CPU cost" = total CPU work (core-seconds), not utilization.** Spending more of
the cores you already have via parallelism keeps total work roughly flat while
wall time drops, so **parallelism always passes the gate**. The gate bites only
when a variant does genuinely more total work, makes redundant passes, holds more
memory, or uses a costlier per-pixel metric to buy its speed.

**What to measure and report for every variant**, so the gate can be applied:
`remap_ms` (best-of-N), total CPU core-seconds, peak RAM (RSS / `VmHWM` or
`runtime.MemStats`), and where relevant allocs/op and B/op.

## Two hard exceptions that override "spend freely"

1. **Do not OOM at 8K.** An OOM-kill is infinitely slow. Memory growth that risks
   the 8K peak must keep band streaming available as the safety lever, even though
   "RAM is cheap" otherwise.
2. **Do not let speed downgrade the exact default.** Exactness is the default and
   is non-negotiable on that path: a faster matcher that changes output (fails the
   bit-identical / golden gate) is not eligible as the default. "Fastest" for the
   default means "fastest among bit-identical options." Approximate/Fast paths are
   opt-in only; "fastest overall" applies only there.

## Order of constraints (when they conflict)

1. Correctness on the default path (bit-identical to the exact oracle).
2. Do not OOM.
3. Lowest wall-clock time.
4. Among equal-time options, lowest resource cost.

Approximate paths relax constraint 1 only when the caller explicitly opted in.
