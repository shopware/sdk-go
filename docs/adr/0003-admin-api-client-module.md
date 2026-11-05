# ADR 0003: Admin API Client as a Separate Module

- **Status:** Proposed
- **Date:** 2026-09-29
- **Deciders:** app-sdk-go maintainers
- **Tags:** go-modules, admin-api, http-client, dal, architecture
- **Relates to:** [ADR 0001](0001-multi-module-repository-structure.md), [ADR 0002](0002-no-logging-in-core.md)

---

## Context

The SDK's current Admin API client (`pkg/client`) is small:

- It supports only the `client_credentials` grant (app and integration credentials).
  Admin user login (username and password) and refresh tokens are not supported.
- The Admin API has no higher-level helpers: no DAL criteria builder, no typed
  repositories, no sync, no extension manager.
- `App.NewClient` and `App.NewClientForShop` (`pkg/app/app.go`) create clients
  without a token storage, so every client fetches a fresh OAuth token from the shop.
  The App configuration offers no way to set one.

The repository [`shopwareLabs/go-shopware-http-client`](https://github.com/shopwareLabs/go-shopware-http-client)
already implements most of this. It includes OAuth with integration, password and
refresh-token grants, pluggable token storage, a DAL criteria builder, typed
repositories and aggregations, extension and media helpers, and a PKCE browser
login. We agreed with the repository owner that this functionality fits well into
the official app SDK and should continue there. The original repository is archived
and will not be developed further. The code is MIT licensed,
`Copyright (c) 2022 Friends of Shopware`.

That code requires Go 1.27. It uses `encoding/json/v2`, the standard library `uuid`
package, generic methods and `strings.CutLast`. The SDK supports Go 1.26 and 1.27.

The core module depends on the AWS SDK (DynamoDB store) and SQLite. A user who
needs only an Admin API client (a CLI tool, a worker, an integration) should not
have to pull those in.

---

## Decision

Bring the Admin API functionality into this repository as a **separate Go module**,
step by step. It **replaces** `pkg/client`: the SDK uses it directly, and the old
package is removed (see [Replacing the old client in v1.3.0](#replacing-the-old-client-in-v130)).

### Module

- Path: `github.com/shopware/app-sdk-go/clients/adminapi`, directory
  `clients/adminapi/`, package `adminapi`.
- The top-level `clients/` directory leaves room for other API clients (for example
  a Store API client). `integrations/` stays reserved for web framework adapters.
- Further functionality is added as **packages inside this module**, not as separate
  modules. Planned packages are `tokenstorage`, `dal`, `extension`, `media`,
  `instance` and `pkce`. None of them brings dependencies of its own, so separate
  modules would only add release overhead. A package that needs a heavy dependency
  (for example a ready-made Redis adapter) gets its own module, the same way as
  `integrations/gin`.

### Dependency direction

```
clients/adminapi  ←  app-sdk-go (pkg/app)  ←  integrations/gin
```

`clients/adminapi` never imports `github.com/shopware/app-sdk-go`. It knows nothing
about `store.Shop`, registration or signatures. The SDK maps a `store.Shop` to
client configuration.

### Dependencies and Go version

- `go 1.26.0`, matching the rest of the repository.
- Only the standard library, plus `golang.org/x/sync` (`singleflight`, used to
  collapse concurrent token requests).
- Features of the original code that need Go 1.27 are replaced during the port:

  | Original (Go 1.27)                      | Replacement (Go 1.26)                    |
  |-----------------------------------------|------------------------------------------|
  | `encoding/json/v2`, `jsontext.Value`    | `encoding/json`, `json.RawMessage`       |
  | standard library `uuid`                 | internal helper based on `crypto/rand`   |
  | generic methods (`repo.SearchIDsAs[T]`) | generic package functions                |
  | `strings.CutLast`                       | `strings.LastIndex`                      |
  | `github.com/shyim/go-version`           | internal version comparison              |

### Token storage

Token storage is defined as interfaces only. The module ships in-memory and no-op
implementations and an adapter for a minimal key-value interface. Users plug in
external stores (Redis, Valkey, Memcached, …) with their own driver, and the module
takes no dependency on any driver.

### SDK integration

`App.NewClient(ctx, shopID)` and `App.NewClientForShop(shop)` return
`*adminapi.Client`. The App maps a `store.Shop` to `adminapi.Config` (shop URL,
integration credentials) and passes its token storage, token key, Locker, HTTP client
and User-Agent. `pkg/client` and `pkg/client/tokenstorage` are removed.

### Licensing

`clients/adminapi/LICENSE` carries both copyright lines (Shopware and Friends of
Shopware). Pull requests that port code name the source repository.

---

## Versioning & Tagging

- Tags use the module path prefix: `clients/adminapi/vX.Y.Z` (see
  [RELEASING.md](../RELEASING.md)).
- The module stays on `v0.x` until its API settles.
- It must reach `v1.0.0` **before** any of its types appear in the core module's
  public API. Otherwise the stable core `v1` would expose unstable types.
- Release order when the core needs a new client version: tag
  `clients/adminapi/…`, then bump the core requirement, then tag the core, then
  `integrations/gin` if needed.
- While nothing requires the module, `go.work` needs only a `use` entry. Once the
  core requires it, `go.work` also gets a version-pinned `replace` for it, the same
  as the existing ones.

---

## Consequences

### Benefits

- A full Admin API client that can be used without the rest of the SDK and its
  storage dependencies.
- One implementation for the SDK and standalone users.
- Token storage in shared external stores without extra dependencies.
- Each step can be released on its own.

### Costs and trade-offs

- One more module to tag and release, with a fixed release order when the core
  depends on a new version.
- Porting takes more effort than copying, because of the Go 1.26 adaptations.
- v1.3.0 has breaking changes for users of the old client (see below). This is
  acceptable only because the SDK has not been published yet.

---

## Replacing the old client in v1.3.0

The first plan was to deprecate the old client in v1.3.0 and remove it in v2. Because
the SDK has not been published yet and has no external users, the maintainers decided
to skip the deprecation and replace the old client right away, in core `v1.3.0`
together with `clients/adminapi` `v1.0.0`:

- `App.NewClient(shopID) (*client.Client, error)` becomes
  `App.NewClient(ctx, shopID) (*adminapi.Client, error)`.
- `App.NewClientForShop(shop) *client.Client` becomes
  `App.NewClientForShop(shop) (*adminapi.Client, error)`.
- The packages `pkg/client` and `pkg/client/tokenstorage` are removed; use
  `clients/adminapi` and `clients/adminapi/tokenstorage`.
- The compatibility behavior of the old client is gone: redirects are not followed,
  response bodies are limited (32 MiB by default), and tokens are cached in memory by
  default.

This breaks the API inside a minor version, which Semantic Versioning does not allow
once a module is published. The module path stays `github.com/shopware/app-sdk-go`
(no `/v2`). After the first public release, breaking changes go through deprecation
and a major version as usual.

Consequences: no compatibility code (no wrapper, no adapter for the old token storage,
no legacy HTTP settings) has to be kept and removed later, and users meet one client
API from the start. `docs/MIGRATION.md` describes the upgrade step by step.

---

## Alternatives Considered

1. **Package inside the core module** (`pkg/adminapi`). Rejected: standalone users
   would pull the core module's AWS and SQLite dependencies.
2. **`integrations/adminapi/`.** Rejected: `integrations/` holds web framework
   adapters, and the client is not one.
3. **`adminapi/` at the repository root.** Viable, but leaves no common place for
   other API clients.
4. **A separate module per package** (`dal`, `extension`, …). Rejected: no package has
   dependencies of its own, so this only multiplies tags and CI entries.
5. **Depend on `go-shopware-http-client` as is.** Rejected: the repository is
   archived, it requires Go 1.27, and it has no releases.

---

## Acceptance Criteria

- `clients/adminapi` is a standalone module that builds and is tested in CI on every
  supported OS and Go version.
- The module does not import the core SDK.
- Its dependencies are limited to the standard library and `golang.org/x/sync`.
- It is released independently with `clients/adminapi/vX.Y.Z` tags.
- The SDK uses it directly: `App.NewClient` and `App.NewClientForShop` return
  `*adminapi.Client`, and `pkg/client` is removed.
