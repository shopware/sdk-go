# Gin Service Example - Using Integration

This example demonstrates the **recommended approach** for building Shopware apps with the Gin integration. All Shopware endpoints (registration, confirmation, webhooks) are configured automatically with one `Configure` call.

**For comparison with manual setup, see the [vanilla-service example](../vanilla-service/).**

## Features

- **Shopware integration set up with one `Configure` call**
- Automatic endpoint configuration (register, confirm, activate, deactivate, delete)
- Automatic signature verification and response signing
- Full registration flow
- Webhook processing with custom handlers
- Admin API calls for changed entities (`clients/adminapi`)
- Admin API token storage in memory, Redis or Valkey (shared between instances)
- Health check endpoint
- Graceful shutdown
- Structured logging with slog
- Environment-based configuration
- Support for in-memory and DynamoDB stores
- Consistent error handling
- Docker support

## Why Use the Integration?

| Manual Setup (vanilla-service) | With Gin Integration (this example) |
|-------------------------------|-------------------------------------|
| Hand-written registration and lifecycle handlers | One `Configure` call |
| Manual signature verification | Automatic verification |
| Manual response signing | Automatic signing |
| Manual context extraction | Context helpers provided |
| Easy to make security mistakes | Security built-in |

## Quick Start

### 1. Install Dependencies

```bash
go mod download
```

### 2. Configure Environment

```bash
cp .env.example .env
# Edit .env with your configuration
```

**Required configuration:**
- `SHOPWARE_APP_SECRET`: Your app secret from manifest.xml (REQUIRED)
- `SHOPWARE_APP_NAME`: Your app name
- `SHOPWARE_CONFIRMATION_URL`: URL where Shopware sends confirmation

### 3. Run the Service

```bash
# Load environment variables and run
source .env && go run .
```

Or use a tool like `godotenv`:

```bash
go install github.com/joho/godotenv/cmd/godotenv@latest
godotenv -f .env go run .
```

The service will start on `http://localhost:8080`.

## Installing in Shopware

### 1. Update manifest.xml

Edit `manifest.xml` to match your deployment:

```xml
<setup>
    <registrationUrl>https://your-domain.com/app/register</registrationUrl>
    <secret>your-app-secret-here</secret>
</setup>
```

Update all webhook URLs to use your domain:

```xml
<webhooks>
    <webhook name="product-written" url="https://your-domain.com/app/webhook/product" event="product.written"/>
</webhooks>
```

**Important:** The `secret` in manifest.xml must match your `SHOPWARE_APP_SECRET` environment variable.

### 2. Prepare App Directory

Create a directory structure for your app:

```
custom/apps/GinShopwareApp/
└── manifest.xml
```

### 3. Install via Shopware Admin

1. Go to **Extensions → My extensions → Apps**
2. Click **Upload app** and upload the `GinShopwareApp` folder (zipped)
3. Or place the folder in `custom/apps/` and click **Reload apps**
4. Click **Install** on your app
5. Click **Activate** to complete the registration flow

The registration flow will:
1. Shopware sends a GET request to your `/app/register` endpoint
2. Your app responds with proof and secret
3. Shopware sends a POST request to `/app/register/confirm` with credentials
4. Your app stores the credentials and responds with 204
5. The app is now active and will receive webhooks

### 4. Verify Installation

Check your app logs to see:
- Registration request received
- Confirmation request received
- Shop activated successfully

Test webhooks by making changes in Shopware (e.g., edit a product).

## Endpoints

### Health Check
```
GET /health
```

Returns the service health status.

**Response (200 OK):**
```json
{
  "status": "healthy",
  "timestamp": 1234567890
}
```

### Registration
```
GET /app/register?shop-id=<id>&shop-url=<url>&timestamp=<ts>
Header: shopware-app-signature: <hmac-signature>
```

Handles app registration from Shopware.

**Response (200 OK):**
```json
{
  "proof": "...",
  "secret": "...",
  "confirmation_url": "http://localhost:8080/app/register/confirm"
}
```

### Confirmation
```
POST /app/register/confirm
Header: Shopware-Shop-Signature: <hmac-signature>
Content-Type: application/json

{
  "shopId": "...",
  "shopUrl": "...",
  "apiKey": "...",
  "secretKey": "...",
  "timestamp": "..."
}
```

Confirms app registration.

**Response:** 204 No Content

### Webhook
```
POST /app/webhook/product
Header: Shopware-Shop-Signature: <hmac-signature>
Content-Type: application/json

{
  "source": {
    "shopId": "...",
    "url": "..."
  },
  "data": {
    "event": "product.written",
    "payload": [...]
  }
}
```

Handles webhook events from Shopware.

**Response:** 204 No Content

## Calling the Admin API

For every inserted or updated product, the webhook handler loads the product from the
shop's Admin API (`processEntityChange` in `main.go`):

```go
client, err := sdk.NewClientForShop(shop)
path := adminapi.Path(strings.ReplaceAll(change.Entity, "_", "-"), change.PrimaryKey)
resp, err := client.Get(ctx, path)
if adminapi.IsNotFound(err) {
    // deleted in the meantime
}
```

- The work runs after the webhook was answered, so it uses
  `context.WithoutCancel(c.Request.Context())` plus its own timeout.
- `adminapi.Path` escapes every segment, so IDs from the payload cannot change the path.
- All clients of a shop share one OAuth token (see below).

## Token Storage: Memory, Redis, Valkey

The App keeps one Admin API token per shop and shares it between all clients. Where the
tokens live is set with `TOKEN_STORAGE`:

- `memory` (default): tokens live in this process. Fine for a single instance.
- `redis`: tokens live in Redis or **Valkey** (same protocol, same client). Use it when
  you run several instances, so they share one token per shop instead of each fetching
  its own.

`tokenstorage.go` shows how little is needed: three methods on top of `go-redis`
(`Get`, `Set` with a TTL, `Delete`) wrapped with `tokenstorage.NewKV`. The SDK itself has
no Redis dependency; only this example does.

```go
type redisStore struct{ rdb redis.UniversalClient }

func (s redisStore) Get(ctx context.Context, key string) ([]byte, error) {
    v, err := s.rdb.Get(ctx, key).Bytes()
    if errors.Is(err, redis.Nil) {
        return nil, adminstorage.ErrNotFound
    }
    return v, err
}
// Set and Delete: SET key value PX ttl / DEL key

app.Config{TokenStorage: adminstorage.NewKV(redisStore{rdb}, adminstorage.WithKeyPrefix(prefix))}
```

Tokens are credentials. Set `TOKEN_ENCRYPTION_KEY` (generate it with
`openssl rand -base64 32`) to store them encrypted with AES-GCM, and protect the store
with a password and network access rules.

`docker compose up` starts the service together with Valkey and `TOKEN_STORAGE=redis`.
Check the stored tokens with `docker compose exec valkey valkey-cli keys 'gin-service:token:*'`.

More options (the `valkey-go` driver, key names, TTL, security tips) are in
[Storing Admin API tokens](../../docs/token-storage.md).

## Configuration

### Environment Variables

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `SHOPWARE_APP_NAME` | App name from manifest.xml | GinShopwareApp | No |
| `SHOPWARE_APP_SECRET` | App secret from manifest.xml | - | **Yes** |
| `SHOPWARE_CONFIRMATION_URL` | Confirmation endpoint URL | http://localhost:8080/app/register/confirm | No |
| `SERVER_ADDR` | Server listen address | :8080 | No |
| `ENVIRONMENT` | Environment (development/production) | development | No |
| `STORE_TYPE` | Shop store type (sqlite/memory/dynamodb) | sqlite | No |
| `DATABASE_PATH` | SQLite database path (when `STORE_TYPE=sqlite`) | ./data/shops.db | No |
| `DYNAMODB_TABLE` | DynamoDB table name | shopware-shops | No |
| `DYNAMODB_REGION` | AWS region | us-east-1 | No |
| `DYNAMODB_ENDPOINT` | Custom DynamoDB endpoint, e.g. DynamoDB Local | - | No |
| `TOKEN_STORAGE` | Admin API token storage (`memory`/`redis`) | memory | No |
| `REDIS_ADDR` | Redis or Valkey address (when `TOKEN_STORAGE=redis`) | localhost:6379 | No |
| `REDIS_PASSWORD` | Redis or Valkey password | - | No |
| `TOKEN_KEY_PREFIX` | Prefix of the token keys | gin-service:token: | No |
| `TOKEN_ENCRYPTION_KEY` | Encrypts tokens at rest (base64 of 32 bytes) | - | No |

### Production Configuration

For production deployment:

1. **Set environment to production:**
   ```bash
   ENVIRONMENT=production
   ```

2. **Use DynamoDB for persistence:**
   ```bash
   STORE_TYPE=dynamodb
   DYNAMODB_TABLE=shopware-shops
   DYNAMODB_REGION=us-east-1
   ```

3. **Expose Shopware app endpoints with HTTPS:**
   - Deploy behind a reverse proxy (nginx, Caddy)
   - Or use TLS directly in Go

4. **Set proper confirmation URL:**
   ```bash
   SHOPWARE_CONFIRMATION_URL=https://your-domain.com/app/register/confirm
   ```

## Testing

### Manual Testing with cURL

**1. Test Health Check:**
```bash
curl http://localhost:8080/health
```

**2. Test Registration (requires valid signature):**
```bash
# Compute HMAC signature first
QUERY="shop-id=test-shop&shop-url=https://test.com&timestamp=$(date +%s)"
SECRET="your-app-secret"
SIGNATURE=$(echo -n "$QUERY" | openssl dgst -sha256 -hmac "$SECRET" | sed 's/^.* //')

curl "http://localhost:8080/app/register?$QUERY" \
  -H "shopware-app-signature: $SIGNATURE"
```

**3. Test Webhook (after registration):**
```bash
# Use shop secret from registration response
SHOP_SECRET="<secret-from-registration>"

BODY='{"source":{"shopId":"test-shop","url":"https://test.com"},"data":{"event":"product.written","payload":[{"entity":"product","operation":"update","primaryKey":"test-uuid","updatedFields":["name"]}]},"timestamp":'$(date +%s)'}'

SIGNATURE=$(echo -n "$BODY" | openssl dgst -sha256 -hmac "$SHOP_SECRET" | sed 's/^.* //')

curl -X POST http://localhost:8080/app/webhook/product \
  -H "Content-Type: application/json" \
  -H "Shopware-Shop-Signature: $SIGNATURE" \
  -d "$BODY"
```

## Project Structure

```
gin-service/
├── main.go              # Main application
├── tokenstorage.go      # Admin API token storage (memory, Redis/Valkey, encryption)
├── tokenstorage_test.go # Token storage tests (miniredis)
├── go.mod               # Go modules
├── env.example          # Environment template
├── docker-compose.yml   # Service + Valkey
├── README.md            # This file
└── Dockerfile           # Docker image (optional)
```

## Docker Deployment

Run the service with Valkey as token storage:

```bash
docker compose up --build
```

Or build and run the image yourself. The image downloads the published SDK modules, so
the build context is just this directory:

```bash
# Build image (from examples/gin-service)
docker build -t shopware-gin-app .

# Run container
docker run -p 8080:8080 \
  -e SHOPWARE_APP_SECRET=your-secret \
  -e SHOPWARE_CONFIRMATION_URL=https://your-domain.com/app/register/confirm \
  -e STORE_TYPE=memory \
  shopware-gin-app
```

## Extending the Service

### Add Custom Webhook Handlers

Modify the `processEntityChange` function to handle specific entity types:

```go
func processEntityChange(ctx context.Context, sdk *app.App, shop *store.Shop, change webhook.EntityChange, logger *slog.Logger) {
    switch change.Entity {
    case "product":
        handleProductChange(ctx, sdk, logger, shop, change)
    case "order":
        handleOrderChange(ctx, sdk, logger, shop, change)
    default:
        logger.Debug("Unhandled entity type",
            slog.String("entity", change.Entity))
    }
}

func handleProductChange(ctx context.Context, sdk *app.App, logger *slog.Logger, shop *store.Shop, change webhook.EntityChange) {
    client, err := sdk.NewClientForShop(shop)
    if err != nil {
        logger.Error("Failed to create client", slog.Any("error", err))
        return
    }

    // Fetch product details
    resp, err := client.Get(ctx, adminapi.Path("product", change.PrimaryKey))
    if adminapi.IsNotFound(err) {
        return // deleted in the meantime
    }
    if err != nil {
        logger.Error("Failed to fetch product", slog.Any("error", err))
        return
    }

    var product struct {
        Data map[string]any `json:"data"`
    }
    if err := resp.JSON(&product); err != nil {
        logger.Error("Failed to parse product", slog.Any("error", err))
        return
    }

    // Do something with the product...
    logger.Info("Product processed", slog.String("id", change.PrimaryKey))
}
```

### Add Action Button Handlers

```go
router.POST("/action-button/:action", handleActionButton(sdk))

func handleActionButton(sdk *app.App) gin.HandlerFunc {
    return func(c *gin.Context) {
        action := c.Param("action")
        
        // Parse action button request
        // Verify signature
        // Process action
        // Return response
        
        c.JSON(http.StatusOK, response.NotificationSuccess(
            "Action completed successfully",
        ))
    }
}
```

## Troubleshooting

### Signature Verification Failed

- Check that `SHOPWARE_APP_SECRET` matches your manifest.xml
- Verify query parameter order in registration requests
- Ensure body is not modified before signature verification
- Check timestamp is within 5-minute window

### Shop Not Found

- Complete registration flow first
- Check shop is in "active" status
- Verify shop ID matches

### Store Connection Issues

If using DynamoDB:
- Verify AWS credentials are configured
- Check DynamoDB table exists
- Verify IAM permissions

## Monitoring

The service logs all requests and errors using structured logging (slog). In production, send logs to your monitoring system:

- CloudWatch (AWS)
- Datadog
- New Relic
- Grafana Loki
- Elasticsearch

Example log output:
```json
{
  "level": "info",
  "ts": 1234567890.123,
  "msg": "HTTP request",
  "method": "POST",
  "path": "/app/webhook/product",
  "status": 204,
  "latency": "15ms",
  "ip": "192.168.1.1"
}
```

## Security Considerations

1. **Always expose Shopware app endpoints via HTTPS instead of HTTP**
2. **Never commit .env files with secrets**
3. **Use DynamoDB or another persistent store in production**
4. **Implement rate limiting for public endpoints**
5. **Monitor and alert on signature verification failures**
6. **Rotate app secrets regularly**
7. **Use IAM roles instead of AWS credentials where possible**

## Support

For issues with the SDK, see the main [SDK documentation](../../README.md).

For Shopware app development, see the [Shopware App Documentation](https://developer.shopware.com/docs/guides/plugins/apps/).
