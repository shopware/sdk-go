# Context Resolver

The `resolver` package provides a framework-agnostic way to resolve and verify shops from HTTP requests. It combines shop lookup and signature verification into a single, reusable component.

## Overview

The `Resolver` acts as the Go SDK's equivalent to:
- PHP SDK's `ShopResolver`
- JS SDK's `ContextResolver`

It provides methods for resolving shops from different request types.

## Usage

### Basic Setup

```go
import (
    "github.com/shopware/sdk-go/app"
    "github.com/shopware/sdk-go/store"
)

// Create SDK instance (includes resolver automatically)
sdk, err := app.New(app.Config{
    AppName:         "MyApp",
    AppSecret:       "my-secret",
    ConfirmationURL: "https://my-app.com/register/confirm",
    ShopStore:       store.NewInMemoryStore(),
})
if err != nil {
    // Handle error
}

// Access the resolver
resolver := sdk.Resolver()
```

### Resolving from Query Parameters (GET Requests)

Used for:
- Frontend iframe authentication (`/api/auth/token`)
- Module requests from Shopware admin
- Any GET request with `shop-id` and `shopware-shop-signature` parameters

```go
func HandleAuthToken(sdk *app.App) gin.HandlerFunc {
    return func(c *gin.Context) {
        // Resolve and verify shop in one call
        shop, err := sdk.Resolver().ResolveFromQuery(c.Request.Context(), c.Request)
        if err != nil {
            c.JSON(401, gin.H{"error": err.Error()})
            return
        }
        
        // Shop is verified! Generate token
        token, err := tokenManager.CreateToken(shop)
        if err != nil {
            c.JSON(500, gin.H{"error": "Failed to create token"})
            return
        }
        
        c.JSON(200, gin.H{"token": token})
    }
}
```

**Request Requirements:**
- Query parameter: `shop-id` - The shop identifier
- Query parameter: `shopware-shop-signature` - HMAC-SHA256 signature
- Query parameter: `timestamp` - Unix seconds the request was created at (see Query Policy below)

**What it does:**
1. Extracts `shop-id` from query parameters
2. Loads shop from store
3. Verifies HMAC signature using shop's secret (falling back to the previous secret inside the rotation grace window)
4. Checks the signed `timestamp` against the freshness window
5. Rejects shops that are not active
6. Returns verified shop or error

### Query Policy (Freshness And Shop State)

The signature proves who built a signed URL, not when. Because the bytes never
change, replaying them reproduces a valid signature forever — the timestamp only
becomes an expiry once it is compared with the current time. `QueryPolicy` adds
that comparison plus the shop-state check, and its zero value is the strictest
policy:

| Field | Default | Meaning |
| --- | --- | --- |
| `Timestamp` | `TimestampRequire` | Reject requests with no timestamp; also `TimestampVerifyIfPresent` and `TimestampIgnore` |
| `MaxAge` | 5 min | How long a signed query stays valid |
| `ClockSkew` | 1 min | How far a timestamp may be dated into the future |
| `AllowInactiveShop` | `false` | Whether inactive shops keep authenticating |

Set it once for the whole app:

```go
sdk, err := app.New(app.Config{
    AppName:   "MyApp",
    AppSecret: "my-secret",
    ShopStore: store.NewInMemoryStore(),
    QueryPolicy: resolver.QueryPolicy{
        MaxAge: 2 * time.Minute,
    },
})
```

Or override it for a single route — for example a legacy signed link whose
callers do not send a timestamp:

```go
shop, err := sdk.Resolver().ResolveFromQueryWithPolicy(ctx, req, resolver.QueryPolicy{
    Timestamp: resolver.TimestampVerifyIfPresent,
})
```

Errors map to `*sdkerrors.Error`: `timestamp_expired` (401) for a stale, missing
or future-dated timestamp, and `shop_not_active` (403) for a deactivated shop.

Freshness bounds the replay window; it does not make a URL single-use. Endpoints
that mint a session or token from a signed query should keep that credential
short-lived and authorize it separately.

### Freshness On The Other Paths

The default mode differs per path, because Shopware only stamps some payloads.
`resolver.Config` carries the resolver-side ones (`QueryPolicy`, `PostPolicy`);
webhooks are governed by `webhook.Policy` on the middleware:

| Path | Timestamp | Where to configure |
| --- | --- | --- |
| `ResolveFromQuery` | required | `app.Config.QueryPolicy` |
| `ResolveFromPost` | checked if present | `app.Config.PostPolicy` |
| `ResolveFromWebhook` | required | `app.Config.WebhookPolicy` |
| `ResolveLifecycleWebhook` | required | `app.Config.LifecyclePolicy` |

`ResolveFromPost` cannot require a timestamp: Shopware builds payment, tax
provider and gateway payloads without one, and stamps action buttons at
`meta.timestamp` rather than the `timestamp` field this package reads. Requiring
it would reject them all. Pass `PostPolicy{Timestamp: TimestampRequire}` to
`ResolveFromPostWithPolicy` on endpoints you know only receive webhook-shaped
bodies.

The matrix is enforced by `TestFreshnessParityAcrossPaths` and
`TestActiveShopParityAcrossPaths` in `policy_test.go`, so it cannot drift
silently.

### Resolving from Webhook (POST Requests)

Used for:
- Webhook events with the webhook event shape (`source`, `data.event`, `timestamp`)

```go
func HandleCustomWebhook(sdk *app.App) gin.HandlerFunc {
    return func(c *gin.Context) {
        traceID := c.GetHeader("X-Request-ID")
        
        // Resolve shop and parse webhook event
        shop, event, err := sdk.Resolver().ResolveFromWebhook(
            c.Request.Context(),
            c.Request,
            traceID,
        )
        if err != nil {
            c.JSON(401, gin.H{"error": err.Error()})
            return
        }
        
        // Process webhook
        logger.Info("Webhook received",
            slog.String("shop_id", shop.ShopID),
            slog.String("event", event.Data.Event),
        )
        
        c.Status(204)
    }
}
```

**Request Requirements:**
- Header: `shopware-shop-signature` - HMAC-SHA256 signature (HTTP header names are case-insensitive)
- Body: JSON with `source.shopId`, `source.url`, and `data.event` fields

**What it does:**
1. Parses the webhook event from the body
2. Loads the shop and rejects it if inactive
3. Verifies the body signature (falling back to the previous secret inside the rotation grace window)
4. Checks the signed `timestamp` against `webhook.Policy` (required by default)
5. Returns verified shop, event, and any error

### Resolving from Source POST Requests

Used for signed JSON POST requests that include `source.shopId` but are not
webhook events, such as action endpoints, module action endpoints, and service
license synchronization endpoints.

```go
func HandleLicenseSync(sdk *app.App) gin.HandlerFunc {
    return func(c *gin.Context) {
        shop, err := sdk.Resolver().ResolveFromPost(c.Request.Context(), c.Request)
        if err != nil {
            c.JSON(401, gin.H{"error": err.Error()})
            return
        }

        // Shop is verified; the request body is restored for the handler.
        _ = shop
    }
}
```

**Request Requirements:**
- Header: `shopware-shop-signature` - HMAC-SHA256 signature (HTTP header names are case-insensitive)
- Body: JSON with `source.shopId` field

**What it does:**
1. Reads and restores the raw request body
2. Extracts `source.shopId`
3. Loads the shop from the store and rejects it if inactive
4. Verifies the body signature using the shop secret
5. Checks the body's `timestamp` against `PostPolicy` — only when one is present, by default
6. Returns the verified shop

### Resolving Lifecycle Webhooks

Used for the `activate`, `deactivate` and `delete` routes. Unlike every other
path it serves inactive shops, because the activate webhook arrives before the
shop is active and the deactivate webhook while it is being switched off.

It is also the only path that takes an expected event, and it is not optional:

```go
func HandleDelete(sdk *app.App) gin.HandlerFunc {
    return func(c *gin.Context) {
        shop, event, err := sdk.Resolver().ResolveLifecycleWebhook(
            c.Request.Context(), c.Request, webhook.EventAppDeleted,
        )
        if err != nil {
            c.JSON(403, gin.H{"error": err.Error()})
            return
        }

        _ = event
        _ = sdk.Store().DeleteShop(c.Request.Context(), shop.ShopID)
    }
}
```

A signature proves which shop secret signed the bytes, never which transition
those bytes authorize. Three lifecycle routes mutate the same shop in three
different ways, and the caller picks the route — so without the binding a signed
`app.activated` body aimed at the delete route deletes the shop. A body whose
`data.event` is not the expected one is rejected with `403 event_mismatch` before
anything is written.

**Request Requirements:**
- Header: `shopware-shop-signature` - HMAC-SHA256 signature of the request body
- Body: JSON with `source.shopId`, `data.event` and `timestamp` fields

**What it does:**
1. Reads the raw request body
2. Extracts `source.shopId` and loads the shop — **without** the active check
3. Verifies the body signature (falling back to the previous secret inside the rotation grace window)
4. Rejects the request unless `data.event` is the event this route accepts
5. Checks the body's `timestamp` against `LifecyclePolicy` (required by default)
6. Returns the verified shop and event

`LifecyclePolicy` governs step 5 only. Step 4 is mandatory and has no policy
field, so no configuration can turn it off. Use
`ResolveLifecycleWebhookWithPolicy` to vary the freshness window per route.

### Auto-Detection (Mixed Requests)

Automatically detects GET requests and routes all other methods to the webhook
resolver. Use `ResolveFromPost` directly for signed JSON POST requests that are
not webhook events.

```go
func HandleGeneric(sdk *app.App) gin.HandlerFunc {
    return func(c *gin.Context) {
        shop, event, err := sdk.Resolver().ResolveFromRequest(
            c.Request.Context(),
            c.Request,
            "trace-123",
        )
        if err != nil {
            c.JSON(401, gin.H{"error": err.Error()})
            return
        }
        
        // For GET requests, event will be nil
        // For POST requests, event will be populated
        
        c.JSON(200, gin.H{
            "shop_id": shop.ShopID,
            "has_event": event != nil,
        })
    }
}
```

## Framework Independence

The resolver works with any HTTP framework:

### With Standard net/http

```go
http.HandleFunc("/api/auth/token", func(w http.ResponseWriter, r *http.Request) {
    shop, err := sdk.Resolver().ResolveFromQuery(r.Context(), r)
    if err != nil {
        http.Error(w, err.Error(), 401)
        return
    }
    
    // Generate token...
})
```

### With Echo

```go
e.GET("/api/auth/token", func(c echo.Context) error {
    shop, err := sdk.Resolver().ResolveFromQuery(c.Request().Context(), c.Request())
    if err != nil {
        return c.JSON(401, gin.H{"error": err.Error()})
    }
    
    // Generate token...
})
```

### With Chi

```go
r.Get("/api/auth/token", func(w http.ResponseWriter, r *http.Request) {
    shop, err := sdk.Resolver().ResolveFromQuery(r.Context(), r)
    if err != nil {
        http.Error(w, err.Error(), 401)
        return
    }
    
    // Generate token...
})
```

## Security

The resolver performs **cryptographic verification** of all requests:

1. **Signature Verification**: Uses HMAC-SHA256 to verify request authenticity
2. **Shop Validation**: Ensures shop exists and is registered
3. **Constant-Time Comparison**: Prevents timing attacks
4. **No Exposed Secrets**: Shop secrets never leave the store

## Error Handling

The resolver returns specific errors:

- `"missing shop-id query parameter"` - Shop ID not in request
- `"missing shopware-shop-signature query parameter"` - Signature not in request  
- `"shop not found: ..."` - Shop doesn't exist in store
- `"invalid signature: ..."` - Signature verification failed
- `"webhook verification failed: ..."` - Webhook validation failed

All errors are logged with context for debugging.

## Comparison with PHP/JS SDKs

| Feature | PHP SDK | JS SDK | Go SDK |
|---------|---------|--------|--------|
| **Class/Package** | `ShopResolver` | `ContextResolver` | `resolver.Resolver` |
| **GET Resolution** | `resolveFromQueryString()` | `fromBrowser()` | `ResolveFromQuery()` |
| **POST Resolution** | `resolveFromSource()` | `fromAPI()` | `ResolveFromPost()` |
| **Webhook Resolution** | `assembleWebhook()` | `fromAPI()` | `ResolveFromWebhook()` |
| **Integration** | Symfony bundle | Hono middleware | Gin middleware |
| **Usage** | `$shopResolver->resolveShop($req)` | `await app.contextResolver.fromAPI(req)` | `sdk.Resolver().ResolveFromPost(ctx, req)` |
| **Signed query freshness** | not checked | not checked | enforced (`QueryPolicy`) |
| **Shop state on signed query** | not checked | not checked | enforced (`QueryPolicy`) |

All three follow the same security pattern: load shop → verify signature → return verified shop.

The Go SDK goes one step further on signed GET requests: `authenticateGetRequest`
in `app-php-sdk` and `verifyGetRequest` in `app-sdk-js` stop at the signature, so
a signed URL they accept never expires. Every documented Shopware signed-query
flow sends a `timestamp`, so enforcing it stays protocol-compatible; use
`TimestampVerifyIfPresent` or `TimestampIgnore` per route if you have a flow that
genuinely omits it.
