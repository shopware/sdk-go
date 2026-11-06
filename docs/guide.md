# Usage Guide

How to use the main parts of the SDK. For a first working app, start with
[Getting Started](getting-started.md); for production setup, see [Deployment](deployment.md).

- [Admin API Client](#admin-api-client)
- [Response Builders](#response-builders)
- [Storage Backends](#storage-backends)
- [Error Handling](#error-handling)
- [Retry Middleware](#retry-middleware)
- [Token Storage](#token-storage)
- [Logging](#logging)
- [Security Features](#security-features)
- [Testing Your App](#testing-your-app)
- [Package Layout](#package-layout)

## Admin API Client

Make authenticated API calls to Shopware with the Admin API client
([`clients/adminapi`](../clients/adminapi/README.md)):

```go
// Get shop from webhook or resolver
verified, _ := sdk.WebhookMiddleware().VerifyWebhook(ctx, r, traceID)

// Create the Admin API client for the shop
client, err := sdk.NewClientForShop(verified.Shop)
if err != nil {
    return err
}

// GET request; paths are relative to /api, build them with adminapi.Path
resp, err := client.Get(ctx, adminapi.Path("product", productID))
if adminapi.IsNotFound(err) {
    // the product does not exist
}
if err != nil {
    return err
}

var result struct {
    Data map[string]any `json:"data"`
}
if err := resp.JSON(&result); err != nil {
    return err
}

// POST request
newProduct := map[string]any{
    "name":          "My Product",
    "productNumber": "PROD-123",
    "stock":         100,
    "taxId":         "tax-id-here",
    "price": []map[string]any{
        {"currencyId": "currency-id-here", "gross": 19.99, "net": 16.80, "linked": true},
    },
}
resp, err = client.Post(ctx, "/product", newProduct)
```

All clients of a shop share its OAuth token: the token is fetched once and reused
until shortly before it expires (see [Token Storage](#token-storage)). The client also
offers `Put`, `Patch`, `Delete`, `RequestRaw` for uploads, request options such as
`adminapi.WithHeader`, and typed errors (`*adminapi.APIError`, `*adminapi.TokenError`).

With only a shop ID, for example in a background job, use
`sdk.NewClient(ctx, shopID)`; it loads the shop from the store.

## Response Builders

Type-safe response builders for common Shopware response types:

```go
import "github.com/shopware/sdk-go/response"

// Notifications
resp := response.NotificationSuccess("Operation completed")
resp := response.NotificationError("Something went wrong")
resp := response.NotificationInfo("Processing...")
resp := response.NotificationWarning("Check your settings")

// Actions
resp := response.ReloadResponse()
resp := response.OpenNewTabResponse("https://example.com")

// Payment responses
resp := response.PaymentPaid()
resp := response.PaymentFailed("Payment declined")
resp := response.PaymentCancelled()
resp := response.PaymentRedirect("https://payment-gateway.com/checkout")
```

## Storage Backends

### In-Memory Store (Development/Testing)

```go
store := store.NewInMemoryStore()
```

### SQLite Store (Local Development)

```go
sqliteStore, err := store.NewSQLiteStore(store.SQLiteConfig{
    DatabasePath: "./data/shops.db",
})
if err != nil {
    log.Fatal(err)
}
defer sqliteStore.Close()
```

### DynamoDB Store (Production)

```go
dynamoStore, err := store.NewDynamoDBStore(ctx, store.DynamoDBConfig{
    TableName: "shopware-shops",
    Region:    "us-east-1",
})
if err != nil {
    log.Fatal(err)
}
```

All stores implement the `ShopStore` interface and are safe for concurrent use.

## Error Handling

The SDK uses a unified error type with structured information:

```go
import "github.com/shopware/sdk-go/sdkerrors"

// Check error type
var sdkErr *sdkerrors.Error
if errors.As(err, &sdkErr) {
    switch sdkErr.Type {
    case sdkerrors.TypeInvalidSignature:
        // Handle signature error
    case sdkerrors.TypeShopNotFound:
        // Handle missing shop
    case sdkerrors.TypeTimestampExpired:
        // Handle replay attack
    }
}

// Error types
sdkerrors.TypeInvalidSignature    // Signature verification failed
sdkerrors.TypeShopNotFound         // Shop not in database
sdkerrors.TypeShopNotActive        // Shop is inactive
sdkerrors.TypeTimestampExpired     // Request too old (replay attack)
sdkerrors.TypeInvalidRequest       // Malformed request
sdkerrors.TypeRegistrationState    // Registration state transition cannot proceed
sdkerrors.TypeVerificationFailed   // General verification failure
sdkerrors.TypeInvalidPayload       // Invalid JSON/data
```

## Retry Middleware

Add automatic retry logic with exponential backoff to any store:

```go
import (
    "github.com/shopware/sdk-go/store"
    "github.com/shopware/sdk-go/store/middleware"
)

// Create base store
baseStore, _ := store.NewDynamoDBStore(ctx, store.DynamoDBConfig{
    TableName: "shops",
    Region:    "us-east-1",
})

// Wrap with retry middleware
storeWithRetry := middleware.NewRetryMiddleware(baseStore, middleware.RetryConfig{
    MaxRetries: 3,
    BaseDelay:  100 * time.Millisecond,
    MaxDelay:   20 * time.Second,
})

// Use in SDK
sdk, _ := app.New(app.Config{
    ShopStore: storeWithRetry,
})
```

The retry middleware automatically retries on:
- DynamoDB throttling errors
- Network timeouts
- Internal server errors

## Token Storage

The `App` keeps the Admin API tokens of all shops and shares them between the clients
it creates. By default tokens live in memory, which is fine for a single instance.
To share tokens between instances, set `TokenStorage`, for example to Redis or Valkey.
Implement three methods with the driver you already use; the SDK adds no driver
dependency:

```go
import adminstorage "github.com/shopware/sdk-go/clients/adminapi/tokenstorage"

type redisStore struct{ rdb *redis.Client }

func (s redisStore) Get(ctx context.Context, key string) ([]byte, error) {
    v, err := s.rdb.Get(ctx, key).Bytes()
    if errors.Is(err, redis.Nil) {
        return nil, adminstorage.ErrNotFound
    }
    return v, err
}

func (s redisStore) Set(ctx context.Context, key string, v []byte, ttl time.Duration) error {
    return s.rdb.Set(ctx, key, v, ttl).Err()
}

func (s redisStore) Delete(ctx context.Context, key string) error {
    return s.rdb.Del(ctx, key).Err()
}

sdk, err := app.New(app.Config{
    // ...
    TokenStorage: adminstorage.NewKV(redisStore{rdb}, adminstorage.WithKeyPrefix("myapp:token:")),
})
```

Clients of a shop that start at the same time fetch one token. With several instances
on a shared storage, each instance may fetch its own token once; that is harmless, but
you can set `Locker` (for example on Redis `SET NX PX`) to fetch one token per shop
across all instances.

When a shop registers again with new credentials, the token of the old credentials is
deleted once the registration is confirmed. When
the app is removed, call `sdk.ClearShopToken(ctx, shop)`; the Gin integration does
this for you.

Without an App, pass the same storage to `adminapi.Config.TokenStorage` when you create
a client with `adminapi.NewClient`.

See [Storing Admin API tokens](token-storage.md) for Valkey, key names, how long
tokens are kept, encryption and security tips.

## Logging

The SDK core does not log — it returns typed errors (see `sdkerrors`) and leaves
logging to the caller. Integrations log through the standard library `log/slog`. Pass
a `*slog.Logger` to the Gin integration; if omitted it uses a no-op logger. See
[ADR 0002](adr/0002-no-logging-in-core.md).

```go
import (
    "log/slog"
    "os"

    ginintegration "github.com/shopware/sdk-go/integrations/gin"
)

log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

// Gin integration with optional logging
ginintegration.Configure(router, ginintegration.Config{
    AppName:   "MyApp",
    AppSecret: "secret",
    ShopStore: store,
    Logger:    log, // Optional - defaults to a no-op slog.Logger
})
```

To plug in zap/zerolog/etc., use their `slog.Handler` bridge — the SDK depends on none
of them.

## Security Features

A summary of what the SDK checks for you. The threat model, the per-path freshness
rules and how to report a vulnerability are in [SECURITY.md](../SECURITY.md).

### Signature Verification
- HMAC-SHA256 signatures on all requests
- Constant-time comparison prevents timing attacks
- Separate secrets for registration (app secret) and webhooks (shop secret)

### Replay Attack Protection
- Timestamp validation with 5-minute window
- Requests outside window are rejected
- Applies to signed GET queries too (admin modules, action button tabs): an HMAC
  makes the timestamp tamper-proof but does not make it expire, so the SDK
  compares it against the current time. Configure via `app.Config.QueryPolicy`
  (`resolver.QueryPolicy`) and override per route with
  `Resolver.ResolveFromQueryWithPolicy` / `GetRequestVerifierWithPolicy`.
  See [Replay Protection](../SECURITY.md#replay-protection) for the per-path matrix.

### Shop State Enforcement
- Signed queries, signed POSTs and webhooks require an active shop (403 `shop_not_active`)
- Lifecycle webhooks (activate/deactivate/delete) intentionally accept inactive shops

### Freshness Per Path
Defaults differ because Shopware does not stamp every payload. Configure via
`app.Config` (`QueryPolicy`, `PostPolicy`, `WebhookPolicy`); the zero value of
each is that path's documented default.

| Path | Timestamp | Policy |
| --- | --- | --- |
| Signed GET query | required | `resolver.QueryPolicy` |
| Webhook | required | `webhook.Policy` |
| Signed JSON POST | checked if present | `resolver.PostPolicy` |
| Lifecycle webhook | not checked | — |

### Confirmation Security
- Confirmation requests verified before storing credentials
- Failed verifications automatically delete the shop
- Prevents unauthorized credential injection

### URL Sanitization
- Double slashes removed
- Trailing slashes removed
- Prevents URL-based attacks

### Token Management
- Thread-safe token acquisition
- Automatic token refresh
- Tokens stored per-shop

## Testing Your App

Use the in-memory store to test your handlers without external services:

```go
func TestMyHandler(t *testing.T) {
    // Create SDK
    sdk, err := app.New(app.Config{
        AppName:   "TestApp",
        AppSecret: "test-secret-min-8-chars",
        ShopStore: store.NewInMemoryStore(),
    })
    require.NoError(t, err)
    
    // Test your code
}
```

## Package Layout

Multi-module workspace (see [go.work](../go.work)):

```
sdk-go/                     - core module: github.com/shopware/sdk-go (no framework deps)
├── app/                    - Main SDK entry point and configuration
├── registration/           - Registration and confirmation handlers
├── resolver/               - Context resolver (shop resolution from requests)
├── response/               - Response builders for common types
├── sdkerrors/              - Unified error types and helpers
├── signature/              - HMAC signature verification and generation
├── store/                  - Shop persistence (memory, SQLite, DynamoDB)
├── webhook/                - Webhook verification and processing
├── clients/
│   └── adminapi/           - Admin API client module (github.com/shopware/sdk-go/clients/adminapi)
└── integrations/
    └── gin/                - Gin integration module (github.com/shopware/sdk-go/integrations/gin)
```

The core module logs nothing and depends on no web framework. Each integration is a
separate Go module with its own version line — see [RELEASING.md](RELEASING.md).
