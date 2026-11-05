# ADR 0001: Multi-Module Repository Structure

- **Status:** Proposed
- **Date:** 2026-06-16
- **Deciders:** app-sdk-go maintainers
- **Tags:** go-modules, versioning, architecture, ci-cd
- **Relates to:** [ADR 0002](0002-no-logging-in-core.md)

---

## Context

Today the whole SDK and the gin integration live in a single Go module
`github.com/shopware/app-sdk-go` (`go 1.26.0`). The integration sits in
`pkg/integration/gin/gin.go`. This setup causes a few problems:

- **Framework dependencies leak into the core.** gin and its full dependency tree
  (`github.com/bytedance/sonic`, `github.com/quic-go/quic-go`,
  `github.com/go-playground/validator/v10`, `github.com/cloudwego/base64x`,
  `github.com/goccy/*`, and so on) are declared in the **root** `go.mod`. They are
  forced on every consumer — even one that uses only the core SDK.
- **Versioning is coupled.** Any change in the integration needs a new release of the
  whole SDK. Right now this is a single tag line `v0.0.1 … v0.0.7`.
- **Larger security surface.** `govulncheck ./...` and the core CVE graph grow with
  framework dependencies that core consumers do not care about.

**Key fact:** the dependency is strictly **one-way**.
The core does not import gin. The link goes only in the direction
`pkg/integration/gin` → `pkg/app`, `pkg/errors`, `pkg/logger`, `pkg/registration`,
`pkg/signature`, `pkg/store`, `pkg/webhook`. Searching for `gin-gonic` under `pkg/`
outside the integration finds nothing. So the split into modules is clean and does
not need any untangling of cyclic dependencies.

Also: the folders `examples/gin-service` and `examples/admin-extension` are already
separate Go modules with `replace` directives. The multi-module pattern is already
used in part.

---

## Decision

Move the repository to a **multi-module Go repository** with a thin,
framework-independent core and integrations as standalone modules.

### Modules

- **Core** — `module github.com/shopware/app-sdk-go`, with no HTTP framework
  dependencies.
- **Integrations** — separate modules under the top-level `integrations/<fw>/` folder:
  - `github.com/shopware/app-sdk-go/integrations/gin`
  - `github.com/shopware/app-sdk-go/integrations/fiber` (future)
  - `github.com/shopware/app-sdk-go/integrations/echo` (future)

Each integration:

- declares its own dependencies in its own `go.mod`;
- depends on the core through Go Modules with an explicit version.

### Local development

Use a root `go.work` (Go workspaces) instead of `replace` directives:

```
go 1.26.0

use (
    ./
    ./integrations/gin
    ./examples/gin-service
    ./examples/admin-extension
)
```

`go.work` is committed to the repository and **does not affect consumers** (Go ignores
the workspace when the module is installed as a dependency). The existing `examples/*`
move from `replace` to the workspace.

> Note on intra-repo replaces: `go.work` keeps version-pinned `replace` directives
> **inside `go.work`** (never in any module's `go.mod`), e.g.
> `replace github.com/shopware/app-sdk-go v1.2.0 => .`. These are local-development
> only and never reach consumers. While the repository is private they are **required,
> not just a bootstrap**: `use` redirects package builds to local source, but Go still
> reads the `go.mod` of each `require`d version when building the module graph, which
> for a private repo means a failing `git ls-remote` in CI. Keep each replace version
> in lockstep with the corresponding `require`. See [RELEASING.md](../RELEASING.md).
>
> **Update (2026-11-05):** the repository is public, so Go reads released versions from
> the module proxy and the permanent replaces were removed from `go.work`. A temporary
> `replace` is only needed while a module requires a version that is not tagged yet; see
> [Pinning an unreleased version](../RELEASING.md#pinning-an-unreleased-version).
> The `pkg/client/` package in the layout below was later replaced by the
> `clients/adminapi` module ([ADR 0003](0003-admin-api-client-module.md)).

### Target layout

```
app-sdk-go/
├── go.mod                      # core github.com/shopware/app-sdk-go — NO gin
├── go.work                     # local development (workspace)
├── pkg/                        # core — no framework deps, no logger (see ADR 0002)
│   ├── app/  client/  signature/  store/  webhook/
│   ├── registration/  resolver/  response/  errors/  ...
│   └── internal/testutil/
├── integrations/
│   ├── gin/    { go.mod → .../integrations/gin, gin.go, *_test.go, doc.go; logs via slog }
│   ├── fiber/  { go.mod }       # future
│   └── echo/   { go.mod }       # future
└── examples/                   # already separate modules; replace → go.work
    ├── gin-service/
    ├── admin-extension/
    └── vanilla-service/
```

---

## Versioning & Tagging

Each module has its own version line. The tag carries the module path prefix inside
the repository:

| Module             | Module path                                            | Example tag                 |
|--------------------|--------------------------------------------------------|-----------------------------|
| Core               | `github.com/shopware/app-sdk-go`                       | `v1.8.0`                    |
| Gin integration    | `github.com/shopware/app-sdk-go/integrations/gin`      | `integrations/gin/v1.3.0`   |
| Fiber integration (v2) | `github.com/shopware/app-sdk-go/integrations/fiber/v2` | `integrations/fiber/v2.1.0` |
| Echo integration   | `github.com/shopware/app-sdk-go/integrations/echo`     | `integrations/echo/v1.0.0`  |

Rules:

- **An integration release does not require a core release** if the core did not change.
- **Go rule for major versions (≥ v2):** the module path must end with the suffix
  `/vN`, or `go get` will not resolve the version. Example: the module
  `github.com/shopware/app-sdk-go/integrations/fiber/v2` is tagged as
  `integrations/fiber/v2.1.0`. The same rule applies to the core when it moves to v2+.
- Install commands for consumers:
  - `go get github.com/shopware/app-sdk-go` — core only (no gin);
  - `go get github.com/shopware/app-sdk-go/integrations/gin` — the integration,
    which pulls gin only here.

---

## CI/CD (roadmap; not built in this ADR)

The current `.github/workflows/ci.yml` runs lint/test/`govulncheck`/build only for
`./pkg/...` of the root module. The target pipeline should:

- **Detect changed modules** (by file path diff) and build a matrix.
- **Run per-module** `golangci-lint`, `go test -race`, `govulncheck`, `go build`
  inside each affected module folder.
- **Tag only affected modules** with the path prefix (`integrations/gin/vX.Y.Z`).
- **Check compatibility** of the core version that each integration points to (for
  example, run the integration tests against the minimum required and the latest core
  versions).

---

## Consequences

### Benefits

- A light, framework-independent core without the CVE surface of gin and its
  transitive dependencies.
- Independent releases: integrations ship separately from the core.
- A consumer pulls gin (or fiber/echo) **only** when installing that integration.
- A clear, enforced boundary: the core simply cannot depend on a framework.
- Atomic PRs and a shared CI stay in place (unlike separate repositories).

### Costs and trade-offs

- **Breaking import path change:** `…/pkg/integration/gin` → `…/integrations/gin`.
  This needs a migration guide and, possibly, one final core release with a
  deprecation note in the old package.
- More complex CI: a per-module matrix, tags with a path prefix, compatibility checks.
- A learning curve for the team on `go.work` and the tagging scheme.
- The need to track compatibility between core and integration versions.

---

## Migration (and rollback)

Step-by-step plan (done as a separate task):

1. Create `integrations/gin/go.mod` (module
   `github.com/shopware/app-sdk-go/integrations/gin`) and move the package there from
   `pkg/integration/gin`.
2. Delete `pkg/logger` (the custom `Logger` interface, `Field` helpers, and zap
   adapter); the gin integration logs via `*slog.Logger` instead (see ADR 0002).
3. Remove gin and its transitive dependencies, plus `go.uber.org/zap`, from the root
   `go.mod`, then run `go mod tidy` in the core.
4. Add a root `go.work`; move `examples/*` from `replace` directives to the workspace.
5. Update CI: a per-module matrix and tagging with the path prefix.
6. Update docs: `README.md`, install examples (core alone, integrations alone), the
   release/tagging strategy, the local development workflow with several modules, and a
   migration guide.

**Rollback:** move the integration package back into the root module and delete
`integrations/gin/go.mod` (git revert of the change set).

---

## Alternatives Considered

1. **Single module with build tags for integrations.** Rejected: build tags do not
   remove gin from the module dependency graph — the dependencies and the CVE surface
   stay with every consumer.
2. **Separate repositories per integration.** Rejected: we lose atomic PRs across
   core + integration, the shared CI, and a single release process; higher cost to
   maintain and to keep versions in sync.
3. **Separate module but keep it under `pkg/integration/`.** Breaks the import path
   less, but mixes framework-specific code into the core tree and goes against the idea
   of a "clean" core `pkg/`. Rejected in favor of the top-level `integrations/`.

---

## Acceptance Criteria

- The core can be consumed without framework dependencies
  (`go get github.com/shopware/app-sdk-go`).
- Every integration is a standalone Go module.
- Integrations can be released independently from the core.
- Go tooling resolves module versions correctly (including `/vN` for majors ≥ 2).
- Existing consumers can migrate with minimal changes (following the migration guide).
- CI/CD supports per-module releases and tagging.
- Documentation reflects the new architecture.
