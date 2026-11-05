# Shopware App SDK for Go

[![CI](https://github.com/shopware/app-sdk-go/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/shopware/app-sdk-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/shopware/app-sdk-go.svg)](https://pkg.go.dev/github.com/shopware/app-sdk-go)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Go SDK for building [Shopware 6 apps](https://developer.shopware.com/docs/guides/plugins/apps/):
app registration, signed webhooks and requests, shop storage and an Admin API client.

## Features

- **Secure registration** - HMAC-SHA256 signature verification with replay protection
- **Webhook handling** - Signature validation and event parsing
- **Storage backends** - In-memory, SQLite and DynamoDB behind one interface
- **Admin API client** - OAuth token management with pluggable token storage
- **Response builders** - Typed builders for notifications, redirects and payments
- **Gin integration** - All app endpoints with one call
- **Structured errors** - Error types with HTTP status codes
- **Minimal core** - No forced logging, retries or caching; add them as middleware when needed

## Installation

This repository is a multi-module Go workspace. The core SDK is framework-agnostic
and pulls no HTTP-framework dependencies; framework integrations are separate modules
that you install only if you need them.

Core SDK (no framework dependencies):

```bash
go get github.com/shopware/app-sdk-go
```

Gin integration (pulls in Gin only here):

```bash
go get github.com/shopware/app-sdk-go/integrations/gin
```

Admin API client only (no app-system code, no storage dependencies). The SDK uses it
under the hood; install it on its own for CLI tools, workers or integrations:

```bash
go get github.com/shopware/app-sdk-go/clients/adminapi
```

Upgrading from an older version? See [docs/MIGRATION.md](docs/MIGRATION.md).

## Quick Start with Gin (Recommended)

The fastest way to get started is using the Gin integration:

```go
package main

import (
    "log"
    
    "github.com/gin-gonic/gin"
    ginintegration "github.com/shopware/app-sdk-go/integrations/gin"
    "github.com/shopware/app-sdk-go/pkg/store"
)

func main() {
    // Setup store (in-memory for development, use SQLite/DynamoDB for production)
    shopStore := store.NewInMemoryStore()
    
    // Create router
    router := gin.Default()
    
    // Configure all Shopware endpoints with ONE line
    err := ginintegration.Configure(router, ginintegration.Config{
        AppName:         "MyApp",
        AppSecret:       "your-app-secret-min-8-chars",
        ConfirmationURL: "https://myapp.com/app/register/confirm",
        ShopStore:       shopStore,
        EnableCORS:      true,
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // Add custom webhook handlers (automatically protected)
    router.POST("/app/webhook/product", func(c *gin.Context) {
        shop, _ := ginintegration.GetShop(c)
        event, _ := ginintegration.GetVerifiedEvent(c)
        
        // Process your webhook
        log.Printf("Processing product webhook for shop %s: %s", shop.ShopID, event.Data.Event)
        
        c.Status(204)
    })
    
    log.Fatal(router.Run(":8080"))
}
```

**What's automatically configured:**
- `GET /app/register` - Registration endpoint
- `POST /app/register/confirm` - Confirmation endpoint
- `POST /app/activate` - App activation webhook
- `POST /app/deactivate` - App deactivation webhook
- `POST /app/delete` - App deletion webhook
- Signature verification for all routes
- Response signing
- CORS support (optional)

## Without Gin

The core SDK works with `net/http` or any other router: create an `app.App` and call its
registration handler, resolver and webhook middleware from your handlers. See
[Manual Routing](docs/getting-started.md#manual-routing) for a complete example.

## Admin API Client

Call the Shopware Admin API on behalf of a registered shop. All clients of a shop share
its OAuth token:

```go
// shop comes from a verified webhook or signed request
client, err := sdk.NewClientForShop(shop)
if err != nil {
    return err
}

resp, err := client.Post(ctx, "/search/product", map[string]any{"limit": 10})
if err != nil {
    return err
}
```

The client is also available as a standalone module for CLI tools and workers. See the
[Admin API client README](clients/adminapi/README.md) and the
[Usage Guide](docs/guide.md#admin-api-client).

## Documentation

| Guide | Content |
|---|---|
| [Getting Started](docs/getting-started.md) | First app with Gin or `net/http`, signed GET requests |
| [Usage Guide](docs/guide.md) | Admin API client, response builders, storage, errors, retries, logging, security features |
| [Deployment](docs/deployment.md) | Stores, runtime requirements, production checklist |
| [Storing Admin API Tokens](docs/token-storage.md) | Redis, Valkey, key names, encryption |
| [Migration Guide](docs/MIGRATION.md) | Upgrading between versions |
| [Testing](docs/testing.md) and [Benchmarks](docs/benchmarks.md) | Checks and performance baselines |
| [Security Policy](SECURITY.md) | Threat model, replay protection, reporting vulnerabilities |
| [Architecture decisions](docs/adr/) | Why the SDK is built the way it is |

API reference: [pkg.go.dev](https://pkg.go.dev/github.com/shopware/app-sdk-go).

## Examples

Complete working examples are available:

- **[vanilla-service](examples/vanilla-service/)** - Basic example using `net/http`; loads changed entities through the Admin API
- **[gin-service](examples/gin-service/)** - Gin integration with SQLite or DynamoDB; Admin API tokens in memory, Redis or Valkey
- **[admin-extension](examples/admin-extension/)** - Admin UI extension with Vue.js; dashboard numbers from the Admin API

## Requirements

- Go 1.26 or newer. Build with the latest patch release of your Go line: standard-library
  security fixes ship only in Go patch releases. See [SECURITY.md](SECURITY.md#go-toolchain).
- HTTPS endpoint for production (required by Shopware).
- AWS credentials when you use the DynamoDB store.

### Go version support policy

Following the [Go release policy](https://go.dev/doc/devel/release#policy), the SDK
supports the **two most recent major Go releases** — currently **Go 1.26 and Go 1.27**.
When a new major Go release ships, support for the oldest of the two is dropped and the
`go` directive in `go.mod` may be raised accordingly. CI verifies the SDK against every
supported version.

## Versioning

The repository contains three Go modules, each with its own version and tags:

| Module | Import path |
|---|---|
| Core SDK | `github.com/shopware/app-sdk-go` |
| Gin integration | `github.com/shopware/app-sdk-go/integrations/gin` |
| Admin API client | `github.com/shopware/app-sdk-go/clients/adminapi` |

All modules follow [Semantic Versioning](https://semver.org/). Breaking changes to a
stable (`v1+`) module are released only as a new major version, after a deprecation
period. Release notes are published on
[GitHub Releases](https://github.com/shopware/app-sdk-go/releases); how releases are made
is described in [docs/RELEASING.md](docs/RELEASING.md).

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for the development
setup, the checks to run and the pull request guidelines.

## Security

Please report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

## License

MIT License - see [LICENSE](LICENSE) file for details

## Support

- **Issues**: [GitHub Issues](https://github.com/shopware/app-sdk-go/issues)
- **Documentation**: [Shopware App Documentation](https://developer.shopware.com/docs/guides/plugins/apps/)
- **Community**: [Shopware Community Slack](https://slack.shopware.com/)
