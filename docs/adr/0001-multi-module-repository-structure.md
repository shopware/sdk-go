# ADR 0001: Multi-Module Repository Structure

- **Status:** Accepted
- **Date:** 2026-06-16
- **Deciders:** sdk-go maintainers
- **Tags:** go-modules, versioning, architecture, ci-cd
- **Relates to:** [ADR 0002](0002-no-logging-in-core.md), [ADR 0003](0003-admin-api-client-module.md)

---

## Context

The SDK has a framework-independent core (registration, webhook verification, shop
context resolution, storage) and adapters for HTTP frameworks such as Gin. If all of
it lived in a single Go module, this would cause a few problems:

- **Framework dependencies leak into the core.** Gin and its full dependency tree
  (`github.com/bytedance/sonic`, `github.com/quic-go/quic-go`,
  `github.com/go-playground/validator/v10`, `github.com/cloudwego/base64x`,
  `github.com/goccy/*`, and so on) would be declared in the **root** `go.mod` and
  forced on every consumer — even one that uses only the core SDK.
- **Versioning is coupled.** Any change in an integration would need a new release of
  the whole SDK.
- **Larger security surface.** `govulncheck ./...` and the core CVE graph would grow
  with framework dependencies that core consumers do not care about.

**Key fact:** the dependency is strictly **one-way**. The core does not import any
framework; an integration imports the core packages (`app`, `sdkerrors`,
`registration`, `signature`, `store`, `webhook`, ...). So the split into modules is
clean and needs no untangling of cyclic dependencies.

---

## Decision

The repository is a **multi-module Go repository** with a thin, framework-independent
core and integrations as standalone modules.

### Modules

- **Core** — `module github.com/shopware/sdk-go`, with no HTTP framework
  dependencies.
- **Integrations** — separate modules under the top-level `integrations/<fw>/` folder:
  - `github.com/shopware/sdk-go/integrations/gin`
  - `github.com/shopware/sdk-go/integrations/fiber` (future)
  - `github.com/shopware/sdk-go/integrations/echo` (future)
- **API clients** — separate modules under `clients/`, usable without the core
  (see [ADR 0003](0003-admin-api-client-module.md)):
  - `github.com/shopware/sdk-go/clients/adminapi`

Each integration:

- declares its own dependencies in its own `go.mod`;
- depends on the core through Go Modules with an explicit version.

### Local development

A root `go.work` (Go workspaces) builds all modules and examples from local source:

```
go 1.26.10

use (
    .
    ./clients/adminapi
    ./examples/admin-extension
    ./examples/gin-service
    ./examples/vanilla-service
    ./integrations/gin
)
```

`go.work` is committed to the repository and **does not affect consumers** (Go ignores
the workspace when the module is installed as a dependency). `replace` directives are
never put into a module's `go.mod`. A temporary `replace` in `go.work` is only needed
while a module requires a version of another module that is not tagged yet; see
[Pinning an unreleased version](../RELEASING.md#pinning-an-unreleased-version).

### Layout

```
sdk-go/
├── go.mod                      # core github.com/shopware/sdk-go — no framework deps
├── go.work                     # local development (workspace)
├── app/  registration/  resolver/  response/
├── sdkerrors/  signature/  store/  webhook/
├── internal/testutil/          # shared test helpers (not exported)
├── clients/
│   └── adminapi/   { go.mod → .../clients/adminapi }
├── integrations/
│   ├── gin/    { go.mod → .../integrations/gin; logs via slog }
│   ├── fiber/  { go.mod }       # future
│   └── echo/   { go.mod }       # future
└── examples/                   # separate modules, not released
    ├── gin-service/
    ├── admin-extension/
    └── vanilla-service/
```

---

## Versioning & Tagging

Each module has its own version line. The tag carries the module path prefix inside
the repository:

| Module             | Module path                                        | Example tag                 |
|--------------------|----------------------------------------------------|-----------------------------|
| Core               | `github.com/shopware/sdk-go`                       | `v1.2.0`                    |
| Gin integration    | `github.com/shopware/sdk-go/integrations/gin`      | `integrations/gin/v1.1.0`   |
| Admin API client   | `github.com/shopware/sdk-go/clients/adminapi`      | `clients/adminapi/v1.0.1`   |
| Fiber integration (v2) | `github.com/shopware/sdk-go/integrations/fiber/v2` | `integrations/fiber/v2.1.0` |

Rules:

- **An integration release does not require a core release** if the core did not change.
- **Go rule for major versions (≥ v2):** the module path must end with the suffix
  `/vN`, or `go get` will not resolve the version. Example: the module
  `github.com/shopware/sdk-go/integrations/fiber/v2` is tagged as
  `integrations/fiber/v2.1.0`. The same rule applies to the core when it moves to v2+.
- Install commands for consumers:
  - `go get github.com/shopware/sdk-go` — core only (no Gin);
  - `go get github.com/shopware/sdk-go/integrations/gin` — the integration,
    which pulls Gin only here.

---

## CI/CD

`.github/workflows/ci.yml` runs lint, tests, `govulncheck` and build **per module**
(a matrix over `.`, `integrations/gin` and `clients/adminapi`), and builds every
example. Modules are tagged independently with the path prefix
(`integrations/gin/vX.Y.Z`), as described in [RELEASING.md](../RELEASING.md).

---

## Consequences

### Benefits

- A light, framework-independent core without the CVE surface of Gin and its
  transitive dependencies.
- Independent releases: integrations ship separately from the core.
- A consumer pulls Gin (or Fiber/Echo) **only** when installing that integration.
- A clear, enforced boundary: the core simply cannot depend on a framework.
- Atomic PRs and a shared CI stay in place (unlike separate repositories).

### Costs and trade-offs

- More complex CI: a per-module matrix, tags with a path prefix.
- A learning curve for contributors on `go.work` and the tagging scheme.
- The need to track compatibility between core and integration versions.

---

## Adding a new integration module

1. Create `integrations/<fw>/go.mod` (module
   `github.com/shopware/sdk-go/integrations/<fw>`) that requires the core.
2. Add the folder to `go.work`.
3. Add the module to the CI matrix and to the module table in
   [RELEASING.md](../RELEASING.md).
4. Copy `LICENSE` into the module folder (pkg.go.dev looks for a license in each module).
5. Document the install command in `README.md`.

---

## Alternatives Considered

1. **Single module with build tags for integrations.** Rejected: build tags do not
   remove Gin from the module dependency graph — the dependencies and the CVE surface
   stay with every consumer.
2. **Separate repositories per integration.** Rejected: we lose atomic PRs across
   core + integration, the shared CI, and a single release process; higher cost to
   maintain and to keep versions in sync.
3. **Integration modules nested inside the core package tree.** Rejected: mixes
   framework-specific code into the core tree. A top-level `integrations/` folder keeps
   the boundary visible.

---

## Acceptance Criteria

- The core can be consumed without framework dependencies
  (`go get github.com/shopware/sdk-go`).
- Every integration is a standalone Go module.
- Integrations can be released independently from the core.
- Go tooling resolves module versions correctly (including `/vN` for majors ≥ 2).
- CI/CD supports per-module releases and tagging.
