// Package resolver provides context resolution for Shopware requests.
//
// The resolver extracts and verifies shop information from HTTP requests,
// supporting query parameter authentication (iframes, redirects), generic
// signed JSON POST requests, and webhook POST requests.
//
// # Query Resolution (GET requests)
//
//	r := resolver.NewResolver(resolver.Config{
//	    ShopStore:       shopStore,
//	    WebhookVerifier: webhookMW,
//	})
//	shop, err := r.ResolveFromQuery(ctx, request)
//	// Extracts shop-id, verifies shopware-shop-signature, and applies QueryPolicy
//
// # Query Policy (freshness and shop state)
//
// A signed query is only as safe as its expiry: the signature covers the
// timestamp but does not make it expire, so a captured URL would replay forever
// if nobody compared that timestamp with the current time. QueryPolicy adds
// those checks, and its zero value is the strictest one — a timestamp is
// required, signed queries expire after signature.ReplayWindowSeconds, and
// inactive shops are rejected.
//
//	r := resolver.NewResolver(resolver.Config{
//	    ShopStore:       shopStore,
//	    WebhookVerifier: webhookMW,
//	    QueryPolicy:     resolver.QueryPolicy{MaxAge: 2 * time.Minute},
//	})
//
//	// Per-route override, e.g. a legacy link without a timestamp:
//	shop, err := r.ResolveFromQueryWithPolicy(ctx, request, resolver.QueryPolicy{
//	    Timestamp: resolver.TimestampVerifyIfPresent,
//	})
//
// # Source Resolution (signed JSON POST requests)
//
//	shop, err := r.ResolveFromPost(ctx, request)
//	// Extracts source.shopId, verifies the raw body signature, and applies PostPolicy
//
// PostPolicy only checks the body's timestamp when one is present. That is a
// protocol constraint, not caution: Shopware stamps webhook payloads, but not
// payment, tax provider or gateway payloads, and stamps action buttons at
// meta.timestamp rather than the top-level timestamp this package reads.
// Requiring a timestamp there would reject every such request, so replay on
// those flows needs a nonce, a dedup on source.eventId, or endpoint
// authorization.
//
// # Webhook Resolution (webhook POST requests)
//
//	shop, event, err := r.ResolveFromWebhook(ctx, request)
//	// Returns both shop and parsed webhook event
//
// # Lifecycle Resolution (activate, deactivate, delete)
//
//	shop, event, err := r.ResolveLifecycleWebhook(ctx, request, webhook.EventAppDeleted)
//	// Serves inactive shops, and only for the named event
//
// The expected event is required: three routes mutate the same shop in three
// ways, so the route alone does not say which transition was signed. A body
// carrying any other event is rejected with 403 event_mismatch. LifecyclePolicy
// governs the freshness check only.
//
// # Signature Verification
//
// Query, signed JSON POST, and webhook requests use the shop-specific secret.
//
// The resolver automatically handles signature extraction, shop lookup,
// and verification in a single call.
package resolver
