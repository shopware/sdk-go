# Getting Started

This guide shows the fastest local path for building a Shopware app backend with the Go SDK.

## Install

Core SDK (framework-agnostic, no Gin/zap in the dependency graph):

```bash
go get github.com/shopware/sdk-go
```

Gin integration (separate module, pulls in Gin only here):

```bash
go get github.com/shopware/sdk-go/integrations/gin
```

Use Go 1.26 or newer (the SDK supports the two most recent major Go releases —
currently Go 1.26 and 1.27; see the README's version support policy). Prefer the
latest patch release of your line — see the recommended patch under
[Requirements](../README.md#requirements). The repository is a multi-module Go workspace; each module (core,
`integrations/gin`, `clients/adminapi`) is versioned independently, see
[RELEASING.md](RELEASING.md).

## Gin Quick Start

```go
package main

import (
	"log"

	"github.com/gin-gonic/gin"
	ginintegration "github.com/shopware/sdk-go/integrations/gin"
	"github.com/shopware/sdk-go/store"
)

func main() {
	router := gin.Default()

	err := ginintegration.Configure(router, ginintegration.Config{
		AppName:         "MyApp",
		AppSecret:       "your-app-secret-min-8-chars",
		ConfirmationURL: "https://myapp.example.com/app/register/confirm",
		ShopStore:       store.NewInMemoryStore(),
		EnableCORS:      true,
	})
	if err != nil {
		log.Fatal(err)
	}

	router.POST("/app/webhook/product", func(c *gin.Context) {
		shop, _ := ginintegration.GetShop(c)
		event, _ := ginintegration.GetVerifiedEvent(c)

		log.Printf("product webhook for shop %s: %s", shop.ShopID, event.Data.Event)
		c.Status(204)
	})

	log.Fatal(router.Run(":8080"))
}
```

The Gin integration registers the Shopware lifecycle endpoints below the configured base path. With the default base path, the important routes are:

- `GET /app/register`
- `POST /app/register/confirm`
- `POST /app/activate`
- `POST /app/deactivate`
- `POST /app/delete`

## Success Checks

A setup is working when:

- Shopware can reach the app's registration URL.
- The registration response contains `proof`, `secret`, and `confirmation_url`.
- The confirmation endpoint returns `204 No Content`.
- Lifecycle webhooks return `204 No Content`.
- Custom signed webhook handlers can read `ginintegration.GetShop(c)` and `ginintegration.GetVerifiedEvent(c)`.

## Run The Examples

```bash
(cd examples/vanilla-service && go test ./...)
(cd examples/gin-service && go test ./...)
(cd examples/admin-extension && go test ./...)
```

Each example is its own module. The committed `go.work` resolves the SDK modules to
the local sources, so the examples always run against the current checkout.

## Manual Routing

Without Gin, call the SDK from your own `net/http` handlers (or any other router). The
registration handler covers registration and confirmation; the webhook middleware
verifies signed webhooks:

```go
package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/shopware/sdk-go/app"
	"github.com/shopware/sdk-go/sdkerrors"
	"github.com/shopware/sdk-go/registration"
	"github.com/shopware/sdk-go/signature"
	"github.com/shopware/sdk-go/store"
	"github.com/shopware/sdk-go/webhook"
)

func main() {
	sdk, err := app.New(app.Config{
		AppName:         "MyApp",
		AppSecret:       "your-app-secret-min-8-chars",
		ConfirmationURL: "https://myapp.example.com/app/register/confirm",
		ShopStore:       store.NewInMemoryStore(),
		// MaxBodyBytes caps the size of request bodies read before authentication
		// (DoS guard). Optional; 0 means the default of 5 MiB.
	})
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("GET /app/register", handleRegistration(sdk))
	http.HandleFunc("POST /app/register/confirm", handleConfirmation(sdk))
	http.HandleFunc("POST /app/webhook", handleWebhook(sdk))

	log.Fatal(http.ListenAndServe(":8080", nil))
}

// handleRegistration answers the first registration request from Shopware.
func handleRegistration(sdk *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		timestamp, err := strconv.ParseInt(q.Get("timestamp"), 10, 64)
		if err != nil {
			http.Error(w, "invalid timestamp", http.StatusBadRequest)
			return
		}

		resp, err := sdk.RegistrationHandler().HandleRegistration(r.Context(), &registration.Request{
			ShopID:        q.Get("shop-id"),
			ShopURL:       q.Get("shop-url"),
			Timestamp:     timestamp,
			RawQuery:      r.URL.RawQuery,
			AppSignature:  r.Header.Get(signature.AppSignatureHeader),
			ShopSignature: r.Header.Get(signature.ShopSignatureHeader),
		})
		if err != nil {
			writeError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// handleConfirmation stores the API credentials Shopware sends after registration.
func handleConfirmation(sdk *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := webhook.ReadLimitedBody(r, sdk.Config().MaxBodyBytes)
		if err != nil {
			writeError(w, err)
			return
		}

		var req registration.ConfirmationRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		req.Signature = r.Header.Get(signature.ShopSignatureHeader)
		req.PreviousSignature = r.Header.Get(signature.ShopSignaturePreviousHeader)
		req.RawBody = body

		if _, err := sdk.RegistrationHandler().HandleConfirmation(r.Context(), &req); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleWebhook verifies a signed webhook and reads its entity changes.
func handleWebhook(sdk *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		verified, err := sdk.WebhookMiddleware().VerifyWebhook(r.Context(), r, r.Header.Get("X-Request-ID"))
		if err != nil {
			writeError(w, err)
			return
		}

		var changes []webhook.EntityChange
		if err := verified.Event.Data.UnmarshalPayload(&changes); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		log.Printf("%s from shop %s: %d changes", verified.Event.Data.Event, verified.Shop.ShopID, len(changes))

		w.WriteHeader(http.StatusNoContent)
	}
}

// writeError turns SDK errors into their HTTP status; anything else is a 500.
func writeError(w http.ResponseWriter, err error) {
	var sdkErr *sdkerrors.Error
	if errors.As(err, &sdkErr) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(sdkErr.Code)
		_ = json.NewEncoder(w).Encode(sdkErr)
		return
	}
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
```

Register the same paths in your `manifest.xml`. For a runnable version with an Admin API
call, see [`examples/vanilla-service`](../examples/vanilla-service/).

## Signed GET Requests (Admin Modules)

For browser-initiated requests — admin modules, action button tabs and modals — use
the resolver:

```go
shop, err := sdk.Resolver().ResolveFromQuery(r.Context(), r)
```

Besides the HMAC, this enforces `app.Config.QueryPolicy`: the signed `timestamp`
must be recent (5 minutes by default) and the shop must be active. Without the
freshness check a leaked signed URL would authenticate forever, since replaying
the same query reproduces the same valid signature. Shopware sends `timestamp` in
all documented signed-query flows; if you own a flow that omits it, opt out for
that route with `ResolveFromQueryWithPolicy` (or `GetRequestVerifierWithPolicy` in
Gin) rather than globally. See
[Replay Protection](../SECURITY.md#replay-protection).
