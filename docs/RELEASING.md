# Releasing

This repository is a **multi-module Go workspace**. Each module is versioned and
released independently. See [ADR 0001](adr/0001-multi-module-repository-structure.md).

## Modules

| Module                | Import path                                          | Tag prefix             |
|-----------------------|------------------------------------------------------|------------------------|
| Core SDK              | `github.com/shopware/sdk-go`                     | `vX.Y.Z`                  |
| Gin integration       | `github.com/shopware/sdk-go/integrations/gin`    | `integrations/gin/vX.Y.Z` |
| Admin API client      | `github.com/shopware/sdk-go/clients/adminapi`    | `clients/adminapi/vX.Y.Z` |

The `examples/*` modules are not published and carry no tags.

## Tagging convention

Go derives the module version from a tag whose prefix is the module's path **inside the
repository**:

- Core: `git tag -a v1.0.0 -m "sdk-go v1.0.0"`
- Gin integration: `git tag -a integrations/gin/v1.0.0 -m "integrations/gin v1.0.0"`
- Admin API client: `git tag -a clients/adminapi/v1.0.0 -m "clients/adminapi v1.0.0"`

Use annotated (`-a`) or signed (`-s`) tags. A tag is permanent: once a version has been
pushed (and fetched by the Go proxy), it cannot be moved or reused — fix mistakes with
a new patch version.

A module is only released when it changed. An integration release does **not**
require a core release unless the core changed, and a core release does not require a
new client unless the client changed.

### Major versions (v2+)

Go requires the major version suffix in the module path for `v2` and above. Before
tagging `v2.0.0` of a module, its `go.mod` module path must end with `/v2`, e.g.:

```
module github.com/shopware/sdk-go/integrations/fiber/v2
```

then tag `integrations/fiber/v2.0.0`. The same applies to the core module.

## Dependency order

The modules depend on each other in one direction only
([ADR 0003](adr/0003-admin-api-client-module.md)):

```
clients/adminapi  ←  sdk-go (core)  ←  integrations/gin  ←  examples/*
```

The client never imports the core, and the core never imports an integration.
**Always tag in this order** (client, then core, then integrations): a module's tag
must only point at a commit whose `go.mod` requires versions that already exist,
otherwise `go get` of the new version fails for consumers.

## Release workflow

A module that needs a newer version of another module in this repository gets it in
one of two ways.

### A. Pin ahead (recommended)

The feature PR already sets the **upcoming** versions:

- the `require` lines in every `go.mod` that depends on the changed modules
  (other modules and the examples), and
- a temporary `replace` for each not-yet-existing version in `go.work`
  (see [Pinning an unreleased version](#pinning-an-unreleased-version)).

CI builds against the local source through `go.work`, so the not-yet-existing
versions are never downloaded. After the PR is merged, all modules are tagged on the
**same merge commit**, in dependency order.

Release checklist:

1. Merge the PR and wait until CI on `main` is green. Never tag a red commit.
2. Update your local `main`: `git checkout main && git pull origin main`.
3. Check that every `require` names the versions you are about to tag:
   `grep -rn "shopware/sdk-go" go.work go.mod */*/go.mod`.
4. Tag and push in dependency order, each tag on the merge commit:

   ```bash
   git tag -a clients/adminapi/v1.1.0 -m "clients/adminapi v1.1.0"
   git push origin clients/adminapi/v1.1.0
   git tag -a v1.2.0 -m "sdk-go v1.2.0"
   git push origin v1.2.0
   git tag -a integrations/gin/v1.2.0 -m "integrations/gin v1.2.0"
   git push origin integrations/gin/v1.2.0
   ```

   Skip the modules that did not change.
5. Create the umbrella GitHub Release (see [GitHub Releases](#github-releases)).
6. Open a follow-up PR that removes the temporary `replace` lines from `go.work`. The
   versions now exist, so the workspace resolves them without help.

### B. Pin after

If the feature PR did not pin the new versions:

1. Tag and push the lower module (for example `clients/adminapi/v1.1.0`).
2. Open a PR that bumps the `require` in the dependent modules and the examples
   (`go get github.com/shopware/sdk-go/clients/adminapi@v1.1.0 && go mod tidy`).
3. Merge it, then tag the dependent module (for example `v1.4.0`), and repeat for the
   next level (`integrations/gin`) if it needs the new core.

This needs one extra PR per dependency level, but no temporary `replace`.

## Local development & `go.work`

Local development uses the committed [`go.work`](../go.work). It lists every module of
the repository in `use`, so builds and tests always run against the local source. No
module `go.mod` carries a `replace` directive: it would reach consumers.

Go still reads the `go.mod` of every version a module lists in `require` to build the
module graph. For released versions it gets them from the Go module proxy, so
`go.work` needs nothing else.

### Pinning an unreleased version

With [pin ahead](#a-pin-ahead-recommended), a module requires a version that is not
tagged yet, for example core `v1.4.0`. Go cannot download its `go.mod`, so the PR adds
a temporary `replace` to `go.work`:

```
replace github.com/shopware/sdk-go v1.4.0 => .
```

- Pin the exact version: Go rejects a versionless `replace` for a module that is
  already in `use`.
- Keep it in lockstep with the `require`; a `replace` for another version has no effect.
- Remove it in the first PR after the tags are pushed.

The example Docker images download the published modules, so they cannot build an
example that requires an unreleased version until its tag exists.

## GitHub Releases

In addition to the Go module tags above, every release gets a single **umbrella
GitHub Release** covering all modules released that day. The umbrella tag is not a
Go module version — Go tooling ignores it; it exists only for release notes.

### Tag naming

- One release per day: `release-YYYY-MM-DD`, e.g. `release-2026-07-08`.
- Several releases on the same day: append a sub-release counter after a dot,
  e.g. `release-2026-07-08.2` for the second release of that day.

### Release title

`Release (YYYY-MM-DD)` — matching the tag, including the sub-release counter:
`Release (2026-07-08.2)`.

### Release description

The body lists every module released under a **Module Highlights** heading. Each
entry names the module import path with its new version, followed by one bullet
per change, labeled **Feature**, **Bug Fix**, **Documentation**, or **Release**:

```markdown
## Module Highlights

* `github.com/shopware/sdk-go`: v1.1.0
  * **Feature**: Add slog-based request logging middleware.
  * **Bug Fix**: Fix HMAC validation for empty request bodies.
* `github.com/shopware/sdk-go/integrations/gin`: integrations/gin/v1.0.1
  * **Documentation**: Document router group usage.
```

### Creating the release

Point the umbrella tag at the same commit that carries the module tags:

```bash
gh release create release-2026-07-08 \
  --target main \
  --title "Release (2026-07-08)" \
  --notes-file release-notes.md
```

or via the GitHub UI: **Releases → Draft a new release**, type the umbrella tag
name (GitHub creates it on publish), set the title and paste the notes.

## CI

`.github/workflows/ci.yml` runs lint, test, vulnerability scan, benchmarks and build per
module via a matrix. Coverage gating runs for the core module and `clients/adminapi`.

## Adding a new module

A new module (for example `integrations/fiber` or `clients/storeapi`) is added in its
own PR, before the code that uses it:

1. Create `<path>/go.mod` with `module github.com/shopware/sdk-go/<path>` and the
   same `go` directive as the other modules, plus `README.md` and a copy of `LICENSE`
   (pkg.go.dev looks for a license in each module).
2. Add it to `use` in [`go.work`](../go.work). Until its first tag exists, a module
   that requires it needs a temporary `replace` (see
   [Pinning an unreleased version](#pinning-an-unreleased-version)).
3. Register it in CI and tooling:
   - the module matrices in `.github/workflows/ci.yml` (lint, vuln, test, benchmarks,
     build);
   - `MODULES` in `.github/scripts/ci.sh` (also used by `compatibility.yml`);
   - the `vet` loop in the `Makefile`;
   - the `go_modules` directories in `.github/dependabot.yml`.
4. Document it: the [Modules](#modules) table and [Tagging convention](#tagging-convention)
   above, the architecture tree in the [README](../README.md), and an ADR when the
   module changes the dependency direction.
5. Decide the first version: start at `v0.x` while the API may still change, and reach
   `v1.0.0` before a stable module exposes its types in its public API.
6. Release it with its first tag `<path>/vX.Y.Z`, following the
   [dependency order](#dependency-order).
