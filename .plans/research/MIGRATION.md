# MIGRATION: this research belongs in `noelruault/research`

## Destination

This entire folder is staged here temporarily. Its permanent home is:

```
noelruault/research/pixelize-nearest-color-scaling/
```

## Why it is here and not there

The work was produced in a session whose tooling was scoped to two repos
only: `noelruault/aitelier` and `noelruault/pixelize`. The `noelruault/research`
repo was not reachable, so the research could not be pushed there directly.

The session also ran in an ephemeral container, where a plain temp folder is
reclaimed when the session ends. Committing into `pixelize` was the only way to
preserve the work durably. So it is staged on the pixelize branch, clearly
labeled, until someone with access moves it.

## What to move

Everything under `.plans/research/` is the research record:

- `01-imagemagick-reverse-engineering.md`
- `02-algorithms-and-libraries-survey.md`
- `03-experiments.md` (+ `03-experiments-data.txt`)
- `04-informed-challenger.md` (+ `04-informed-challenger-data.txt`)
- `05-cross-disciplinary-transfer.md`
- `06-puzzle-exact-structures.md` (+ data) — when complete
- `07-puzzle-enhancement-combinations.md` (+ data) — when complete

The synthesis that consumes this research, `.plans/00-overview.md` and
`.plans/01-execution-plan.md`, is pixelize project planning and stays in
pixelize. Only the raw research record migrates.

## How to move it (for someone with access to `noelruault/research`)

```sh
# in a clone of noelruault/research
mkdir -p pixelize-nearest-color-scaling
# copy the files listed above from the pixelize branch into that folder
git add pixelize-nearest-color-scaling
git commit -m "Import pixelize nearest-color scaling research"
```

After it lands in `noelruault/research`, this staged copy in pixelize can be
removed, leaving only the planning files in `.plans/`.
