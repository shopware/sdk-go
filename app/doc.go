// Package app provides the main SDK entry point for Shopware app integration.
//
// The app package is the central component of the Shopware SDK for Go.
// It orchestrates all SDK functionality including shop registration, webhook
// verification, and API client creation.
//
// # Quick Start
//
// Create a new SDK instance with your app configuration:
//
//	import (
//	    "github.com/shopware/sdk-go/app"
//	    "github.com/shopware/sdk-go/store"
//	)
//
//	sdk, err := app.New(app.Config{
//	    AppName:         "MyApp",
//	    AppSecret:       "your-app-secret-from-manifest",
//	    ConfirmationURL: "https://my-app.com/register/confirm",
//	    ShopStore:       store.NewInMemoryStore(),
//	})
//	if err != nil {
//	    log.Fatalf("failed to create app: %v", err)
//	}
//
// The core SDK does not log. It returns typed errors (see package errors) and
// leaves logging to the caller or framework integration. See docs/adr/0002.
//
// # Components
//
// The App struct provides access to all SDK components:
//
//   - RegistrationHandler: Handles app installation and confirmation
//   - WebhookMiddleware: Verifies and processes webhook events
//   - Resolver: Resolves shops from HTTP requests (query params or webhooks)
//   - Verifier: Verifies HMAC-SHA256 signatures
//   - Store: Persists shop data
//
// # Shop Registration Flow
//
//	handler := sdk.RegistrationHandler()
//	resp, err := handler.HandleRegistration(ctx, registrationRequest)
//	// Later, handle confirmation
//	_, err = handler.HandleConfirmation(ctx, confirmationRequest)
//
// # Webhook Processing
//
//	middleware := sdk.WebhookMiddleware()
//	verified, err := middleware.VerifyWebhook(ctx, httpRequest, traceID)
//	// Process verified.Event and verified.Shop
//
// # API Client
//
// Create the Admin API client for a specific shop. Paths are relative to /api:
//
//	client, err := sdk.NewClient(ctx, "shop-id-123")
//	resp, err := client.Get(ctx, adminapi.Path("product", productID))
//
// The client is *adminapi.Client from
// github.com/shopware/sdk-go/clients/adminapi. In a webhook handler, use
// NewClientForShop with the verified shop.
//
// All clients of a shop share its OAuth token through Config.TokenStorage
// (in memory by default; use tokenstorage.NewKV from
// github.com/shopware/sdk-go/clients/adminapi/tokenstorage for Redis or
// Valkey). Clients of a shop that start at the same time fetch one token; set
// Config.Locker to a distributed lock to get the same across instances. Call
// ClearShopToken when the app is removed from a shop; the Gin integration does
// this for you.
//
// # Framework Integration
//
// For Gin integration, use the dedicated package:
//
//	import ginintegration "github.com/shopware/sdk-go/integrations/gin"
//	ginintegration.Configure(router, ginintegration.Config{
//	    AppName:   "MyApp",
//	    AppSecret: "secret",
//	    ShopStore: store.NewInMemoryStore(),
//	})
//
// # Configuration Validation
//
// The Config struct validates:
//   - AppName must not be empty
//   - AppSecret must be at least 8 characters
//   - ConfirmationURL must be a valid HTTPS URL
//
// Invalid configurations return an error from New().
//
// # Thread Safety
//
// The App struct and all its components are safe for concurrent use.
package app
