# Research moved

The pixelize nearest-color-scaling research record (reports 01–12 + their
raw-data companions + the ImageMagick reverse-engineering scratch) has moved to
its permanent home:

    noelruault/research/nearest-color-scaling/
    https://github.com/noelruault/research/tree/86bd65da70c654c33cbf33fde213b6bf78180391/nearest-color-scaling
    (pinned to the import commit; browse the branch tip for any later edits)

Imported from pixelize@551d4f2 (branch
`claude/extract-nearest-color-scaling-VtO3b`; original authorship lineage
`549498cb3aa8ff7b8d7d9a75160d18ad4c83a525`, branch
`claude/readme-skill-agent-pixelize-I4GK3`) on 2026-06-01, now at
noelruault/research@86bd65da70c654c33cbf33fde213b6bf78180391.

## Why it moved

The record was staged here only because the sessions that produced it were
network-scoped to pixelize and ran in ephemeral containers — committing into the
app repo was the only way to preserve it. A raw research record (including
third-party ImageMagick source under its own license) does not belong in the
application repo long-term, so it now lives in `noelruault/research`.

## What stayed here

The pixelize planning files that consume this research stay in `.plans/`, because
they drive the build:

- `../00-overview.md` — the judged synthesis / verdict-per-regime
- `../01-execution-plan.md` — the phased implementation plan
- `../EVALUATION-RUBRIC.md`

Only the raw research record moved.
