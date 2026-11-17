# Changelog & Releases

This repository automates releases from small **changelog fragments**.
Contributors drop one JSON file per user-visible change; a maintainer then runs
a single workflow that derives the versions, generates the release notes, tags
the affected modules, and publishes a GitHub Release.

Nothing is hand-written into a `CHANGELOG.md`, and the release workflow never
pushes a commit to a branch — it only creates tags and the Release.

```
 contributor PR                          maintainer
 ┌──────────────┐   merge to main   ┌─────────────────────────┐
 │ + .changelog │ ────────────────▶ │ Actions ▸ Release ▸ Run  │
 │   /<id>.json │                   └───────────┬─────────────┘
 └──────────────┘                               │
                                                ▼
             versions derived from fragments ▸ module tags (dependency order)
                                                ▸ umbrella GitHub Release
```

The tooling lives in [`tools/changelog`](../tools/changelog) and the workflow in
[`.github/workflows/release.yml`](../.github/workflows/release.yml). The tag and
release-notes conventions are defined in [RELEASING.md](RELEASING.md).

---

## For contributors: adding a changelog entry

A fragment is **required** for pull requests whose title type is `feat`, `fix`,
`docs`, or `perf` (enforced by the `Changelog entry required` CI job). It is not
required for `chore`, `ci`, `test`, `style`, or `build`. To bypass in a genuine
edge case, apply the `skip-changelog` label to the PR.

### Create a fragment

Interactive (recommended) — it prompts for module(s), type, and description, and
assumes a **non-breaking** change:

```bash
make changelog-new
```

For a **breaking change** (forces a `major` bump), use the dedicated target:

```bash
make changelog-new-major
```

Non-interactive:

```bash
make changelog ARGS='new --type feature --modules . --description "Add slog-based request logging middleware."'
```

All wrap `go run ./tools/changelog` (see the [Makefile](../Makefile)); you can
also call the tool directly, e.g. `cd tools && go run ./changelog new` (add
`--major` for a major bump). Other targets: `make changelog-validate`,
`make changelog-plan`, `make changelog-gen`.

This writes `.changelog/<random-id>.json`. Commit it in your PR. Do not edit or
delete fragments after merge — they are the historical record and are collected
by the next release.

### Fragment schema

```json
{
  "type": "feature",
  "bump": "minor",
  "description": "Add slog-based request logging middleware.",
  "modules": ["."]
}
```

| Field | Required | Meaning |
|-------|----------|---------|
| `type` | yes | Category → note label. One of `feature`, `bugfix`, `documentation`, `dependency`, `release`. |
| `bump` | no | Version increment: `patch`, `minor`, `major`. Defaults from `type` (below). Set `major` only for breaking changes. |
| `description` | yes | Single-line markdown, shown verbatim in the notes. |
| `modules` | yes | Repo-relative module dirs the change affects: `clients/adminapi`, `.` (core), `integrations/gin`. List several for a cross-module change. |

**Type → label / default bump:**

| `type` | Label in notes | Default `bump` |
|--------|----------------|----------------|
| `feature` | **Feature** | minor |
| `bugfix` | **Bug Fix** | patch |
| `documentation` | **Documentation** | patch |
| `dependency` | **Dependency** | patch |
| `release` | **Release** | minor |

### General Highlights

The **General Highlights** section (repo-wide headline notes at the top of a
release — e.g. "the SDK was split into independently versioned modules") is not
driven by changelog fragments. It comes only from optional free text the
maintainer enters when running the release workflow, and is omitted when left
blank. Every fragment belongs to one or more modules and renders under
**Module Highlights**.

### Examples

Single module:

```json
{ "type": "bugfix", "description": "Fix HMAC validation for empty request bodies.", "modules": ["."] }
```

Spanning several modules (one file, listed in each module's notes):

```json
{ "type": "feature", "description": "Support context cancellation end to end.", "modules": [".", "integrations/gin"] }
```

Admin API client change that the core picks up (pin ahead, see below):

```json
{ "type": "feature", "description": "Add Sync API helpers.", "modules": ["clients/adminapi", "."] }
```

---

## For maintainers: cutting a release

1. Open **Actions → Release → Run workflow**.
2. Optionally fill **General Highlights** (markdown for repo-wide headline notes)
   and/or tick **dry_run** to preview.
3. Run it.

The workflow:

- Selects the fragments added since the previous `release-*` tag.
- **Derives versions**: each module's next version = its latest tag bumped by the
  highest level among that module's fragments. Modules with no fragments are not
  released.
- **Fails if there is nothing to release** (no fragments since the last release).
- **Fails on a `major` bump** — major versions require a manual `/vN`
  module-path migration (see [RELEASING.md](RELEASING.md)) and are not automated.
- Generates the notes in the **Module Highlights** format.
- Enforces the **release-order guard** (below).
- Creates and pushes annotated module tags in **dependency order**:
  `clients/adminapi`, then core, then `integrations/gin`.
- Creates the umbrella tag `release-YYYY-MM-DD` (with a `.2`, `.3`… suffix for
  additional releases the same day) and publishes the GitHub Release.
- Fetches the new versions through `proxy.golang.org`, so they are available to
  `go get` and pkg.go.dev right away.
- Warns when `go.work` still has `replace` directives (see below).

### Release-order guard

Modules are tagged in the dependency order from [RELEASING.md](RELEASING.md#dependency-order):

```
clients/adminapi  ←  sdk-go (core)  ←  integrations/gin
```

When a dependent is released, every intra-repo version pinned in its `go.mod`
(core → `clients/adminapi`; gin → core and `clients/adminapi`) must be **either
already tagged or released in the same run at exactly that version**. So:

- **core only, needs a newer, untagged client** → the workflow fails. Add a
  `clients/adminapi` fragment so the client is released too, or pin the core to
  an already tagged client version.
- **client + core together**, with the core's `go.mod` requiring the client
  version being released → passes (the client is tagged first).
- **gin only, needs a newer, untagged core** → the workflow fails. Add a core
  fragment, or land a PR that bumps `integrations/gin/go.mod`
  (`go get github.com/shopware/sdk-go@vX.Y.Z`).
- **core + gin together**, with gin's `go.mod` requiring the core version being
  released → passes (core is tagged before gin).

This is the automated form of the **pin ahead** flow in
[RELEASING.md](RELEASING.md#a-pin-ahead-recommended): the feature PR pins the
upcoming versions in `go.mod` (plus a temporary `replace` in `go.work` for each
version that is not tagged yet), and the fragments must produce exactly those
versions. Example: the core requires
`clients/adminapi v1.1.0`, so the PR needs a client fragment with a **minor**
bump (`feature`) — a `bugfix` fragment would compute `v1.0.1` and the guard
stops the release. Run `make changelog-plan` before merging to check.

### What is not committed

The workflow creates only tags and the Release. `.changelog/` fragments are left
in place (append-only history); versions are scoped by "added since the last
`release-*` tag", so nothing needs to be deleted. An optional cleanup PR can
prune old fragments later without changing any generated output.

The temporary `go.work` replaces from a pin-ahead PR are not removed either:
after the release, open a follow-up PR that drops them (step 6 of the
[pin ahead checklist](RELEASING.md#a-pin-ahead-recommended)). The workflow prints a
notice when `go.work` still has a `replace`.

---

## Adding a new module to the tooling

Follow the checklist in [RELEASING.md → Adding a new module](RELEASING.md#adding-a-new-module).
For the release tooling, register the module in the `Modules` registry in
[`tools/changelog/changelog.go`](../tools/changelog/changelog.go) (dir, import
path, tag prefix, and `DependsOn`). The registry order is the tagging order, so
place the module **after** every module in its `DependsOn` (a unit test enforces
this). A module that was never tagged starts from `v0.0.0`, so its first release
is `v0.1.0` (`feature`) or `v0.0.1` (`bugfix`).
