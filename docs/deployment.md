# Deployment And Storage

Shopware apps need stable HTTPS endpoints, persistent shop storage, and a clear token/cache strategy. The SDK keeps these choices explicit.

## Runtime Requirements

- Go 1.26 or newer. The SDK supports the two most recent major Go releases
  (currently Go 1.26 and 1.27); see the Go version support policy in the README.
  Build production binaries with the **latest patch release** of a supported
  line — standard-library CVEs are fixed by the toolchain, not by dependency
  upgrades. The exact recommended patch is listed under
  [Requirements](../README.md#requirements); pinning a base image to
  `golang:1.26-alpine` (major line, no patch) keeps it current automatically.
- Always expose Shopware app endpoints via HTTPS instead of HTTP. Use HTTP only for local-only development examples.
- A persistent `store.ShopStore` outside local development.
- App secrets stored in deployment secrets, not source code.
- Request bodies preserved until signature verification completes.

## Store Options

| Store | Use for | Notes |
| --- | --- | --- |
| In-memory | Tests and local demos | Data is lost on restart; do not use for production |
| SQLite | Small single-instance services | Pure Go driver (`modernc.org/sqlite`); no CGO needed |
| DynamoDB | Distributed services | Requires AWS credentials, table provisioning, and network access |

The store interface is intentionally small so applications can add Redis, Postgres, or another backend without changing SDK callers.

## Admin API Tokens

The SDK keeps the Admin API tokens of all shops in memory by default. That is fine for
one instance. With several instances, set `app.Config.TokenStorage` to an external store
such as Redis or Valkey, so they share one token per shop. See
[Storing Admin API tokens](token-storage.md).

## SQLite

The SQLite store uses `modernc.org/sqlite`, a pure Go driver. Builds work with
`CGO_ENABLED=0`, so the SDK fits into static binaries and minimal container images
without a C toolchain.

SQLite keeps data in a local file. Use it for a single instance with a persistent volume;
for several instances use a shared store such as DynamoDB.

## Framework Integration

Framework adapters are split out of the core dependency graph. The core module
(`github.com/shopware/app-sdk-go`) pulls in no web framework; the Gin adapter lives in
its own module, `github.com/shopware/app-sdk-go/integrations/gin`, and is installed only
if you need it. Apps that do not use Gin therefore carry no Gin dependencies.

## Production Checklist

- Use HTTPS URLs for all externally exposed endpoints in `manifest.xml`.
- Use the same app secret in `manifest.xml` and deployment configuration.
- Use a persistent store.
- With several instances, keep Admin API tokens in a shared, protected store
  ([token storage](token-storage.md)).
- Configure request timeouts and graceful shutdown in the host application.
- Capture application logs outside the SDK; the core SDK does not force logging.
- Add retry middleware only around operations that should be retried.
- Confirm the `compatibility` range in example manifests before publishing an app.
