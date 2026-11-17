# Changelog fragments

Each user-visible change adds one small JSON file here describing what changed.
At release time these fragments are collected into the GitHub Release notes and
used to compute the next version of each module. Nothing is hand-written into a
`CHANGELOG.md`, and fragments are **never edited after merge** — they are the
historical record.

See the full guide: [docs/CHANGELOG_AND_RELEASES.md](../docs/CHANGELOG_AND_RELEASES.md).

## Quickstart

Create a fragment interactively (recommended). It prompts for module(s), type,
and description, and assumes a **non-breaking** change:

```bash
make changelog-new
```

For a **breaking change** (forces a major bump), use the dedicated target:

```bash
make changelog-new-major
```

Non-interactive:

```bash
make changelog ARGS='new --type feature --modules . --description "Add slog-based request logging middleware."'
```

(All wrap `go run ./tools/changelog`; see the [Makefile](../Makefile). You can
still call the tool directly, e.g. `cd tools && go run ./changelog new` or
`... new --major`.)

## Fragment format

`.changelog/<random-id>.json`:

```json
{
  "type": "feature",
  "bump": "minor",
  "description": "Add slog-based request logging middleware.",
  "modules": ["."]
}
```

| Field | Required | Values |
|-------|----------|--------|
| `type` | yes | `feature`, `bugfix`, `documentation`, `dependency`, `release` |
| `bump` | no | `patch`, `minor`, `major` — defaults from `type` (below). Set `major` for breaking changes. |
| `description` | yes | Single-line markdown; shown verbatim in the notes. |
| `modules` | yes | One or more of: `clients/adminapi`, `.` (core), `integrations/gin`. Use several for a change that spans modules. |

**Default bump per type:** `feature` → minor; `bugfix`/`documentation`/`dependency` → patch; `release` → minor.

**General Highlights** (repo-wide headline notes at the top of a release) are not
driven by fragments — they come only from optional free text the maintainer
enters when running the release workflow. When left blank, the section is omitted.

**Major versions** are not automated (they need a `/vN` module-path migration) —
a `major` bump makes the release workflow stop with instructions.
