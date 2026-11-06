# Vanilla Service Example

This example demonstrates a basic Shopware app implementation using manual SDK setup without framework integrations. 

**For a simplified approach using the Gin integration, see the [gin-service example](../gin-service/).**

## Features

- ✅ Registration endpoint with signature verification
- ✅ Confirmation endpoint with security checks
- ✅ Webhook handler with event processing
- ✅ Health check endpoint
- ✅ Production-grade logging
- ✅ Error handling

## Running the Example

```bash
cd examples/vanilla-service
go run main.go
```

The app will start on `http://localhost:3000`.

## Installing in Shopware

### 1. Update manifest.xml

Edit `manifest.xml` to match your deployment:

```xml
<setup>
    <registrationUrl>https://your-domain.com/register</registrationUrl>
    <secret>your-app-secret-here</secret>
</setup>
```

Update all webhook URLs to use your domain.

**Important:** The `secret` in manifest.xml must match your `SHOPWARE_APP_SECRET` environment variable.

### 2. Install the App

1. Create app directory: `custom/apps/SimpleShopwareApp/`
2. Place `manifest.xml` in the directory
3. In Shopware Admin: **Extensions → My extensions → Apps**
4. Click **Reload apps** or upload the zipped folder
5. Install and activate the app

The registration flow will complete automatically, and your app will start receiving webhooks.

## Endpoints

### Registration
```
GET /register?shop-id=<id>&shop-url=<url>&timestamp=<ts>
Header: shopware-app-signature: <hmac-signature>
```

### Confirmation
```
POST /register/confirm
Header: Shopware-Shop-Signature: <hmac-signature>
Body: {
  "shopId": "...",
  "shopUrl": "...",
  "apiKey": "...",
  "secretKey": "...",
  "timestamp": "..."
}
```

### Webhook
```
POST /webhook
Header: Shopware-Shop-Signature: <hmac-signature>
Body: {
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

### Health Check
```
GET /health
```

## Calling the Admin API

For every inserted or updated entity, the webhook handler loads the entity from the
shop's Admin API (`fetchEntityDetails` in `main.go`):

```go
client, err := sdk.NewClientForShop(shop)
path := adminapi.Path(strings.ReplaceAll(change.Entity, "_", "-"), change.PrimaryKey)
resp, err := client.Get(ctx, path)
if adminapi.IsNotFound(err) {
    // deleted in the meantime
}
```

- The work runs after the webhook was answered, so it uses
  `context.WithoutCancel(r.Context())` plus its own timeout.
- `adminapi.Path` escapes every segment, so IDs from the payload cannot change the path.
- The app needs read permissions for the entities (see `manifest.xml`).
- Tokens are kept in memory and shared by all clients of a shop. To share them between
  several instances, set `app.Config.TokenStorage` — the
  [gin-service example](../gin-service/README.md#token-storage-memory-redis-valkey) shows
  Redis and Valkey.

## Configuration

Update the following values in `main.go`:

```go
sdk, err := app.New(app.Config{
    AppName:         "SimpleApp",          // Must match manifest.xml
    AppSecret:       "your-app-secret-from-manifest-min8chars", // Must match manifest.xml
    ConfirmationURL: "http://localhost:3000/register/confirm",
    ShopStore:       shopStore,
})
if err != nil {
    return fmt.Errorf("failed to create SDK: %w", err)
}
```

## Testing Without Shopware

### Manual Testing with cURL

**1. Registration (simulate Shopware request):**

```bash
# Compute HMAC signature first
QUERY="shop-id=test-shop&shop-url=https://test.com&timestamp=1234567890"
SECRET="your-app-secret"
SIGNATURE=$(echo -n "$QUERY" | openssl dgst -sha256 -hmac "$SECRET" | sed 's/^.* //')

# Make request
curl -v "http://localhost:3000/register?$QUERY" \
  -H "shopware-app-signature: $SIGNATURE"
```

**2. Confirmation (after registration):**

```bash
# Get shop secret from registration response
SHOP_SECRET="<secret-from-registration>"

# Create body
BODY='{"shopId":"test-shop","shopUrl":"https://test.com","apiKey":"test-api-key","secretKey":"test-secret-key","timestamp":"'$(date +%s)'"}'

# Compute signature
SIGNATURE=$(echo -n "$BODY" | openssl dgst -sha256 -hmac "$SHOP_SECRET" | sed 's/^.* //')

# Make request
curl -v -X POST http://localhost:3000/register/confirm \
  -H "Content-Type: application/json" \
  -H "Shopware-Shop-Signature: $SIGNATURE" \
  -d "$BODY"
```

**3. Webhook (after confirmation):**

```bash
# Use shop secret from registration
SHOP_SECRET="<secret-from-registration>"

# Create webhook payload
BODY='{"source":{"shopId":"test-shop","url":"https://test.com"},"data":{"event":"product.written","payload":[{"entity":"product","operation":"update","primaryKey":"test-uuid","updatedFields":["name"]}]},"timestamp":'$(date +%s)'}'

# Compute signature
SIGNATURE=$(echo -n "$BODY" | openssl dgst -sha256 -hmac "$SHOP_SECRET" | sed 's/^.* //')

# Make request
curl -v -X POST http://localhost:3000/webhook \
  -H "Content-Type: application/json" \
  -H "Shopware-Shop-Signature: $SIGNATURE" \
  -d "$BODY"
```

## Manifest.xml

Your Shopware app needs a `manifest.xml` file:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<manifest xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
          xsi:noNamespaceSchemaLocation="https://raw.githubusercontent.com/shopware/platform/trunk/src/Core/Framework/App/Manifest/Schema/manifest-1.0.xsd">
    <meta>
        <name>SimpleApp</name>
        <label>Simple App</label>
        <label lang="de-DE">Simple App</label>
        <description>A simple Shopware app example</description>
        <author>Your Name</author>
        <copyright>(c) 2025</copyright>
        <version>1.0.0</version>
        <compatibility>&gt;=6.6.0.0 &lt;6.8.0.0</compatibility>
        <license>MIT</license>
    </meta>

    <setup>
        <registrationUrl>http://localhost:3000/register</registrationUrl>
        <secret>your-app-secret</secret>
    </setup>

    <permissions>
        <read>product</read>
        <read>order</read>
    </permissions>

    <webhooks>
        <webhook name="product-written" url="http://localhost:3000/webhook" event="product.written"/>
        <webhook name="order-placed" url="http://localhost:3000/webhook" event="order.placed.order"/>
    </webhooks>
</manifest>
```

The `compatibility` range follows the SDK policy of supporting the two latest Shopware major release lines.

## Production Deployment

For production, make these changes:

1. **Use DynamoDB or SQLite Store:**
```go
// DynamoDB (recommended for production)
shopStore, err := store.NewDynamoDBStore(ctx, store.DynamoDBConfig{
    TableName: "shopware-shops",
    Region:    "us-east-1",
})

// Or SQLite (for single-instance deployments)
shopStore, err := store.NewSQLiteStore(store.SQLiteConfig{
    DatabasePath: "/var/lib/shopware-app/shops.db",
})
```

2. **Use a Production Logger (stdlib slog):**
```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
```

3. **Expose Shopware app endpoints with HTTPS:**
```go
ConfirmationURL: "https://your-app.com/register/confirm"
```

4. **Use Environment Variables:**
```go
appSecret := os.Getenv("SHOPWARE_APP_SECRET")
confirmURL := os.Getenv("SHOPWARE_CONFIRMATION_URL")
```

5. **Add TLS:**
```go
http.ListenAndServeTLS(":443", "cert.pem", "key.pem", nil)
```

6. **Optional: Add Retry Middleware for DynamoDB:**
```go
import "github.com/shopware/sdk-go/store/middleware"

baseStore, _ := store.NewDynamoDBStore(ctx, store.DynamoDBConfig{...})
shopStore := middleware.NewRetryMiddleware(baseStore, middleware.RetryConfig{
    MaxRetries: 3,
    BaseDelay:  100 * time.Millisecond,
})
```

## Next Steps

- Add custom business logic to webhook handler
- Implement action button handlers
- Add more Admin API calls (see `clients/adminapi`) to sync data with Shopware
- Set up proper logging and monitoring
- Deploy to production environment

## Troubleshooting

### Signature Verification Failed

- Check that app secret matches manifest.xml
- Verify query parameter order in registration
- Ensure body is not modified before signature verification
- Check timestamp is within 5-minute window

### Shop Not Found

- Complete registration flow first
- Check shop is in "active" status
- Verify shop ID matches

### Token Acquisition Failed

- Check API credentials are correct
- Verify shop URL is accessible
- Check OAuth endpoint is available

## Learn More

- [Shopware App Documentation](https://developer.shopware.com/docs/guides/plugins/apps/)
- [SDK Documentation](../../README.md)
- [Webhook Events](https://developer.shopware.com/docs/resources/references/app-reference/webhook-events-reference)
