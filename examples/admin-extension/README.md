# Admin Extension Example

A comprehensive example demonstrating how to build a Shopware app with an admin UI extension using the Go SDK and Vue.js.

## What This Example Shows

This example demonstrates:

1. **App Lifecycle Management** - Registration, activation, deactivation, deletion webhooks
2. **Admin UI Extension** - Custom admin module with Vue.js frontend embedded in Shopware admin
3. **Frontend Authentication** - Secure token-based authentication between iframe and backend
4. **Dynamic Sidebar Registration** - Using Meteor Admin SDK to register menu items
5. **Context Resolution** - SDK's Resolver for verifying Shopware signatures
6. **API Endpoints** - RESTful API for frontend-backend communication with token validation
7. **Local Development Setup** - Docker Compose with host-accessible local URLs

## Architecture

```
┌────────────────────────────────────────────────────────────┐
│                      Shopware Admin                        │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  Sidebar: "Extension Demo"                           │  │
│  │  ┌────────────────────────────────────────────────┐  │  │
│  │  │                                                │  │  │
│  │  │         Vue.js Frontend (iframe)               │  │  │
│  │  │         (Meteor Admin SDK)                     │  │  │
│  │  │         http://host.docker.internal:5173       │  │  │
│  │  │                                                │  │  │
│  │  └────────────────────────────────────────────────┘  │  │
│  │                        │                             |  │
│  └─────────────────────── │─────────────────────────────┘  │
└────────────────────────── │────────────────────────────────┘
                            │
                     1. Get Token (with Shopware signature)
                     2. API Calls (with token)
                            │
                            ▼
┌────────────────────────────────────────────────────────────┐
│         Go Backend Service                                │
│         (http://host.docker.internal:8085)                │
│                                                            │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  GET /api/auth/token                                 │  │
│  │  - Receives ALL query params from Shopware iframe    │  │
│  │  - Verifies signature, freshness and shop state      │  │
│  │    via SDK Resolver                                  │  │
│  │  - Generates encrypted token (AES-256-GCM, 15 min)   │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                            │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  POST /api/auth/refresh                              │  │
│  │  - Slides the token using the token itself           │  │
│  │  - Re-checks shop state, caps the session at 8h      │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                            │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  /api/* (Protected Routes)                           │  │
│  │  - Token validation middleware                       │  │
│  │  - Extracts shop context from token                  │  │
│  │  - Serves API responses                              │  │
│  │  - /api/shop/info, /api/dashboard/stats              │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                            │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  Shopware SDK Integration (Gin)                      │  │
│  │  - POST /app/register (registration)                 │  │
│  │  - POST /app/register/confirm (confirmation)         │  │
│  │  - POST /app/activate (lifecycle)                    │  │
│  │  - POST /app/deactivate (lifecycle)                  │  │
│  │  - POST /app/delete (lifecycle)                      │  │
│  │  - POST /app/webhook/* (business webhooks)           │  │
│  │  - Shop store (SQLite)                               │  │
│  └──────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────┘
```

## Authentication Flow

### 1. **Initial Load (Frontend Initialization)**

When Shopware loads the admin, the frontend runs in two contexts:

```typescript
// main.ts checks the iframe context
if (location.is(location.MAIN_HIDDEN)) {
  // HIDDEN IFRAME: Register sidebar entry
  ui.sidebar.add({
    title: 'Extension Demo',
    locationId: 'admin-extension-demo',
    icon: 'solid-puzzle',
  })
} else {
  // CONTENT IFRAME: Initialize app
  await initializeShopwareContext()
  createApp(App).mount('#app')
}
```

### 2. **Token Request (Frontend → Backend)**

When the content iframe loads, it requests a token:

```typescript
// services/shopware.ts
// Get the FULL query string from iframe URL (preserves parameter order!)
const originalQuery = window.location.search

// Forward to backend (maintains exact signature)
GET http://host.docker.internal:8085/api/auth/token${originalQuery}
```

**Critical:** The query string contains:
- `shop-id` - Shop identifier
- `shop-url` - Shop URL
- `timestamp` - Request timestamp
- `shopware-shop-signature` - HMAC signature of ALL parameters
- Other metadata (sw-version, app-version, location-id, etc.)

**Important:** We must forward the ENTIRE query string exactly as Shopware sent it, because:
1. Shopware signs ALL parameters (not just shop-id, shop-url, timestamp)
2. Parameter ORDER matters for signature verification
3. Reconstructing the query string breaks the signature

### 3. **Token Generation (Backend)**

```go
// main.go - createAuthToken handler
// SDK Resolver verifies signature and returns shop
shop, err := sdk.Resolver().ResolveFromQuery(c.Request.Context(), c.Request)

// Generate encrypted token with shop context (15 min validity)
token, err := tokenMgr.Generate(shop.ShopID, shop.ShopURL, tokenDuration)

// Return token to frontend
c.JSON(200, gin.H{
  "token": token,
  "expiresIn": int(tokenDuration.Seconds()),
})
```

**How Resolver Works:**
1. Extracts `shop-id` and `shopware-shop-signature` from query
2. Loads shop from store (SQLite)
3. Removes signature from query string
4. Computes HMAC-SHA256 of remaining query with shop secret
5. Uses constant-time comparison to verify signature
6. Returns verified shop if valid

### 4. **API Requests (Frontend → Backend)**

```typescript
// services/api.ts
// All API calls automatically include token
axios.get('/api/dashboard/stats', {
  headers: {
    Authorization: `Bearer ${token}`
  }
})
```

### 5. **Token Validation (Backend Middleware)**

```go
// main.go - tokenAuthMiddleware
// Extract Bearer token from Authorization header
token := authHeader[len("Bearer "):]

// Decrypt and validate token
shopCtx, err := tokenMgr.Validate(token)

// Inject shop context into request
c.Set("shopContext", shopCtx)
c.Set("shopID", shopCtx.ShopID)
```

## Project Structure

```
admin-extension/
├── app/
│   └── manifest.xml          # Shopware app config (local Docker URLs)
├── main.go                    # Main backend application
├── stats.go                   # Dashboard data from the Shopware Admin API
├── main_test.go               # Handler tests against a fake Shopware
├── auth/
│   └── token.go              # AES-256-GCM token encryption/decryption
├── frontend/                  # Vue.js application
│   ├── src/
│   │   ├── App.vue           # Main component (shop info, stats)
│   │   ├── main.ts           # Entry point (sidebar registration)
│   │   ├── assets/
│   │   │   └── main.css      # Styling
│   │   └── services/
│   │       ├── api.ts        # Backend API client (axios)
│   │       └── shopware.ts   # Meteor Admin SDK integration
│   ├── package.json          # Dependencies (Vue 3.5, Meteor 6.4)
│   ├── vite.config.ts        # Vite config (port 5173, allowed hosts)
│   └── tsconfig.json
├── go.mod                     # Go dependencies (Gin, SQLite, SDK)
├── Dockerfile                 # Multi-stage build (Go + Alpine)
├── docker-compose.yml         # Backend + Frontend services
├── env.example                # Environment variables template
└── README.md                  # This file
```

## Quick Start (Docker Compose)

### Prerequisites

- **Docker Compose**
- **Shopware 6.6 or 6.7** (running locally or in Docker)

### 1. **Setup Environment**

```bash
cd examples/admin-extension

# Copy environment file
cp env.example .env

# Generate secure token secret (min 32 characters for AES-256)
echo "TOKEN_SECRET=$(openssl rand -base64 32)" >> .env

# Edit .env and verify:
# - SHOPWARE_APP_NAME=AdminExtensionApp
# - SHOPWARE_APP_SECRET=my-super-secret-key-12345 (must match manifest)
# - SHOPWARE_CONFIRMATION_URL=http://host.docker.internal:8085/app/register/confirm
# - TOKEN_SECRET=<your-generated-secret>
# - DATABASE_PATH=./data/shops.db
```

### 2. **Start Services**

```bash
# Build and start backend + frontend
docker-compose up -d

# View logs
docker-compose logs -f

# Check health
curl http://host.docker.internal:8085/health
```

**Services:**
- **Backend:** `http://host.docker.internal:8085` (Go, port 8080 internal)
- **Frontend:** `http://host.docker.internal:5173` (Vite dev server, port 5173 internal)

### 3. **Install in Shopware**

```bash
# 1. Upload manifest to your service registry
# 2. In Shopware Admin: Extensions → App Store → My Extensions
# 3. Find "Admin Extension Example" and click "Install"
```

### 4. **Access the Extension**

1. Refresh Shopware Admin page
2. In the top right you will find a new icon (puzzle icon)
3. Click it to open the admin extension
4. You should see:
   - Shop Information (ID, URL, Status)
   - Dashboard Stats (Orders, Revenue, Products, Last Sync)
   - Quick Actions (Refresh Data, Test Notification)

## Configuration

### Environment Variables

| Variable | Description | Example/Default |
|----------|-------------|---------|
| `SHOPWARE_APP_NAME` | App name (must match manifest `<label>`) | `AdminExtensionApp` |
| `SHOPWARE_APP_SECRET` | App secret (must match manifest `<secret>`) | `my-super-secret-key-12345` |
| `SHOPWARE_CONFIRMATION_URL` | Registration callback URL | `http://host.docker.internal:8085/app/register/confirm` |
| `TOKEN_SECRET` | Secret for encrypting auth tokens (min 32 chars) | Generate with `openssl rand -base64 32` |
| `DATABASE_PATH` | Path to SQLite database file | `./data/shops.db` |

### Port Configuration

**Backend:**
- Internal (container): `8080`
- External (host): `8085`
- Docker host URL: `http://host.docker.internal:8085`

**Frontend:**
- Internal (container): `5173`
- External (host): `5173`
- Docker host URL: `http://host.docker.internal:5173`

### Generating Secure Secrets

```bash
# Generate app secret (for manifest.xml)
openssl rand -hex 32

# Generate token secret (for .env)
openssl rand -base64 32
```

## Docker Details

### Backend Service

```yaml
backend:
  build:
    context: .  # This directory; the SDK is downloaded as a published module
    dockerfile: Dockerfile
  ports:
    - "8085:8080"
  environment:
    - SHOPWARE_APP_SECRET=${SHOPWARE_APP_SECRET}
    - TOKEN_SECRET=${TOKEN_SECRET}
    - SERVER_ADDR=:8080  # Internal port
  volumes:
    - admin-data:/app/data  # Persist SQLite database
```

**Dockerfile Highlights:**
- Multi-stage build (Go 1.26 alpine → Alpine)
- Non-root user (`appuser`)
- Health check on `/health` endpoint

### Frontend Service

```yaml
frontend:
  image: node:20-alpine
  working_dir: /app
  command: sh -c "npm install && npm run dev -- --host 0.0.0.0"
  ports:
    - "5173:5173"
  volumes:
    - ./frontend:/app  # Live reload for development
  environment:
    - VITE_API_URL=http://host.docker.internal:8085
```

**Benefits:**
- Live reload during development
- No need to rebuild on frontend changes
- npm dependencies installed on first start

## Frontend Development

### Tech Stack

- **Vue 3.5** with Composition API and `<script setup>`
- **TypeScript** for type safety
- **Vite 5.4** for fast HMR
- **Axios 1.7** for HTTP client
- **Meteor Admin SDK 6.4** for Shopware integration

### Key Components

**1. Sidebar Registration (`main.ts`):**

```typescript
if (location.is(location.MAIN_HIDDEN)) {
  // Register in sidebar when Shopware loads admin
  ui.sidebar.add({
    title: 'Extension Demo',
    locationId: 'admin-extension-demo',
    icon: 'solid-puzzle',
    resizable: true,
  })
}
```

**2. Context Initialization (`services/shopware.ts`):**

```typescript
export async function initializeShopwareContext() {
  // Initialize Meteor Admin SDK
  const context = await Meteor.context.getContext()
  
  // Get auth token from backend
  const backendUrl = import.meta.env.VITE_API_URL
  const originalQuery = window.location.search  // Preserve exact query!
  const authUrl = `${backendUrl}/api/auth/token${originalQuery}`
  
  const response = await fetch(authUrl)
  const { token, expiresIn } = await response.json()
  
  // Store in global context
  setShopwareContext({ shopId, locale, accessToken: token })
}
```

**3. Authenticated API Client (`services/api.ts`):**

```typescript
export class ShopwareAPI {
  private client: AxiosInstance
  
  constructor() {
    this.client = axios.create({
      baseURL: import.meta.env.VITE_API_URL,
    })
    
    // Add Bearer token to all requests
    this.client.interceptors.request.use((config) => {
      const context = getShopwareContext()
      config.headers.Authorization = `Bearer ${context.accessToken}`
      return config
    })
  }
  
  async getDashboardStats() {
    const response = await this.client.get('/api/dashboard/stats')
    return response.data
  }
}
```

**4. Main UI (`App.vue`):**

```vue
<template>
  <div class="admin-extension">
    <h1>🚀 Admin Extension Demo</h1>
    
    <!-- Shop Information -->
    <div class="card">
      <h2>🏪 Shop Information</h2>
      <p><strong>Shop ID:</strong> {{ shopData.shopId }}</p>
      <p><strong>Shop URL:</strong> {{ shopData.shopUrl }}</p>
      <p><strong>Status:</strong> <span class="status">ACTIVE</span></p>
    </div>
    
    <!-- Dashboard Stats -->
    <div class="card">
      <h2>📊 Dashboard Statistics</h2>
      <!-- Stats grid... -->
    </div>
  </div>
</template>
```

### Development Workflow

```bash
# Start backend and frontend
docker-compose up -d

# Watch frontend logs for HMR
docker logs -f admin-extension-frontend

# Make changes to frontend/src/**/*.vue, *.ts
# Vite automatically reloads the browser!

# Rebuild backend (only when changing Go code)
docker-compose up -d --build backend
```

## Security Deep Dive

### 1. Signature Verification (Registration/Iframe Load)

**What Shopware Signs:**
```
shop-id=abc&shop-url=https://shop.com&timestamp=1234&location-id=...&sw-version=...
```

**Signature Algorithm:**
```
signature = HMAC-SHA256(queryString, shopSecret)
```

**SDK Resolver Verification:**
```go
// resolver/resolver.go
func (r *Resolver) ResolveFromQuery(req *http.Request) (*store.Shop, error) {
  shopID := req.URL.Query().Get("shop-id")
  sig := req.URL.Query().Get("shopware-shop-signature")
  
  shop := r.store.GetShop(shopID)
  
  // Remove signature from query
  queryWithoutSig := removeSignatureFromQuery(req.URL.RawQuery, sig)
  
  // Verify signature
  expectedSig := HMAC_SHA256(queryWithoutSig, shop.ShopSecret)
  if !constantTimeCompare(sig, expectedSig) {
    return nil, errors.New("invalid signature")
  }
  
  return shop, nil
}
```

**Why This Matters:**
- Prevents unauthorized token requests
- Ensures request came from legitimate Shopware instance
- Uses constant-time comparison to prevent timing attacks

### 2. Token Encryption (Frontend Auth)

**Token Structure:**
```go
// auth/token.go
type ShopContext struct {
  ShopID    string
  ShopURL   string
  ExpiresAt time.Time
}

// Encrypted with AES-256-GCM
token := encrypt(json.Marshal(ctx), tokenSecret)
```

**Benefits:**
- Self-contained (no database lookups)
- Tamper-proof (authenticated encryption)
- Short-lived (15 min default), refreshed via `/api/auth/refresh`

**Security Properties:**
- ✅ Confidentiality (AES-256)
- ✅ Integrity (GCM authentication tag)
- ✅ Expiration checking
- ✅ Replay protection (via expiration)
- ✅ Absolute session cap (refresh cannot extend past 8h)
- ✅ Shop state re-checked on every refresh

### 3. CORS Configuration

```go
// main.go
router.Use(cors.New(cors.Config{
  AllowOrigins:     []string{"http://host.docker.internal:5173", "http://localhost:5173"},
  AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
  AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
  ExposeHeaders:    []string{"Content-Length"},
  AllowCredentials: true,
}))
```

**Why Specific Origin:**
- Prevents unauthorized domains from calling API
- Shopware admin can still embed iframe (different mechanism)
- Only frontend can make authenticated API calls

### 4. SQLite Persistence

**Why SQLite:**
- Simple for local development
- File-based (easy to backup/inspect)
- No external database required
- Fine for a single instance with a persistent volume

**Schema:**
```sql
CREATE TABLE shops (
  shop_id TEXT PRIMARY KEY,
  shop_url TEXT NOT NULL,
  shop_secret TEXT NOT NULL,
  api_key TEXT,
  secret_key TEXT,
  active BOOLEAN DEFAULT 0,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

## Code Walkthrough

### Backend Key Functions

**1. Token Generation (`/api/auth/token`):**

```go
func createAuthToken(sdk *app.App, tokenMgr *auth.TokenManager) gin.HandlerFunc {
  return func(c *gin.Context) {
    // Step 1: Verify signature via SDK Resolver
    shop, err := sdk.Resolver().ResolveFromQuery(c.Request.Context(), c.Request)
    if err != nil {
      c.JSON(401, gin.H{"error": "Invalid shop authentication"})
      return
    }
    
    // Step 2: Generate a short-lived encrypted token
    token, err := tokenMgr.Generate(shop.ShopID, shop.ShopURL, tokenDuration)
    if err != nil {
      c.JSON(500, gin.H{"error": "Failed to generate token"})
      return
    }
    
    // Step 3: Return token to frontend
    c.JSON(200, gin.H{
      "token": token,
      "expiresIn": 900,
    })
  }
}
```

**2. Token Validation Middleware:**

```go
func tokenAuthMiddleware(tokenMgr *auth.TokenManager) gin.HandlerFunc {
  return func(c *gin.Context) {
    // Extract Bearer token
    authHeader := c.GetHeader("Authorization")
    if !strings.HasPrefix(authHeader, "Bearer ") {
      c.JSON(401, gin.H{"error": "Missing token"})
      c.Abort()
      return
    }
    
    token := authHeader[7:]  // Remove "Bearer "
    
    // Decrypt and validate
    shopCtx, err := tokenMgr.Validate(token)
    if err != nil {
      c.JSON(401, gin.H{"error": "Invalid token"})
      c.Abort()
      return
    }
    
    // Inject context
    c.Set("shopContext", shopCtx)
    c.Next()
  }
}
```

**3. Token Refresh (`/api/auth/refresh`):**

The signed iframe URL is a one-shot credential: the SDK expires signed queries
after 5 minutes, so the frontend cannot re-authenticate by replaying
`window.location.search`. Refresh therefore uses the token itself.

```go
func refreshAuthToken(sdk *app.App, tokenMgr *auth.TokenManager, logger *slog.Logger) gin.HandlerFunc {
  return func(c *gin.Context) {
    token, _ := bearerToken(c)

    // Step 1: Renew. Accepts a token that expired less than tokenRenewGrace
    // ago, but never past maxSessionDuration measured from the session start.
    renewed, shopCtx, err := tokenMgr.Renew(token, auth.RenewOptions{
      Duration:      tokenDuration,
      Grace:         tokenRenewGrace,
      MaxSessionAge: maxSessionDuration,
    })
    if err != nil {
      c.JSON(401, gin.H{"error": "Session expired, reload the module", "reauthorize": true})
      return
    }

    // Step 2: Re-check shop state. The token says who the shop was; the store
    // says whether it may still be served. A store failure is not an answer to
    // that question, so it must stay retryable — 401/403 tell the frontend the
    // session is over for good.
    shop, err := sdk.Store().GetShopByID(c.Request.Context(), shopCtx.ShopID)
    if err != nil {
      c.JSON(503, gin.H{"error": "Shop lookup temporarily failed"})
      return
    }
    if !shop.Active {
      c.JSON(403, gin.H{"error": "Shop is not active"})
      return
    }

    c.JSON(200, gin.H{"token": renewed, "expiresIn": int(time.Until(shopCtx.ExpiresAt).Seconds())})
  }
}
```

The frontend refreshes **proactively**, three minutes before expiry — around
minute 12 of a 15-minute token (`REFRESH_LEAD_MS` in
`frontend/src/services/shopware.ts`). That keeps an idle tab alive for the full
session: waiting for a request to fail would kill the session after 20 minutes of
inactivity, since that is when the backend's renewal window closes.

Two mechanisms back it up:

- **401 retry** in `frontend/src/services/api.ts` — the fallback for a timer that
  browsers throttled in a background tab, a suspended machine, or a clock jump.
  It refreshes once and replays the failed request.
- **`visibilitychange`** — returning to a hidden tab re-checks the token
  immediately instead of trusting a possibly-late timer.

Both share a single in-flight refresh, so a timer and a 401 firing together
rotate the token once rather than twice. A 401 whose token was already replaced
by a refresh that landed mid-flight is simply retried with the newer token, with
no second rotation.

Failures are split by what they say about the session. A **401 or 403** means it
is over — the timer stops and `onSessionExpired` listeners fire so `App.vue` can
show a banner, since a background failure would otherwise be invisible. Anything
else (a network blip, a 5xx while the backend redeploys) says nothing about the
session, so the loop backs off and retries instead of ending a session that is
still valid. Without that split a single failed attempt would stop the proactive
loop for good, and an idle tab would then coast past the 20-minute renewal
window and lose a session with hours left on it.

Three stages, each shorter-lived than the one it protects:

| Stage | Lifetime | Renewable by |
| --- | --- | --- |
| Signed iframe query | 5 min (SDK `QueryPolicy`) | reopening the module |
| Access token | 15 min (`tokenDuration`), refreshed at ~12 min | `/api/auth/refresh` |
| Session | 8 h (`maxSessionDuration`) | not renewable |

The 20-minute renewal window (`tokenDuration` + `tokenRenewGrace`) is what the
frontend must stay inside. With the proactive timer it does, so the practical
limit is the 8-hour cap rather than an idle timeout.

**4. Protected API Endpoint — real data from the Admin API:**

The dashboard numbers come from the shop's Admin API (`stats.go`). The handler gets the
shop from the frontend token and asks the SDK for an Admin API client:

```go
func getDashboardStats(sdk *app.App, logger *slog.Logger) gin.HandlerFunc {
  return func(c *gin.Context) {
    shopID := c.GetString("shopID") // injected by tokenAuthMiddleware

    client, err := sdk.NewClient(c.Request.Context(), shopID)
    if err != nil { /* 502 */ }

    stats, err := loadDashboardStats(c.Request.Context(), client)
    if err != nil { /* 502 */ }
    c.JSON(http.StatusOK, stats)
  }
}
```

`loadDashboardStats` runs two DAL searches with plain JSON criteria:

| Number | Request |
| --- | --- |
| Total orders, revenue | `POST /api/search/order` with `total-count-mode: 1` and a `sum` aggregation over `amountTotal` |
| Active products | `POST /api/search/product` with the filter `active = true` and `total-count-mode: 1` |

- Both requests use one client and share one OAuth token. The SDK keeps the token per
  shop, so later requests reuse it until shortly before it expires.
- The revenue is a plain sum over all orders without currency conversion — an example,
  not accounting.
- The app needs `<read>order</read>` and `<read>product</read>` in `app/manifest.xml`.
- `/api/shop/info` also returns `shopwareVersion` from `GET /api/_info/version`.

### Frontend Key Functions

**1. Sidebar Registration:**

```typescript
// main.ts
import { ui, location } from '@shopware-ag/meteor-admin-sdk'

if (location.is(location.MAIN_HIDDEN)) {
  // Hidden iframe - register sidebar
  ui.sidebar.add({
    title: 'Extension Demo',
    locationId: 'admin-extension-demo',
    icon: 'solid-puzzle',
  })
} else {
  // Content iframe - initialize app
  initializeShopwareContext()
    .then(() => createApp(App).mount('#app'))
}
```

**2. Query String Preservation:**

```typescript
// services/shopware.ts
export async function initializeShopwareContext() {
  // CRITICAL: Use exact query string from Shopware
  // Do NOT reconstruct it - parameter order matters for signature!
  const originalQuery = window.location.search
  
  const backendUrl = import.meta.env.VITE_API_URL
  const authUrl = `${backendUrl}/api/auth/token${originalQuery}`
  
  const response = await fetch(authUrl)
  const { token } = await response.json()
  
  setShopwareContext({ accessToken: token, ... })
}
```

**3. Axios Interceptor:**

```typescript
// services/api.ts
this.client.interceptors.request.use((config) => {
  const context = getShopwareContext()
  if (!context.accessToken) {
    throw new Error('No access token available')
  }
  config.headers.Authorization = `Bearer ${context.accessToken}`
  return config
})

this.client.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      // Token expired - could refresh here
      console.error('Authentication failed - token may be expired')
    }
    return Promise.reject(error)
  }
)
```

## Testing

### Manual Testing

```bash
# 1. Health check
curl http://host.docker.internal:8085/health

# 2. Test token generation (requires valid signature)
curl "http://host.docker.internal:8085/api/auth/token?shop-id=test&shopware-shop-signature=..."

# 3. Test protected endpoint (requires token)
curl -H "Authorization: Bearer <token>" \
  http://host.docker.internal:8085/api/dashboard/stats
```

### Integration Testing

```bash
# 1. Install app in Shopware
# 2. Open "Extension Demo" in admin
# 3. Verify:
#    - Sidebar entry appears
#    - Iframe loads without errors
#    - Shop information displays correctly
#    - Dashboard stats load
#    - Refresh button works
#    - Test notification shows in admin
```

### Debugging

```bash
# Backend logs
docker logs -f shopware-admin-extension

# Frontend logs
docker logs -f admin-extension-frontend

# Check database
docker exec shopware-admin-extension ls -la /app/data/

# Inspect SQLite (if sqlite3 available)
sqlite3 ./data/shops.db "SELECT * FROM shops;"
```

## Common Issues

### 1. "Invalid signature" error

**Cause:** Query string parameters don't match what Shopware signed

**Fix:** Ensure frontend forwards the EXACT query string:
```typescript
const originalQuery = window.location.search  // Don't modify!
```

### 2. CORS error in browser console

**Cause:** Backend CORS config doesn't allow frontend origin

**Fix:** Update `main.go`:
```go
AllowOrigins: []string{"http://host.docker.internal:5173", "http://localhost:5173"}
```

### 3. "Token expired" error

**Cause:** The refresh call failed too. Usually the absolute session cap (8h)
elapsed; it can also mean the tab was suspended for longer than
`tokenDuration` + `tokenRenewGrace` (20 min), so the proactive refresh never ran
and the token fell outside the renewal window.

**Fix:** Reopen the module in the Administration. That produces a freshly signed
iframe URL; the expired signed query in the old URL cannot be replayed, because
the SDK expires signed queries after 5 minutes.

### 4. Frontend not loading in iframe

**Cause:** Vite's allowed hosts config

**Fix:** Update `vite.config.ts`:
```typescript
server: {
  allowedHosts: ['host.docker.internal', 'localhost'],
}
```

### 5. SQLite "database is locked"

**Cause:** Multiple goroutines accessing SQLite without proper configuration

**Fix:** SQLite store uses `_journal_mode=WAL` and `_busy_timeout=5000` by default

## Learn More

- [Shopware App System](https://developer.shopware.com/docs/guides/plugins/apps/)
- [Meteor Admin SDK](https://shopware-meteor.mintlify.app/admin-sdk)
- [Admin Module Documentation](https://developer.shopware.com/docs/guides/plugins/apps/administration/)
- [Shopware Go SDK](../../README.md)
- [Context Resolver](../../resolver/README.md)

## What You Learned

After studying this example, you now understand:

1. ✅ How to build a Shopware admin extension with Go backend
2. ✅ How to use the SDK's Resolver for signature verification
3. ✅ How to implement custom frontend authentication
4. ✅ How to register dynamic sidebar entries with Meteor Admin SDK
5. ✅ How to structure a full-stack Shopware app
6. ✅ How to handle CORS between iframe and backend
7. ✅ How to use SQLite for shop persistence
8. ✅ How to deploy with Docker Compose

## Next Steps

**Extend This Example:**
- Add more API endpoints (products, orders, customers)
- Implement real data fetching from Shopware API
- Add WebSocket for real-time updates
- Build more complex UI components
- Add user preferences/settings
- Implement proper error boundaries

**Production Deployment:**
- Replace local Docker URLs with production HTTPS domains
- Use managed database (PostgreSQL, MySQL)
- Add rate limiting and request throttling
- Implement proper logging and monitoring
- Set up CI/CD pipeline
- Add comprehensive test suite

## License

MIT - See [LICENSE](../../LICENSE)
