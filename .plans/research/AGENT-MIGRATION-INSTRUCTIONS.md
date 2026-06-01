# Agent instructions — migrate this research to `noelruault/research`

**This is an actionable runbook for an AI agent.** If you have been asked to
migrate this research, follow "Migration runbook" below. If not, read "What this
is" and leave the directory untouched.

---

## What this is

Everything in this `.plans/research/` directory is the **research record** behind
the pixelize nearest-color matcher work: a numbered series of reports (`01`–`12`),
each with its raw-data companion (`*-data.txt`), plus a reverse-engineering scratch
subdirectory. It was committed here **only because the session that created it was
network-scoped to `noelruault/aitelier` and `noelruault/pixelize`** and ran in an
ephemeral container — committing into pixelize was the only way to preserve it. It
does not belong in the pixelize application repo long-term.

**Permanent home:**

```
noelruault/research/pixelize-nearest-color-scaling/
```

### Provenance (for the reference stub left behind)

- Staged on pixelize branch: `claude/readme-skill-agent-pixelize-I4GK3`
- Last commit before migration: `549498cb3aa8ff7b8d7d9a75160d18ad4c83a525`

---

## What migrates vs. what stays

**MIGRATES** — the whole research record (this entire `.plans/research/` directory,
including this instructions file):

```
AGENT-MIGRATION-INSTRUCTIONS.md            (this file — migrate it too)
MIGRATION.md                               (historical note)
01-imagemagick-reverse-engineering.md
02-algorithms-and-libraries-survey.md
03-experiments.md                          + 03-experiments-data.txt
04-informed-challenger.md                  + 04-informed-challenger-data.txt
05-cross-disciplinary-transfer.md
06-puzzle-exact-structures.md              + 06-puzzle-exact-structures-data.txt
07-puzzle-enhancement-combinations.md      + 07-puzzle-enhancement-combinations-data.txt
08-scan-variants.md                        + 08-scan-variants-data.txt
09-kd-variants.md                          + 09-kd-variants-data.txt
10-runlength-variants.md                   + 10-runlength-variants-data.txt
11-lut-fastmode-variants.md                + 11-lut-fastmode-variants-data.txt
12-phase3-enhancement-variances.md         + 12-phase3-enhancement-variances-data.txt
01-imagemagick-reverse-engineering-scratch/   (whole subdirectory)
```

> ⚠️ **The `01-imagemagick-reverse-engineering-scratch/` subdirectory contains
> verbatim third-party ImageMagick source** (© ImageMagick Studio LLC, ImageMagick
> License — see that directory's `README.md`). It is included only as the analyzed
> subject of report 01. **Its attribution README must travel with it**, and the
> destination repo should treat it as third-party material.

**STAYS in pixelize** — these are pixelize project planning, not research, and must
NOT be moved (they live one level up, in `.plans/`):

```
.plans/00-overview.md          (synthesis / verdicts that drive the build)
.plans/01-execution-plan.md    (the phased implementation plan)
.plans/EVALUATION-RUBRIC.md
.plans/README.md
```

---

## Migration runbook (for an agent with access to `noelruault/research`)

**Precondition:** you must be able to push to `noelruault/research`. If your tooling
is not scoped to that repo, STOP — you cannot complete this; report that the move
is blocked on repo access and leave everything as-is.

1. **Copy** the research record into the destination repo, preserving structure:

   ```sh
   # from a clone of noelruault/research, with the pixelize branch checked out
   # somewhere as $PIXELIZE (branch claude/readme-skill-agent-pixelize-I4GK3):
   DEST=pixelize-nearest-color-scaling
   mkdir -p "$DEST"
   cp -R "$PIXELIZE/.plans/research/." "$DEST/"
   git add "$DEST"
   git commit -m "Import pixelize nearest-color-scaling research (from pixelize@549498c)"
   git push
   ```

2. **Verify** the import: confirm reports `01`–`12` + every `*-data.txt` + the
   `01-imagemagick-reverse-engineering-scratch/` subdir (with its attribution
   README) all landed, then capture the destination commit SHA.

3. **Leave the reference behind in pixelize.** Replace the contents of
   `.plans/research/` with a single pointer file so the link is preserved both ways
   and the third-party source no longer lives in the app repo. Delete everything
   else under `.plans/research/` and write `.plans/research/MOVED.md` as:

   ```markdown
   # Research moved

   The pixelize nearest-color-scaling research record (reports 01–12 + data +
   the ImageMagick reverse-engineering scratch) has moved to its permanent home:

       noelruault/research/pixelize-nearest-color-scaling/

   Imported from pixelize@549498cb3aa8ff7b8d7d9a75160d18ad4c83a525
   (branch claude/readme-skill-agent-pixelize-I4GK3) on <DATE>,
   now at noelruault/research@<DEST_SHA>.

   The pixelize planning files (../00-overview.md, ../01-execution-plan.md,
   ../EVALUATION-RUBRIC.md) stay here — only the raw research record moved.
   ```

   Fill in `<DATE>` and `<DEST_SHA>`, commit on a branch, and push.

4. **Do not** touch the `.plans/` planning files in step 3 — only `.plans/research/`.

---

## If you are NOT migrating

Leave this directory as-is. It is the complete, self-contained research record;
`MIGRATION.md` holds the original historical note. Nothing here is compiled or
imported by the pixelize binary.
