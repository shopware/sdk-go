// Package webhook provides webhook verification and event processing for Shopware apps.
//
// This package handles incoming webhook requests from Shopware, verifying their
// authenticity and parsing event payloads.
//
// # Webhook Verification
//
// Use the Middleware to verify webhooks:
//
//	import (
//	    "github.com/shopware/sdk-go/webhook"
//	    "github.com/shopware/sdk-go/store"
//	)
//
//	// Only ShopStore is required; the verifiers, body limit and policy default.
//	middleware := webhook.NewMiddleware(webhook.Config{ShopStore: shopStore})
//	verified, err := middleware.VerifyWebhook(ctx, httpRequest, traceID)
//	if err != nil {
//	    // Handle verification failure
//	}
//
//	// Access verified data
//	shop := verified.Shop
//	event := verified.Event
//
// # Webhook Events
//
// Parsed webhook events contain:
//
//	type Event struct {
//	    Source    Source   // Shop information
//	    Data      Data     // Event type and payload
//	    Timestamp int64    // Event timestamp
//	    Headers   EventHeaders // Request headers
//	}
//
// # Event Types
//
// Shopware sends various types of webhook events:
//
// 1. Lifecycle Events (empty or object payload):
//   - app.activated: App was activated (empty payload)
//   - app.deactivated: App was deactivated (empty payload)
//   - app.deleted: App was uninstalled (object payload: {"keepUserData": bool})
//
// 2. Entity Events (array payload):
//   - product.written: Product was created or updated
//   - order.placed: New order was placed
//   - customer.written: Customer was created or updated
//   - ... and many more
//
// 3. Custom Events (object payload):
//   - media.uploaded: Media file uploaded (object payload: {"mediaId": "..."})
//
// Data.Payload is json.RawMessage — its shape depends on the event type.
// Use Data.UnmarshalPayload to decode it into the appropriate Go type.
//
// # Event Processing
//
//	switch event.Data.Event {
//	case "app.activated":
//	    log.Info("App activated", "shopId", event.Source.ShopID)
//	case "app.deleted":
//	    var p struct{ KeepUserData bool `json:"keepUserData"` }
//	    if err := event.Data.UnmarshalPayload(&p); err != nil {
//	        log.Error("Failed to unmarshal payload", "error", err)
//	        return
//	    }
//	case "product.written":
//	    var changes []webhook.EntityChange
//	    if err := event.Data.UnmarshalPayload(&changes); err != nil {
//	        log.Error("Failed to unmarshal payload", "error", err)
//	        return
//	    }
//	    for _, change := range changes {
//	        log.Info("Product updated", "id", change.PrimaryKey)
//	    }
//	}
//
// # Verification Interface
//
// The Verifier interface allows for mock implementations in tests:
//
//	type Verifier interface {
//	    VerifyWebhook(ctx context.Context, r *http.Request, traceID string) (*VerifiedRequest, error)
//	}
//
// # Security
//
// Webhook verification includes, in this order:
//   - Shop lookup and status validation (must be active)
//   - HMAC-SHA256 signature verification
//   - Timestamp validation against Policy (required by default)
//   - Payload integrity checks
//
// Freshness is checked after the signature on purpose: until the body is
// authenticated, its timestamp is attacker-controlled data. Requiring a
// timestamp is safe because Shopware stamps every webhook payload and re-stamps
// and re-signs it on each delivery attempt, so retries arrive fresh.
//
// Set Config.Policy to widen the window or relax the mode:
//
//	webhook.NewMiddleware(webhook.Config{
//	    ShopStore: shopStore,
//	    Policy: webhook.Policy{
//	        Freshness: signature.FreshnessPolicy{Mode: signature.TimestampVerifyIfPresent},
//	    },
//	})
//
// # Entity Changes
//
// For entity-write webhooks, unmarshal the payload into []EntityChange:
//
//	type EntityChange struct {
//	    Entity        string   // Entity type (e.g., "product")
//	    Operation     string   // Operation (insert/update/delete)
//	    PrimaryKey    string   // Entity ID
//	    UpdatedFields []string // Changed fields
//	}
//
// # Error Handling
//
// Verification failures return structured errors:
//
//	verified, err := middleware.VerifyWebhook(ctx, req, traceID)
//	if err != nil {
//	    var sdkErr *sdkerrors.Error
//	    if errors.As(err, &sdkErr) {
//	        switch sdkErr.Type {
//	        case sdkerrors.TypeInvalidSignature:
//	            // Invalid HMAC signature
//	        case sdkerrors.TypeShopNotActive:
//	            // Shop deactivated
//	        }
//	    }
//	}
//
// # Thread Safety
//
// Webhook middleware is safe for concurrent use.
package webhook
