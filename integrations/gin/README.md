# Gin Integration for Shopware App SDK

This module connects the Shopware App SDK and the [Gin web framework](https://github.com/gin-gonic/gin).

## Features

- **Automatic endpoint configuration** - All Shopware app endpoints are set up with a single function call
- **Signature verification** - Every incoming request is verified
- **Response signing** - Responses are signed automatically
- **Lifecycle webhooks** - Built-in handlers for app activation, deactivation and deletion
- **Context helpers** - Access the shop and the verified event in your handlers
- **CORS support** - Optional CORS middleware

## Installation

```bash
go get github.com/shopware/app-sdk-go/integrations/gin
```

## Quick Start

```go
package main

import (
    "log/slog"
    "os"

    "github.com/gin-gonic/gin"

    ginintegration "github.com/shopware/app-sdk-go/integrations/gin"
    "github.com/shopware/app-sdk-go/pkg/store"
)

func main() {
    // Create a structured logger (stdlib log/slog)
    log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
    
    // Create store
    shopStore := store.NewInMemoryStore()
    
    // Create Gin router
    router := gin.Default()
    
    // Configure all Shopware app endpoints
    err := ginintegration.Configure(router, ginintegration.Config{
        AppName:         "MyApp",
        AppSecret:       "my-app-secret-min-8-chars",
        ConfirmationURL: "https://myapp.com/app/register/confirm",
        ShopStore:       shopStore,
        Logger:          log,
        EnableCORS:      true,
    })
    if err != nil {
        log.Error("failed to configure Shopware app", slog.Any("error", err))
        os.Exit(1)
    }
    
    // Add custom webhook handler
    router.POST("/app/webhook/product", handleProductWebhook)
    
    router.Run(":8080")
}

func handleProductWebhook(c *gin.Context) {
    // Get shop from context (automatically set by middleware)
    shop, exists := ginintegration.GetShop(c)
    if !exists {
        c.JSON(400, gin.H{"error": "Shop not found"})
        return
    }
    
    // Get verified webhook event
    event, exists := ginintegration.GetVerifiedEvent(c)
    if !exists {
        c.JSON(400, gin.H{"error": "Event not found"})
        return
    }
    
    // Process webhook...
    c.Status(204)
}
```

## Automatically Configured Endpoints

When you call `Configure()`, the following endpoints are automatically set up:

| Method | Path | Description |
|--------|------|-------------|
| GET | `/app/register` | App registration endpoint |
| POST | `/app/register/confirm` | Registration confirmation |
| POST | `/app/activate` | App activation webhook |
| POST | `/app/deactivate` | App deactivation webhook |
| POST | `/app/delete` | App deletion webhook |

By default these paths use `BasePath`. If `RegistrationBasePath` is set,
registration endpoints use that base path.

All non-registration routes under the base paths automatically have:
- Signature verification
- Response signing
- Shop context injection
- Error handling

Registration endpoints verify signatures internally and do not use response signing.

## Configuration Options

### Using Individual Parameters

```go
import (
    "log/slog"
    "os"
)

log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

err := ginintegration.Configure(router, ginintegration.Config{
    AppName:         "MyApp",                                    // Required
    AppSecret:       "my-secret-min-8-chars",                   // Required (min 8 chars)
    ConfirmationURL: "https://myapp.com/app/register/confirm",  // Required
    ShopStore:       store,                                      // Required
    Logger:          log,                                        // Optional (uses no-op if not provided)
    BasePath:        "/app",                                     // Optional (default: "/app")
    RegistrationBasePath: "/internal/api/lifecycle",             // Optional (default: BasePath)
    EnableCORS:      true,                                       // Optional (default: false)
    MaxBodyBytes:    5 * 1024 * 1024,                            // Optional (default: 5 MiB; DoS guard, applied only when this integration builds the App)
})
```

### Using Pre-configured App Instance

```go
import (
    "log/slog"
    "os"

    "github.com/shopware/app-sdk-go/pkg/app"
)

log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

// Create App instance (the core SDK takes no logger)
sdk, err := app.New(app.Config{
    AppName:         "MyApp",
    AppSecret:       "my-secret-min-8-chars",
    ConfirmationURL: "https://myapp.com/app/register/confirm",
    ShopStore:       shopStore,
})

// Configure with App instance; logging happens in the integration
err = ginintegration.Configure(router, ginintegration.Config{
    App:    sdk,
    Logger: log,
})
```

## Adding Custom Webhooks

Custom webhook handlers automatically benefit from signature verification:

```go
// This route is automatically protected with signature verification
router.POST("/app/webhook/custom", func(c *gin.Context) {
    // Get shop from context
    shop, exists := ginintegration.GetShop(c)
    if !exists {
        c.JSON(400, gin.H{"error": "Shop not found"})
        return
    }
    
    // Get verified event
    event, exists := ginintegration.GetVerifiedEvent(c)
    if !exists {
        c.JSON(400, gin.H{"error": "Event not found"})
        return
    }
    
    // Log with structured fields
    log.Info("Processing custom webhook",
        slog.String("shop_id", shop.ShopID),
        slog.String("event", event.Data.Event),
    )
    
    // Your webhook logic here...
    
    c.Status(204)
})
```

## Adding Action Button Handlers

```go
import "github.com/shopware/app-sdk-go/pkg/response"

router.POST("/app/action/export-product", func(c *gin.Context) {
    shop, exists := ginintegration.GetShop(c)
    if !exists {
        c.JSON(400, gin.H{"error": "Shop not found"})
        return
    }
    
    // Your action logic here...
    
    // Return response using helpers
    c.JSON(200, response.NotificationSuccess("Product exported successfully"))
})
```

## Context Helpers

### GetShop

Retrieves the authenticated shop from the context:

```go
func handler(c *gin.Context) {
    shop, exists := ginintegration.GetShop(c)
    if !exists {
        c.JSON(400, gin.H{"error": "Shop not found"})
        return
    }
    
    // Use shop data
    log.Info("Processing request",
        slog.String("shop_id", shop.ShopID),
        slog.String("shop_url", shop.ShopURL),
    )
}
```

### GetVerifiedEvent

Retrieves the verified webhook event:

```go
import "github.com/shopware/app-sdk-go/pkg/webhook"

func webhookHandler(c *gin.Context) {
    event, exists := ginintegration.GetVerifiedEvent(c)
    if !exists {
        c.JSON(400, gin.H{"error": "Event not found"})
        return
    }
    
    // Entity webhooks (e.g. product.written) carry a list of changes
    var changes []webhook.EntityChange
    if err := event.Data.UnmarshalPayload(&changes); err != nil {
        c.JSON(400, gin.H{"error": "Invalid payload"})
        return
    }
    for _, change := range changes {
        log.Info("Entity change",
            slog.String("entity", change.Entity),
            slog.String("operation", change.Operation),
        )
    }
}
```

## Custom Base Paths

Change the base path for all Shopware endpoints:

```go
err := ginintegration.Configure(router, ginintegration.Config{
    AppName:   "MyApp",
    AppSecret: "my-secret-min-8-chars",
    ShopStore: store,
    BasePath:  "/shopware", // Changes all routes to /shopware/*
})
```

Use separate base paths for registration and lifecycle webhooks:

```go
err := ginintegration.Configure(router, ginintegration.Config{
    AppName:              "MyApp",
    AppSecret:            "my-secret-min-8-chars",
    ShopStore:            store,
    RegistrationBasePath: "/internal/api/lifecycle",
    BasePath:             "/api/lifecycle",
})
```

## CORS Support

Enable CORS for cross-origin requests:

```go
err := ginintegration.Configure(router, ginintegration.Config{
    AppName:    "MyApp",
    AppSecret:  "my-secret-min-8-chars",
    ShopStore:  store,
    EnableCORS: true, // Enables CORS middleware
})
```

## Error Handling

The integration automatically handles SDK errors and returns appropriate HTTP responses:

```go
import sdkerrors "github.com/shopware/app-sdk-go/pkg/errors"

// SDK errors are automatically converted to HTTP responses:
// - TypeInvalidSignature    -> 401 Unauthorized
// - TypeShopNotFound        -> 404 Not Found
// - TypeShopNotActive       -> 403 Forbidden
// - TypeTimestampExpired    -> 401 Unauthorized
// - TypeInvalidRequest      -> 400 Bad Request
// - TypeRegistrationState   -> 409 Conflict
// - TypeVerificationFailed  -> 401 Unauthorized
// - TypeInvalidPayload      -> 400 Bad Request
// - Internal errors         -> 500 Internal Server Error
```

## Logging

The integration logs through the standard library `log/slog`. Pass any `*slog.Logger`;
it depends on no third-party logging library. To use zap/zerolog/etc., wire them via
their `slog.Handler` bridge.

```go
import (
    "log/slog"
    "os"
)

// Production logging (JSON)
log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

// Development logging (text)
log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

// No-op logger (testing / default when Logger is nil)
log := slog.New(slog.DiscardHandler)

// Use in handlers
log.Info("Processing webhook",
    slog.String("shop_id", shop.ShopID),
    slog.String("event", event.Data.Event),
)
```

## Example: Complete Application

See [`examples/gin-service/`](../../examples/gin-service/) for a complete example application.

## Testing

Run the tests from the module root:

```bash
cd integrations/gin && go test ./...
```

## Security

All security features are enabled by default:
- HMAC-SHA256 signature verification
- Constant-time signature comparison
- Replay attack protection (5-minute timestamp window)
- Automatic shop secret validation
- URL sanitization
- Request body size limit before authentication (`MaxBodyBytes`, default 5 MiB) — mitigates memory-exhaustion DoS; oversized requests are rejected with HTTP 413 and logged

## Performance

See [Benchmarks](../../docs/benchmarks.md) for the overhead of the registration and
webhook endpoints.

## License

MIT, see [LICENSE](../../LICENSE).
