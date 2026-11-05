# Migration Guide

## Admin API client (v1.3.0)

**This release has breaking changes.** The old Admin API client (`pkg/client` and
`pkg/client/tokenstorage`) is replaced by the new client module
[`clients/adminapi`](../clients/adminapi/README.md). `App.NewClient` and
`App.NewClientForShop` now return `*adminapi.Client`.

Why in a minor version: this release was made before the SDK became public, so there
were no external users to protect with a deprecation period. Instead of shipping the old client as
deprecated and removing it in v2, it is removed now
([ADR 0003](adr/0003-admin-api-client-module.md#replacing-the-old-client-in-v130)).
After the first public release, breaking changes go through deprecation and a major
version as usual.

**Who is affected:** code that calls `sdk.NewClient`, `sdk.NewClientForShop`, imports
`pkg/client` or `pkg/client/tokenstorage`, or implements the old token storage
interface. Code that only handles registration, webhooks and signed requests needs no
change.

Your code will not compile until it is migrated; nothing changes silently at the API
level. The [behavior changes](#behavior-changes) below can still affect code that
compiles, so read them too.

### Removed API and its replacement

| Removed / changed in v1.3.0 | Use instead |
|---|---|
| `App.NewClient(shopID string) (*client.Client, error)` | `App.NewClient(ctx context.Context, shopID string) (*adminapi.Client, error)` |
| `App.NewClientForShop(shop) *client.Client` | `App.NewClientForShop(shop) (*adminapi.Client, error)`: now also returns an error |
| `client.Client` | `*adminapi.Client` |
| `client.NewClient(client.Config{Shop, TokenStorage})` | `adminapi.NewClient(adminapi.Config{BaseURL, Credentials, TokenStorage})`, or `App.NewClientForShop` |
| `client.Config` | `adminapi.Config` |
| `client.Response` (`StatusCode`, `Body`, `Headers`, `JSON`) | `*adminapi.Response` (same fields and `JSON`) |
| `client.DefaultTimeout` | `adminapi.DefaultTimeout` (still 30 seconds) |
| `client.MinErrorStatusCode` | `errors.As(err, &apiErr)` with `*adminapi.APIError`, or `adminapi.IsNotFound`, `IsUnauthorized`, `IsConflict` |
| `tokenstorage.TokenStorage` from `pkg/client/tokenstorage` (`Get` returns token and expiry) | `tokenstorage.TokenStorage` from `clients/adminapi/tokenstorage` (works with `tokenstorage.Token`) |
| `tokenstorage.NewInMemory()`, `tokenstorage.NewNoOp()` from `pkg/client/tokenstorage` | the same functions from `clients/adminapi/tokenstorage`, plus `NewKV` and `NewScoped` |

New import paths:

```go
import (
    "github.com/shopware/app-sdk-go/clients/adminapi"
    "github.com/shopware/app-sdk-go/clients/adminapi/tokenstorage"
)
```

`clients/adminapi` is a separate Go module. `go get github.com/shopware/app-sdk-go@v1.3.0`
adds it automatically; add it yourself only if you use the client without the core
SDK: `go get github.com/shopware/app-sdk-go/clients/adminapi@v1.0.0`.

### Step by step

#### 1. Create the client

In a webhook or action handler you already have the verified shop.
`NewClientForShop` now returns an error (for example for a shop without a valid URL);
before, the old client reported that problem on the first request.

```diff
- c := sdk.NewClientForShop(verified.Shop)
+ c, err := sdk.NewClientForShop(verified.Shop)
+ if err != nil {
+     return err
+ }
```

With only a shop ID (a background job, a queue worker), pass a context. It is used to
load the shop from the store:

```diff
- c, err := sdk.NewClient(shopID)
+ c, err := sdk.NewClient(ctx, shopID)
```

Creating a client is cheap. All clients of a shop share one token, so create a client
where you need it instead of keeping clients in a map.

#### 2. Send requests

`Get`, `Post`, `Patch` and `Delete` keep their signatures. Paths are still relative to
`/api`. Build paths that contain IDs or other outside values with `adminapi.Path`, which
escapes each segment:

```diff
- resp, err := c.Get(ctx, "/product/"+productID)
+ resp, err := c.Get(ctx, adminapi.Path("product", productID))
```

New in the client:

- `Put(ctx, path, body)`.
- `Request(ctx, method, path, body, opts...)` for any method.
- `RequestRaw(ctx, method, path, bodyFactory, opts...)` for a body that is not JSON,
  for example a file upload.
- Request options: `adminapi.WithHeader(key, value)`, `adminapi.WithQuery(key, value)`.

```go
resp, err := c.Post(ctx, "/search/product", criteria,
    adminapi.WithHeader("sw-language-id", languageID))
```

#### 3. Read responses

`*adminapi.Response` has the same fields as the old `client.Response`, so this part
usually needs no change:

```go
resp.StatusCode // int
resp.Headers    // http.Header
resp.Body       // []byte
err = resp.JSON(&target)
```

#### 4. Handle errors

Errors are now typed. The texts changed, so code that compares error messages must
check the type instead:

| Old check | New check |
|---|---|
| `strings.Contains(err.Error(), "not found")` or `"404"` | `adminapi.IsNotFound(err)` |
| `strings.Contains(err.Error(), "401")` | `adminapi.IsUnauthorized(err)` |
| `strings.Contains(err.Error(), "API error")`, or a status in the text | `var apiErr *adminapi.APIError` and `errors.As(err, &apiErr)`, then `apiErr.StatusCode`, `apiErr.Errors`, `apiErr.Body` |
| `strings.Contains(err.Error(), "authentication error")` | `var tokenErr *adminapi.TokenError` and `errors.As(err, &tokenErr)` |
| `resp.StatusCode >= client.MinErrorStatusCode` | not needed: a status of 400 or higher is always returned as `*adminapi.APIError` |
| `strings.Contains(err.Error(), "context deadline exceeded")` | `errors.Is(err, context.DeadlineExceeded)` |

```go
resp, err := c.Get(ctx, adminapi.Path("product", id))
var apiErr *adminapi.APIError
switch {
case adminapi.IsNotFound(err):
    // the product does not exist
case errors.As(err, &apiErr):
    for _, e := range apiErr.Errors {
        log.Printf("%s: %s", e.Code, e.Detail) // every error Shopware reported
    }
case err != nil:
    return err // network, token or context error
}
```

`*adminapi.APIError` keeps **all** errors of the Shopware response (`Errors`, with
`Status`, `Code`, `Title`, `Detail`, `Source` and `Meta`); the old client kept only the
first `detail`. A `*adminapi.TokenError` wraps an `*adminapi.APIError` when the shop
rejected the token request, so `adminapi.IsUnauthorized` works for both.

#### 5. Token storage

**With the App.** Tokens were not cached before: every `sdk.NewClient` fetched a new
token. Now the App keeps one token per shop in memory and shares it between all its
clients. Nothing to change. To share tokens between instances, set
`app.Config.TokenStorage`:

```go
sdk, err := app.New(app.Config{
    // ...
    TokenStorage: tokenstorage.NewKV(myRedisStore, tokenstorage.WithKeyPrefix("myapp:token:")),
})
```

`myRedisStore` implements three methods (`Get`, `Set` with a TTL, `Delete`); see
[docs/token-storage.md](token-storage.md) for Redis and Valkey code, key names, TTLs
and encryption.

**Without the App** (`client.NewClient` with your own storage):

```diff
- c := client.NewClient(client.Config{
-     Shop:         shop,
-     TokenStorage: tokenstorage.NewInMemory(), // pkg/client/tokenstorage
- })
+ c, err := adminapi.NewClient(adminapi.Config{
+     BaseURL: shop.ShopURL,
+     Credentials: adminapi.IntegrationCredentials{
+         ClientID:     shop.ClientID,
+         ClientSecret: shop.ClientSecret,
+     },
+     TokenStorage: tokenstorage.NewInMemory(), // clients/adminapi/tokenstorage
+ })
+ if err != nil {
+     return err
+ }
```

`adminapi.NewClient` checks the configuration and returns an error for a missing or
invalid `BaseURL` or missing credentials. If `TokenStorage` is nil, tokens are kept in
memory (the old client kept none).

**Your own storage implementation.** The old interface stored only an access token
and its expiry under the shop ID:

```go
// pkg/client/tokenstorage (removed)
Get(ctx, shopID string) (token string, expiry time.Time, err error)
Set(ctx, shopID, token string, expiry time.Time) error
Delete(ctx, shopID string) error
```

The new interface stores a whole token (with a refresh token for admin logins) and
reports a missing key with `tokenstorage.ErrNotFound`:

```go
// clients/adminapi/tokenstorage
Get(ctx, key string) (tokenstorage.Token, error) // ErrNotFound if missing
Set(ctx, key string, token tokenstorage.Token) error
Delete(ctx, key string) error
```

Best option: drop your implementation and write a `tokenstorage.KeyValueStore`
instead (`Get` / `Set` with TTL / `Delete` on bytes), wrapped with
`tokenstorage.NewKV`. `NewKV` handles encoding, TTLs, key prefixes and optional
encryption. If you want to keep your own `TokenStorage`, change it like this:

```go
func (s *myStorage) Get(ctx context.Context, key string) (tokenstorage.Token, error) {
    token, expiry, found, err := s.load(ctx, key)
    if err != nil {
        return tokenstorage.Token{}, err
    }
    if !found {
        return tokenstorage.Token{}, tokenstorage.ErrNotFound // was: "", time.Time{}, nil
    }
    return tokenstorage.Token{AccessToken: token, Expiry: expiry}, nil
}
```

Keys are no longer the shop ID. The App uses `<shop ID>:<hash of the client ID>`, so
tokens stored by v1.2 are simply not found and fetched again once. No data migration
is needed.

### Behavior changes

These do not show up as compile errors:

| | v1.2 (`pkg/client`) | v1.3 (`clients/adminapi`) | What to do |
|---|---|---|---|
| Token caching | none: a new token for every client | one token per shop, shared by all clients of the App, in memory by default | nothing; set `app.Config.TokenStorage` for several instances |
| Concurrent token fetches | one per client | one per shop within the App; across instances with `app.Config.Locker` | optional, see [one refresh at a time](token-storage.md#one-refresh-at-a-time) |
| Redirects | followed (up to 10) | **not followed**; returned as `adminapi.ErrUnexpectedRedirect` | use the final URL as shop URL, or set `app.Config.HTTPClient` with a `CheckRedirect` |
| Response size | unlimited | **32 MiB** (`adminapi.DefaultMaxResponseBytes`), larger bodies return `adminapi.ErrResponseTooLarge` | use paging, or `adminapi.Config.MaxResponseBytes` for standalone clients |
| `Content-Type: application/json` | on every request | only on requests with a body | nothing |
| `User-Agent` | Go default | `shopware-app-sdk-go-adminapi/<version>`, or `app.Config.UserAgent` | nothing |
| HTTP client | fixed, 30 s timeout | `app.Config.HTTPClient` (default: 30 s timeout) | set it for proxies, custom timeouts, retries or tracing |
| 401 on an API request | token deleted, request retried once | token invalidated, request retried once. If the stored token cannot be invalidated (storage error), the request is **not** retried: the error is an `*adminapi.APIError` with status 401 that also wraps the storage error | nothing: `adminapi.IsUnauthorized(err)` still works; use `errors.Is(err, yourStorageErr)` to spot storage problems |
| Token of a removed app | not deleted (only relevant with your own storage) | deleted by `sdk.ClearShopToken`; the Gin integration calls it on `app.deleted` | call `ClearShopToken` in your own delete handler |
| Re-registration with new credentials | a token stored under the shop ID could still be used | old token is deleted once the registration is confirmed | nothing |
| Invalid shop URL, or a nil shop | error on the first request (a nil shop panicked) | error from `NewClient` / `NewClientForShop`; a shop URL must use http or https, have a host and contain no query or fragment | handle the error where the client is created |
| Shop URL at registration | any URL that parses was accepted | rejected with 400 (`invalid_request`, "Invalid shop URL format") unless it is an http or https URL with a host and without query or fragment, so a shop cannot register with a URL the client cannot use | nothing for real shops; update your own registration tests if they use other URLs |
| Token response without `access_token` | accepted, an empty token was sent | error (`adminapi.ErrInvalidTokenResponse`) | nothing |
| Error messages | `API error: <detail>`, `request failed: ...`, `authentication error: ...` | new texts; typed errors | check errors by type (see step 4) |

### What else is new

- `app.Config.TokenStorage`, `app.Config.Locker`, `app.Config.HTTPClient` and
  `app.Config.UserAgent`.
- `App.ClearShopToken(ctx, shop)` removes a shop's token.
- The client can be used **without the App**, with integration credentials, an admin
  user login (`adminapi.PasswordCredentials`) or a refresh token
  (`adminapi.RefreshTokenCredentials`); refresh tokens are rotated and stored.
- `client.Authenticate(ctx)` checks credentials early; `AccessToken`, `SetToken`,
  `InvalidateToken` and `ClearToken` give manual control over the token.
- Token storage in Redis, Valkey or any key-value store without a driver dependency in
  the SDK (`tokenstorage.NewKV`), with optional encryption (`tokenstorage.WithCipher`):
  [docs/token-storage.md](token-storage.md).

### Checklist

1. Update: `go get github.com/shopware/app-sdk-go@v1.3.0` (and
   `github.com/shopware/app-sdk-go/integrations/gin@v1.3.0` if you use Gin), then
   `go mod tidy`.
2. Find the old API:
   ```bash
   grep -rn 'app-sdk-go/pkg/client' --include=*.go .
   grep -rnE '\.NewClient(ForShop)?\(' --include=*.go .
   ```
3. Replace imports and calls as shown above; `go build ./...` lists what is left.
4. Search for checks of error texts (`err.Error()`, `strings.Contains`) around Admin API
   calls and replace them with typed checks.
5. If you run several instances, set `app.Config.TokenStorage` (and optionally
   `Locker`).
6. If your shop URLs redirect (for example `http` to `https`), store the final URL or
   configure `app.Config.HTTPClient`.
7. If you have your own `ShopStore`-based delete handler without the Gin integration,
   call `sdk.ClearShopToken(ctx, shop)` there.

## Multi-module split + slog (from single-module SDK)

The SDK was refactored into a multi-module repository and the logging approach changed.
See [ADR 0001](adr/0001-multi-module-repository-structure.md) and
[ADR 0002](adr/0002-no-logging-in-core.md).

### 1. Gin integration import path changed (breaking)

The Gin integration moved to its own module.

```diff
- import ginintegration "github.com/shopware/app-sdk-go/pkg/integration/gin"
+ import ginintegration "github.com/shopware/app-sdk-go/integrations/gin"
```

And install it separately:

```bash
go get github.com/shopware/app-sdk-go/integrations/gin
```

The core SDK (`github.com/shopware/app-sdk-go`) no longer pulls in Gin or its transitive
dependencies. If you only use the core, you get a lighter dependency graph.

### 2. Logging now uses `log/slog` (breaking)

The custom `pkg/logger` package and the `go.uber.org/zap` dependency were removed. The
core SDK does not log; the Gin integration logs through the standard library `log/slog`.

`Config.Logger` is now a `*slog.Logger`:

```diff
- import (
-     "go.uber.org/zap"
-     "github.com/shopware/app-sdk-go/pkg/logger"
- )
- zapLogger, _ := zap.NewProduction()
- cfg := ginintegration.Config{
-     // ...
-     Logger: logger.NewZapAdapter(zapLogger),
- }
+ import (
+     "log/slog"
+     "os"
+ )
+ cfg := ginintegration.Config{
+     // ...
+     Logger: slog.New(slog.NewJSONHandler(os.Stdout, nil)),
+ }
```

If `Logger` is nil, the integration uses a no-op logger (`slog.New(slog.DiscardHandler)`).

Still want zap (or zerolog, etc.)? Plug it in via its `slog.Handler` bridge — the SDK
depends on none of them.

### 3. The core SDK takes no logger

`app.Config` never had a usable `Logger` field; any code passing a logger to the core
should drop it. The core reports problems via typed errors (`pkg/errors`); inspect them
with `errors.As(err, &sdkErr)` and log at your application/integration boundary.

## Signed query authentication is now checked for freshness and shop state

Fixes SEC-REPLAY-QUERY-L79. Previously `Resolver.ResolveFromQuery` and the Gin
`GetRequestVerifier` middleware verified only the HMAC, so a captured signed URL
authenticated forever and kept working for a deactivated shop.

### 1. Signed GET requests now expire (breaking behaviour)

`ResolveFromQuery` — and therefore `ResolveFromRequest` for GET, the Gin
`GetRequestVerifier` middleware and every GET route behind
`ginintegration.Configure` — now also requires:

- a `timestamp` query parameter no older than 5 minutes and no more than
  1 minute in the future → otherwise `401` / `timestamp_expired`;
- an active shop → otherwise `403` / `shop_not_active`.

Shopware sends `timestamp` in all documented signed-query flows, so no change is
needed for standard admin modules, action button tabs, or registration. If your
own tooling builds signed URLs, add the parameter (it is covered by the
signature, so it must be part of the signed string):

```diff
- shop-id=<id>&shop-url=<url>
+ shop-id=<id>&shop-url=<url>&timestamp=<unix-seconds>
```

Tests that reused a hardcoded timestamp need a current one. The internal test
helper gained `NewSignedQueryRequestAt(url, query, secret, ts)` for this.

To tune or opt out, set the policy once:

```go
sdkApp, err := app.New(app.Config{
    // ...
    QueryPolicy: resolver.QueryPolicy{
        Timestamp: resolver.TimestampVerifyIfPresent, // only check when present
        MaxAge:    2 * time.Minute,
    },
})
```

or per route:

```go
router.GET("/legacy", ginintegration.GetRequestVerifierWithPolicy(sdkApp, logger,
    resolver.QueryPolicy{Timestamp: resolver.TimestampIgnore}), handler)
```

`resolver.QueryPolicy{}` is the strict default, so a partially filled policy
never silently weakens the check.

### 2. Gin `GetRequestVerifier` now returns SDK errors (breaking)

The middleware delegates to the core resolver instead of duplicating its logic,
so failures are serialized `*sdkerrors.Error` values rather than the previous
ad-hoc shape, and some statuses changed:

```diff
- {"error": "Shop not found"}   // HTTP 400
+ {"type": "shop_not_found", "message": "shop not found: <id>", "code": 404}

- {"error": "Missing shop-id or signature"}  // HTTP 400 for both
+ {"type": "invalid_request", "message": "missing shop-id parameter", "code": 400}
+ {"type": "invalid_signature", "message": "missing signature parameter", "code": 401}

- {"error": "Invalid signature"}  // HTTP 401
+ {"type": "invalid_signature", "message": "query signature verification failed", "code": 401}
```

This also fixes a divergence: the middleware previously ignored the
previous-secret grace window, so signed GET requests failed in Gin during a
secret rotation while the same request succeeded through the core resolver.

### 3. Gin `Config` gained pass-through fields

`ginintegration.Configure` now forwards `QueryPolicy`, `PostPolicy`,
`WebhookPolicy`, `InflightAllowanceSeconds` and the re-registration flag to
`app.Config`. They previously had no effect because the integration dropped them
when constructing the `App`. As before, these apply only when the integration
builds the `App` (`Config.App == nil`).

## Freshness is now enforced per path, not only on signed queries

Follow-up to the signed-query fix above. Webhooks and signed JSON POSTs are now
covered by the same mechanism, with defaults chosen per path because Shopware
does not stamp every payload.

| Path | Before | After |
| --- | --- | --- |
| Signed GET query | required (previous release) | unchanged |
| Webhook | checked only if `timestamp > 0`, window hardcoded | **required**, window configurable |
| Signed JSON POST | not checked | checked when present |
| Lifecycle webhook | not checked | unchanged |

### 1. Webhooks require a timestamp (breaking behaviour)

A webhook body without `timestamp`, or with `"timestamp": 0`, is now rejected
with `401` / `timestamp_expired`. Real Shopware traffic is unaffected:
`AppPayloadServiceHelper::createWebhookRequest` stamps every payload and
re-stamps and re-signs it on each delivery attempt, so its retry ladder (5s, 30s,
5m, 30m, 4h) always arrives inside the 5-minute window. Only hand-rolled senders
and test fixtures that omitted the field are affected.

To keep the old behaviour:

```go
sdkApp, err := app.New(app.Config{
    // ...
    WebhookPolicy: webhook.Policy{
        Freshness: signature.FreshnessPolicy{Mode: signature.TimestampVerifyIfPresent},
    },
})
```

### 2. Webhook freshness failures report `timestamp_expired` (breaking)

The error type changed from `verification_failed` to `timestamp_expired`, matching
the signed query path. The HTTP status stays `401`.

### 3. Freshness is checked after the signature (breaking order)

`webhook.Middleware.VerifyWebhook` used to validate the timestamp before
verifying the signature, so an unsigned request with a stale timestamp got a
timestamp error. It now verifies the signature first, because an unauthenticated
timestamp is attacker-controlled data. Such a request now reports
`verification_failed` instead.

### 4. `ResolveFromPost` checks the body timestamp

Only when one is present, which is a protocol constraint rather than caution:
Shopware builds payment, tax provider and gateway payloads without a timestamp,
and stamps action buttons at `meta.timestamp`, which is not the `timestamp` field
the SDK reads. Requiring one would reject them all. Bodies that do carry a stale
timestamp are now rejected. Opt into strictness per endpoint:

```go
shop, err := sdkApp.Resolver().ResolveFromPostWithPolicy(ctx, req,
    resolver.PostPolicy{Timestamp: resolver.TimestampRequire})
```

### 5. Constructors now take a config struct (breaking)

`pkg/resolver` and `pkg/webhook` each had a pair of constructors — a short one
and a `…WithPolicy` variant — and the long one had grown to seven positional
parameters. Both pairs collapse into a single constructor taking a `Config`,
matching `client.NewClient(client.Config{…})`:

```diff
- webhook.NewMiddleware(store, verifier, dual, maxBodyBytes)
- webhook.NewMiddlewareWithPolicy(store, verifier, dual, maxBodyBytes, policy)
+ webhook.NewMiddleware(webhook.Config{
+     ShopStore:    store,
+     MaxBodyBytes: maxBodyBytes, // optional
+     Policy:       policy,       // optional
+ })

- resolver.NewResolver(store, verifier, dual, webhookMW, 0, 0)
- resolver.NewResolverWithPolicies(store, verifier, dual, webhookMW, 0, 0, policies)
+ resolver.NewResolver(resolver.Config{
+     ShopStore:       store,
+     WebhookVerifier: webhookMW,
+     QueryPolicy:     queryPolicy, // optional
+     PostPolicy:      postPolicy,  // optional
+ })
```

`Verifier` and `DualVerifier` are now optional: when omitted they default to
`signature.NewVerifier()` and a dual verifier derived from it plus
`InflightAllowanceSeconds`. Most call sites therefore shrink by two or three
lines. `ShopStore` (both) and `WebhookVerifier` (resolver) have no default.

`resolver.Policies` and `resolver.DefaultPolicies()` were removed. That type
existed only to avoid adding an eighth positional argument, which `Config`
solves properly; the per-path defaults are still available as
`resolver.DefaultQueryPolicy()` and `resolver.DefaultPostPolicy()`.

Applications built through `app.New` or `ginintegration.Configure` need no
change — `app.Config` and the Gin `Config` are unaffected, and both gained
`PostPolicy` and `WebhookPolicy` alongside `QueryPolicy` in this release.

The timestamp mode enum moved to `pkg/signature` and gained a `TimestampUnset`
zero value so each policy can resolve it to that path's own default.
`resolver.TimestampRequire` and friends are re-exported aliases, so existing code
keeps compiling.

### 6. The timestamp header is gone (breaking)

Shopware carries the request time in the signed payload — in the query string for
signed GET requests, in the JSON body for webhooks — never in a header. Two
symbols claimed otherwise and both were removed:

- `webhook.EventHeaders.ShopwareTimestamp`, a field that was always `""`. Read
  `Event.Timestamp` instead.
- `signature.TimestampHeader` (`"shopware-timestamp"`). Nothing ever read that
  header; the constant's only use was in an error message that told developers to
  look for a header which does not exist.

If you were using the constant to set the header on your own requests, drop it —
the SDK ignores it. Send the timestamp where Shopware sends it: as the
`timestamp` query parameter, or as the `timestamp` field in the signed body.

## Re-registration now requires the current shop secret

Fixes SEC-AUTH-REGISTRATION-L51. Previously a confirmed shop could be re-registered with
the app secret alone: the shop-signature check was skipped unless
`EnforceDoubleSignature` was set, the shop had already passed a double-signature flow, or
the caller supplied the header. That last condition made the check optional in practice —
omitting the signature switched it off. Anyone holding the app secret from `manifest.xml`
could take over a shop. Shopware tracks the same issue as
[GHSA-c4p7-rwrg-pf6p](https://github.com/shopware/shopware/security/advisories/GHSA-c4p7-rwrg-pf6p)
(CVSS 8.9).

### 1. Confirmed shops must prove their secret (breaking behaviour)

Re-registering a confirmed shop now requires `shopware-shop-signature`, and confirming it
requires `shopware-shop-signature-previous`. A missing or invalid signature is `401` /
`invalid_signature`; a missing one carries a distinct message naming the header, so an
outdated Shopware is distinguishable from an attempted takeover.

Shopware sends both headers on every re-registration from **6.6.10.15 / 6.7.8.1** onward,
so patched installations need no change. Older ones do not send them at all and will be
rejected. Two ways out, in order of preference:

1. Update Shopware. This is the fix the advisory ships.
2. Set the escape hatch below, accepting the known-vulnerable path meanwhile.

If a re-registration is rejected because the app still holds a stale shop record — the app
was reinstalled while `app.deleted` failed to reach your server — delete that record
instead of enabling the hatch. The next registration then takes the first-registration
path.

### 2. `EnforceDoubleSignature` was replaced (breaking)

Strict verification is now the default, so a flag meaning "enforce it" no longer says
anything. It is replaced by an opt-out with the opposite polarity:

```diff
  sdkApp, err := app.New(app.Config{
      // ...
-     EnforceDoubleSignature: true,
  })
```

Code that set it to `true` already got today's behaviour — just drop the line. Code that
relied on `false` must decide: update Shopware, or opt out explicitly.

```go
sdkApp, err := app.New(app.Config{
    // ...
    // Restores a known-vulnerable path. Legacy shops only; see SECURITY.md.
    AllowUnsignedReregistration: true,
})
```

The hatch never applies to a shop that has already proved knowledge of its secret, so it
cannot be used to downgrade a protected shop. A supplied signature is still verified while
the hatch is on, so a shop whose Shopware gets updated ratchets itself back to strict.

`ginintegration.Config` carries the same field and forwards it to `app.Config` (as before,
only when the integration builds the `App`). It replaces the `EnforceDoubleSignature`
pass-through added earlier in this release, so no released version ever exposed that name
on the Gin `Config`.

### 3. `HasVerifiedWithDoubleSignature` now records verified proof only

The flag used to be set whenever the header was present or enforcement was on — including
on a first registration, where there is no prior secret to verify against, so the value
recorded proof that never happened. It is now set only after a signature was actually
checked against a stored secret. Existing `true` values stay valid; a stored `false` on a
confirmed shop simply means "no proof seen yet", which under the default is unreachable
going forward.

## Lifecycle routes are bound to their signed event

Fixes SEC-AUTH-LIFECYCLE-L218. `ResolveLifecycleWebhook` verified only the body
signature, and the activate, deactivate and delete handlers picked their
transition from the HTTP route, reading the signed `data.event` for the log line
alone. A correctly signed `app.activated` body POSTed to `/app/delete` therefore
returned `204` and deleted the stored shop. Lifecycle was also the last path with
no freshness check, so such a body stayed valid indefinitely.

### 1. `ResolveLifecycleWebhook` takes the expected event (breaking)

```diff
- shop, event, err := sdk.Resolver().ResolveLifecycleWebhook(ctx, req)
+ shop, event, err := sdk.Resolver().ResolveLifecycleWebhook(ctx, req, webhook.EventAppDeleted)
```

Pass the event the route accepts: `webhook.EventAppActivated`,
`webhook.EventAppDeactivated` or `webhook.EventAppDeleted`. A body carrying
anything else — including no event at all — is rejected before the shop is
touched.

The parameter is required rather than a second method, so that no call can skip
the binding. An empty value is rejected as a programming error, not treated as a
wildcard. Routes behind `ginintegration.Configure` need no change; the
integration passes the right constant for each route.

If you were using `ResolveLifecycleWebhook` as a general "resolve without the
active-shop check" helper for non-lifecycle events, that no longer works. Use
`ResolveFromPost` or `ResolveFromWebhook`, whichever matches the body shape.

### 2. Route/event mismatch reports `event_mismatch` (new error, 403)

```json
{"type": "event_mismatch", "message": "route expects signed event app.deleted, body carries app.activated", "code": 403}
```

`403`, not `401`: the signature is valid, so the caller is authenticated — it is
just not authorized for this transition. There is no way to disable the check.
Shopware never sends one lifecycle event to another's route, so treat an
`event_mismatch` in production as a security signal.

### 3. Lifecycle bodies require a timestamp (breaking behaviour)

A lifecycle body without `timestamp`, or with `"timestamp": 0`, is now rejected
with `401` / `timestamp_expired`, matching the webhook path. Real traffic is
unaffected: the manifest registers lifecycle routes as ordinary `<webhook>`
entries, so Shopware delivers them through the pipeline that stamps and re-signs
every attempt. Only hand-rolled senders and test fixtures that omitted the field
are affected.

To relax the freshness check — and only that; the binding stays enforced:

```go
sdkApp, err := app.New(app.Config{
    // ...
    LifecyclePolicy: resolver.LifecyclePolicy{
        Timestamp: resolver.TimestampVerifyIfPresent,
    },
})
```

`ginintegration.Config` gained the same `LifecyclePolicy` field, applied when the
integration builds the `App`. Per-route overrides go through
`Resolver.ResolveLifecycleWebhookWithPolicy`.

## Local development

Use the committed `go.work`; no `replace` directives are needed in your own modules.
`go build ./...` / `go test ./...` resolve the core and integration modules locally.
