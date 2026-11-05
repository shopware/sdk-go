package resolver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	sdkerrors "github.com/shopware/app-sdk-go/pkg/errors"
	"github.com/shopware/app-sdk-go/pkg/signature"
	"github.com/shopware/app-sdk-go/pkg/store"
	"github.com/shopware/app-sdk-go/pkg/webhook"
)

// Resolver resolves and verifies shops from HTTP requests
// This provides a framework-agnostic way to validate incoming requests from Shopware
type Resolver struct {
	store             store.ShopStore
	verifier          signature.Verifier
	dualVerifier      *signature.DualVerifier
	webhookVerifier   webhook.Verifier
	inflightAllowance time.Duration
	maxBodyBytes      int64
	queryPolicy       QueryPolicy
	postPolicy        PostPolicy
	lifecyclePolicy   LifecyclePolicy
}

// Config configures a Resolver
type Config struct {
	// ShopStore loads the shop a request claims to come from. Required.
	ShopStore store.ShopStore

	// WebhookVerifier verifies webhook-shaped POST bodies. Required — normally
	// the *webhook.Middleware built from the same store and policy.
	WebhookVerifier webhook.Verifier

	// Verifier performs the HMAC comparison.
	// When nil, signature.NewVerifier() is used.
	Verifier signature.Verifier

	// DualVerifier adds the previous-secret fallback used during a secret
	// rotation. When nil, it is derived from Verifier and
	// InflightAllowanceSeconds.
	DualVerifier *signature.DualVerifier

	// Grace window for previous-secret signatures; <= 0 uses signature.InflightAllowanceSeconds.
	// Drives the query path directly, separately from DualVerifier — set both or neither.
	InflightAllowanceSeconds int

	// MaxBodyBytes limits the size of incoming request bodies buffered before
	// authentication, mitigating memory-exhaustion DoS via oversized payloads.
	// When <= 0, webhook.DefaultMaxBodyBytes is used.
	MaxBodyBytes int64

	// QueryPolicy governs signed GET query authentication. The zero value is the
	// strictest policy: a timestamp is required and inactive shops are rejected.
	QueryPolicy QueryPolicy

	// PostPolicy governs signed JSON POST authentication that is not a webhook
	// event. The zero value checks the body's timestamp only when one is present.
	PostPolicy PostPolicy

	// LifecyclePolicy governs the freshness check for lifecycle webhook
	// authentication. The zero value requires a timestamp within 5 minutes.
	LifecyclePolicy LifecyclePolicy
}

// withDefaults fills every unset field with its documented default.
func (c Config) withDefaults() Config {
	if c.Verifier == nil {
		c.Verifier = signature.NewVerifier()
	}
	if c.DualVerifier == nil {
		c.DualVerifier = signature.NewDualVerifier(c.Verifier, c.InflightAllowanceSeconds)
	}
	if c.InflightAllowanceSeconds <= 0 {
		c.InflightAllowanceSeconds = signature.InflightAllowanceSeconds
	}
	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = webhook.DefaultMaxBodyBytes
	}
	c.QueryPolicy = c.QueryPolicy.withDefaults()
	c.PostPolicy = c.PostPolicy.withDefaults()
	c.LifecyclePolicy = c.LifecyclePolicy.withDefaults()

	return c
}

// NewResolver creates a context resolver.
//
//	r := resolver.NewResolver(resolver.Config{
//	    ShopStore:       shopStore,
//	    WebhookVerifier: webhookMW,
//	})
//
// Pass QueryPolicy or PostPolicy to deviate from the per-path defaults; see the
// package documentation for what those defaults are and why they differ.
func NewResolver(cfg Config) *Resolver {
	cfg = cfg.withDefaults()

	return &Resolver{
		store:             cfg.ShopStore,
		verifier:          cfg.Verifier,
		dualVerifier:      cfg.DualVerifier,
		webhookVerifier:   cfg.WebhookVerifier,
		inflightAllowance: time.Duration(cfg.InflightAllowanceSeconds) * time.Second,
		maxBodyBytes:      cfg.MaxBodyBytes,
		queryPolicy:       cfg.QueryPolicy,
		postPolicy:        cfg.PostPolicy,
		lifecyclePolicy:   cfg.LifecyclePolicy,
	}
}

// QueryPolicy returns the policy applied by ResolveFromQuery.
func (r *Resolver) QueryPolicy() QueryPolicy {
	return r.queryPolicy
}

// PostPolicy returns the policy applied by ResolveFromPost.
func (r *Resolver) PostPolicy() PostPolicy {
	return r.postPolicy
}

// LifecyclePolicy returns the policy applied by ResolveLifecycleWebhook.
func (r *Resolver) LifecyclePolicy() LifecyclePolicy {
	return r.lifecyclePolicy
}

// ResolveFromQuery resolves and verifies a shop from GET request query parameters
// This is used for:
// - Module requests from Shopware admin
// - Iframe loads with authentication parameters
// - Custom endpoints that receive shop-id and signature in query string
//
// The request must include:
// - shop-id: The unique shop identifier
// - shopware-shop-signature: HMAC-SHA256 signature of the query string
// - timestamp: Unix epoch the request was created at (see QueryPolicy)
//
// Beyond the signature, the resolver applies the configured QueryPolicy: the
// request must be fresh and the shop must be active. Without the freshness
// check a captured signed URL would authenticate forever, because replaying the
// same query reproduces the same valid signature.
//
// Returns the verified shop or an error if validation fails
func (r *Resolver) ResolveFromQuery(ctx context.Context, req *http.Request) (*store.Shop, error) {
	return r.ResolveFromQueryWithPolicy(ctx, req, r.queryPolicy)
}

// ResolveFromQueryWithPolicy behaves like ResolveFromQuery but applies the given
// policy instead of the resolver-wide one. Use it for individual routes that
// need to deviate, e.g. a legacy endpoint whose callers omit the timestamp.
func (r *Resolver) ResolveFromQueryWithPolicy(
	ctx context.Context,
	req *http.Request,
	policy QueryPolicy,
) (*store.Shop, error) {
	policy = policy.withDefaults()

	query := req.URL.Query()

	shopID := query.Get("shop-id")
	if shopID == "" {
		return nil, sdkerrors.NewMissingShopID()
	}

	sig := query.Get("shopware-shop-signature")
	if sig == "" {
		return nil, sdkerrors.NewMissingSignature()
	}

	shop, err := r.store.GetShopByID(ctx, shopID)
	if err != nil {
		if errors.Is(err, store.ErrShopNotFound) {
			return nil, sdkerrors.NewShopNotFound(shopID)
		}
		return nil, sdkerrors.NewStorageError("failed to look up shop", err)
	}

	queryForSignature := signature.PrepareQueryForSignature(req.URL.RawQuery)
	if verifyErr := r.verifier.VerifyRegistration(sig, queryForSignature, shop.ShopSecret); verifyErr != nil {
		if !r.verifyWithPreviousSecret(sig, queryForSignature, shop) {
			return nil, sdkerrors.NewInvalidSignature("query signature verification failed")
		}
	}

	// Freshness is checked only after the signature, because an unverified
	// timestamp is attacker-controlled data.
	if freshErr := checkFreshness(policy.freshness(), query.Get(QueryParamTimestamp)); freshErr != nil {
		return nil, freshErr
	}

	if !policy.AllowInactiveShop && !shop.Active {
		return nil, sdkerrors.NewShopNotActive(shopID)
	}

	return shop, nil
}

// formatBodyTimestamp renders a JSON body timestamp for the freshness check.
// An absent field and an explicit `"timestamp": 0` both read as "no timestamp",
// so a body cannot dodge the check by sending the epoch.
func formatBodyTimestamp(timestamp *int64) string {
	if timestamp == nil || *timestamp == 0 {
		return ""
	}
	return strconv.FormatInt(*timestamp, 10)
}

// checkFreshness applies a freshness policy to an already signature-verified
// timestamp and maps the outcome onto the SDK error type.
func checkFreshness(policy signature.FreshnessPolicy, timestampStr string) error {
	if err := policy.Check(timestampStr, time.Now()); err != nil {
		if errors.Is(err, signature.ErrMissingTimestamp) {
			return sdkerrors.NewTimestampExpired("missing timestamp")
		}
		return sdkerrors.NewTimestampExpired(err.Error())
	}

	return nil
}

// ResolveFromPost resolves and verifies a shop from a signed JSON POST request.
// Unlike ResolveFromWebhook, this only requires source.shopId and does not parse
// webhook-specific fields such as data.event.
func (r *Resolver) ResolveFromPost(ctx context.Context, req *http.Request) (*store.Shop, error) {
	return r.ResolveFromPostWithPolicy(ctx, req, r.postPolicy)
}

// ResolveFromPostWithPolicy behaves like ResolveFromPost but applies the given
// policy instead of the resolver-wide one. Pass
// PostPolicy{Timestamp: TimestampRequire} for endpoints you know only ever
// receive webhook-shaped bodies.
func (r *Resolver) ResolveFromPostWithPolicy(
	ctx context.Context,
	req *http.Request,
	policy PostPolicy,
) (*store.Shop, error) {
	policy = policy.withDefaults()

	rawBody, err := r.extractRawBody(req)
	if err != nil {
		if errors.Is(err, sdkerrors.ErrPayloadTooLarge) {
			return nil, err
		}
		return nil, sdkerrors.NewInvalidPayload("failed to read request body")
	}

	var payload struct {
		Source    json.RawMessage `json:"source"`
		Timestamp *int64          `json:"timestamp"`
	}
	if unmarshalErr := json.Unmarshal(rawBody, &payload); unmarshalErr != nil {
		return nil, sdkerrors.NewInvalidPayload("invalid JSON")
	}

	shopID, sourceErr := extractShopIDFromSource(payload.Source)
	if sourceErr != nil {
		return nil, sourceErr
	}

	shop, getErr := r.store.GetShopByID(ctx, shopID)
	if getErr != nil {
		if errors.Is(getErr, store.ErrShopNotFound) {
			return nil, sdkerrors.NewShopNotFound(shopID)
		}
		return nil, sdkerrors.NewStorageError("failed to look up shop", getErr)
	}
	if !shop.Active {
		return nil, sdkerrors.NewShopNotActive(shopID)
	}

	sig := req.Header.Get(signature.ShopSignatureHeader)
	if sig == "" {
		return nil, sdkerrors.NewMissingSignature()
	}

	if verifyErr := r.dualVerifier.VerifyWebhookWithFallback(sig, rawBody, &signature.ShopState{
		ShopSecret:         shop.ShopSecret,
		PreviousShopSecret: shop.PreviousShopSecret,
		SecretsRotatedAt:   shop.SecretsRotatedAt,
	}); verifyErr != nil {
		return nil, sdkerrors.NewInvalidSignature("post signature verification failed")
	}

	// Checked after the signature: an unverified timestamp is attacker-controlled.
	if freshErr := checkFreshness(policy.freshness(), formatBodyTimestamp(payload.Timestamp)); freshErr != nil {
		return nil, freshErr
	}

	return shop, nil
}

func extractShopIDFromSource(rawSource json.RawMessage) (string, error) {
	if len(rawSource) == 0 {
		return "", sdkerrors.NewMissingShopID()
	}

	var source struct {
		ShopID *string `json:"shopId"`
	}
	if err := json.Unmarshal(rawSource, &source); err != nil {
		return "", sdkerrors.NewMissingShopID()
	}
	if source.ShopID == nil || *source.ShopID == "" {
		return "", sdkerrors.NewMissingShopID()
	}

	return *source.ShopID, nil
}

// ResolveFromWebhook resolves and verifies a shop from a POST webhook request
// This is used for:
// - Webhook events from Shopware
//
// The request must include:
// - shopware-shop-signature header: HMAC-SHA256 signature of the request body
// - JSON body with source.shopId, source.url, and data.event fields
//
// Returns the verified shop, parsed webhook event, and any error
func (r *Resolver) ResolveFromWebhook(
	ctx context.Context,
	req *http.Request,
	traceID string,
) (*store.Shop, *webhook.Event, error) {
	// Use the webhook verifier interface to verify the request
	verified, err := r.webhookVerifier.VerifyWebhook(ctx, req, traceID)
	if err != nil {
		return nil, nil, fmt.Errorf("webhook verification failed: %w", err)
	}

	return verified.Shop, verified.Event, nil
}

// ResolveFromRequest automatically detects the request type and resolves the shop accordingly.
// - GET requests: Resolves from query parameters
// - Non-GET requests: Resolves from webhook body and signature
//
// This is a convenience method that combines ResolveFromQuery and ResolveFromWebhook.
// Use ResolveFromPost directly for signed JSON POST requests that are not webhook events.
func (r *Resolver) ResolveFromRequest(
	ctx context.Context,
	req *http.Request,
	traceID string,
) (*store.Shop, *webhook.Event, error) {
	if req.Method == http.MethodGet {
		shop, err := r.ResolveFromQuery(ctx, req)
		return shop, nil, err
	}

	return r.ResolveFromWebhook(ctx, req, traceID)
}

// ResolveLifecycleWebhook resolves and verifies a shop from a lifecycle webhook
// request (activate, deactivate, delete). Unlike the other resolver methods it
// serves inactive shops.
//
// expected is the event this route accepts: a body carrying any other event is
// rejected with 403 event_mismatch, since the route alone does not say which
// transition was signed.
//
// The signed body must carry source.shopId, data.event and timestamp.
func (r *Resolver) ResolveLifecycleWebhook(
	ctx context.Context,
	req *http.Request,
	expected webhook.LifecycleEvent,
) (*store.Shop, *webhook.Event, error) {
	return r.ResolveLifecycleWebhookWithPolicy(ctx, req, expected, r.lifecyclePolicy)
}

// ResolveLifecycleWebhookWithPolicy behaves like ResolveLifecycleWebhook but
// applies the given freshness policy instead of the resolver-wide one. Only
// freshness is negotiable; the event binding is not a policy field.
func (r *Resolver) ResolveLifecycleWebhookWithPolicy(
	ctx context.Context,
	req *http.Request,
	expected webhook.LifecycleEvent,
	policy LifecyclePolicy,
) (*store.Shop, *webhook.Event, error) {
	// An empty expectation would match a body that carries no event at all.
	if expected == "" {
		return nil, nil, sdkerrors.NewInvalidRequest("lifecycle route must name the event it accepts")
	}

	policy = policy.withDefaults()

	rawBody, err := r.extractRawBody(req)
	if err != nil {
		if errors.Is(err, sdkerrors.ErrPayloadTooLarge) {
			return nil, nil, err
		}
		return nil, nil, sdkerrors.NewInvalidPayload("failed to read request body")
	}

	var event webhook.Event
	if unmarshalErr := json.Unmarshal(rawBody, &event); unmarshalErr != nil {
		return nil, nil, sdkerrors.NewInvalidPayload("invalid JSON")
	}

	if event.Source.ShopID == "" {
		return nil, nil, sdkerrors.NewMissingShopID()
	}

	shop, getErr := r.store.GetShopByID(ctx, event.Source.ShopID)
	if getErr != nil {
		if errors.Is(getErr, store.ErrShopNotFound) {
			return nil, nil, sdkerrors.NewShopNotFound(event.Source.ShopID)
		}
		return nil, nil, sdkerrors.NewStorageError("failed to look up shop", getErr)
	}

	sig := req.Header.Get(signature.ShopSignatureHeader)
	if sig == "" {
		return nil, nil, sdkerrors.NewMissingSignature()
	}

	if verifyErr := r.dualVerifier.VerifyWebhookWithFallback(sig, rawBody, &signature.ShopState{
		ShopSecret:         shop.ShopSecret,
		PreviousShopSecret: shop.PreviousShopSecret,
		SecretsRotatedAt:   shop.SecretsRotatedAt,
	}); verifyErr != nil {
		return nil, nil, sdkerrors.NewInvalidSignature("webhook signature verification failed")
	}

	// Both checks run after the signature: the event name and timestamp are only
	// trustworthy once the body is authenticated. Binding goes first so a
	// mismatched event is reported as such even when the body is also stale.
	if webhook.LifecycleEvent(event.Data.Event) != expected {
		return nil, nil, sdkerrors.NewEventMismatch(string(expected), event.Data.Event)
	}

	if freshErr := checkFreshness(policy.freshness(), formatBodyTimestamp(&event.Timestamp)); freshErr != nil {
		return nil, nil, freshErr
	}

	// Store raw body and headers in event for potential downstream use
	event.RawBody = rawBody
	event.Headers = webhook.EventHeaders{
		ShopwareShopSignature: sig,
		ShopwareVersion:       req.Header.Get("Sw-Version"),
		UserAgent:             req.Header.Get("User-Agent"),
	}

	return shop, &event, nil
}

func (r *Resolver) verifyWithPreviousSecret(sig, queryForSignature string, shop *store.Shop) bool {
	if shop.PreviousShopSecret == "" || shop.SecretsRotatedAt == nil {
		return false
	}
	if time.Since(*shop.SecretsRotatedAt) >= r.inflightAllowance {
		return false
	}
	return r.verifier.VerifyRegistration(sig, queryForSignature, shop.PreviousShopSecret) == nil
}

// extractRawBody reads the request body (enforcing the configured size limit)
// and restores it for subsequent reads
func (r *Resolver) extractRawBody(req *http.Request) ([]byte, error) {
	return webhook.ReadLimitedBody(req, r.maxBodyBytes)
}
