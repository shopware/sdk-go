package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/shopware/sdk-go/sdkerrors"
	"github.com/shopware/sdk-go/signature"
	"github.com/shopware/sdk-go/store"
)

// Verifier defines the interface for webhook verification
type Verifier interface {
	VerifyWebhook(ctx context.Context, r *http.Request, traceID string) (*VerifiedRequest, error)
}

// Middleware handles webhook verification
type Middleware struct {
	store        store.ShopStore
	verifier     signature.Verifier
	dualVerifier *signature.DualVerifier
	maxBodyBytes int64
	policy       Policy
}

// VerifiedRequest contains a verified webhook request
type VerifiedRequest struct {
	Event   *Event
	Shop    *store.Shop
	TraceID string
}

// Config configures a webhook Middleware.
//
// Only ShopStore is required. Every other field resolves its zero value to the
// documented default, so Config{ShopStore: s} is a complete configuration.
type Config struct {
	// ShopStore loads the shop a webhook claims to come from. Required.
	ShopStore store.ShopStore

	// Verifier performs the HMAC comparison.
	// When nil, signature.NewVerifier() is used.
	Verifier signature.Verifier

	// DualVerifier adds the previous-secret fallback used during a secret
	// rotation. When nil, it is derived from Verifier and
	// InflightAllowance.
	DualVerifier *signature.DualVerifier

	// Grace window for previous-secret signatures; <= 0 uses signature.DefaultInflightAllowance.
	// Only used to derive DualVerifier, so supplying one makes this field inert.
	InflightAllowance time.Duration

	// MaxBodyBytes limits the size of incoming request bodies buffered before
	// authentication, mitigating memory-exhaustion DoS via oversized payloads.
	// When <= 0, DefaultMaxBodyBytes is used.
	MaxBodyBytes int64

	// Policy governs the freshness check applied to the webhook body's
	// timestamp. The zero value requires a timestamp within 5 minutes.
	Policy Policy
}

// withDefaults fills every unset field with its documented default.
func (c Config) withDefaults() Config {
	if c.Verifier == nil {
		c.Verifier = signature.NewVerifier()
	}
	if c.DualVerifier == nil {
		c.DualVerifier = signature.NewDualVerifier(c.Verifier, c.InflightAllowance)
	}
	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = DefaultMaxBodyBytes
	}
	c.Policy = c.Policy.withDefaults()

	return c
}

// NewMiddleware creates a webhook middleware.
//
//	mw := webhook.NewMiddleware(webhook.Config{ShopStore: shopStore})
//
// Passing Config{} minus the store is safe in the sense that every optional
// field defaults, but ShopStore has no default: leaving it nil produces a
// middleware that fails every request.
func NewMiddleware(cfg Config) *Middleware {
	cfg = cfg.withDefaults()

	return &Middleware{
		store:        cfg.ShopStore,
		verifier:     cfg.Verifier,
		dualVerifier: cfg.DualVerifier,
		maxBodyBytes: cfg.MaxBodyBytes,
		policy:       cfg.Policy,
	}
}

// Policy returns the policy applied by VerifyWebhook.
func (m *Middleware) Policy() Policy {
	return m.policy
}

// VerifyWebhook verifies and processes a webhook request from Shopware
func (m *Middleware) VerifyWebhook(ctx context.Context, r *http.Request, traceID string) (*VerifiedRequest, error) {
	rawBody, bodyErr := m.extractRawBody(r)
	if bodyErr != nil {
		if errors.Is(bodyErr, sdkerrors.ErrPayloadTooLarge) {
			return nil, bodyErr
		}
		return nil, sdkerrors.Wrap(
			bodyErr,
			sdkerrors.TypeInvalidPayload,
			fmt.Sprintf("failed to read request body: %s", bodyErr.Error()),
			StatusBadRequest,
		)
	}

	event, parseErr := m.parseEvent(rawBody, r)
	if parseErr != nil {
		return nil, parseErr
	}

	shop, shopErr := m.getActiveShop(ctx, event, traceID)
	if shopErr != nil {
		return nil, shopErr
	}

	if sigErr := m.verifySignature(event, rawBody, shop, traceID); sigErr != nil {
		return nil, sigErr
	}

	// Freshness comes after the signature: until the body is authenticated its
	// timestamp is attacker-controlled data.
	if validateErr := m.validateTimestamp(event, traceID); validateErr != nil {
		return nil, validateErr
	}

	return &VerifiedRequest{
		Event:   event,
		Shop:    shop,
		TraceID: traceID,
	}, nil
}

func (m *Middleware) parseEvent(rawBody []byte, r *http.Request) (*Event, error) {
	var event Event
	if unmarshalErr := json.Unmarshal(rawBody, &event); unmarshalErr != nil {
		return nil, sdkerrors.Wrap(
			unmarshalErr,
			sdkerrors.TypeInvalidPayload,
			fmt.Sprintf("invalid JSON: %s", unmarshalErr.Error()),
			StatusBadRequest,
		)
	}

	event.RawBody = rawBody
	event.Headers = EventHeaders{
		ShopwareShopSignature: r.Header.Get(signature.ShopSignatureHeader),
		ShopwareVersion:       r.Header.Get("Sw-Version"),
		UserAgent:             r.Header.Get("User-Agent"),
	}

	if validateErr := event.Validate(); validateErr != nil {
		return nil, validateErr
	}

	return &event, nil
}

// validateTimestamp applies the middleware policy to the webhook body's
// timestamp. An absent field and an explicit `"timestamp": 0` both count as "no
// timestamp", so a body cannot dodge the check by sending the epoch.
func (m *Middleware) validateTimestamp(event *Event, _ string) error {
	var timestampStr string
	if event.Timestamp > 0 {
		timestampStr = strconv.FormatInt(event.Timestamp, 10)
	}

	if err := m.policy.Freshness.Check(timestampStr, time.Now()); err != nil {
		message := err.Error()
		if errors.Is(err, signature.ErrMissingTimestamp) {
			message = "missing timestamp in webhook payload"
		}

		return sdkerrors.Wrap(err, sdkerrors.TypeTimestampExpired, message, StatusUnauthorized)
	}

	return nil
}

func (m *Middleware) getActiveShop(ctx context.Context, event *Event, _ string) (*store.Shop, error) {
	shop, getErr := m.store.GetShopByID(ctx, event.Source.ShopID)
	if getErr != nil {
		return nil, sdkerrors.Wrap(getErr, sdkerrors.TypeShopNotActive, "Shop not found", StatusNotFound)
	}

	if !shop.Active {
		return nil, sdkerrors.NewShopNotActive(shop.ShopID)
	}

	return shop, nil
}

func (m *Middleware) verifySignature(event *Event, rawBody []byte, shop *store.Shop, _ string) error {
	state := &signature.ShopState{
		ShopSecret:         shop.ShopSecret,
		PreviousShopSecret: shop.PreviousShopSecret,
		SecretsRotatedAt:   shop.SecretsRotatedAt,
	}
	if sigErr := m.dualVerifier.VerifyWebhookWithFallback(event.Headers.ShopwareShopSignature, rawBody, state); sigErr != nil {
		return sdkerrors.Wrap(sigErr, sdkerrors.TypeVerificationFailed, "Invalid signature", StatusUnauthorized)
	}

	return nil
}

func (m *Middleware) extractRawBody(r *http.Request) ([]byte, error) {
	return ReadLimitedBody(r, m.maxBodyBytes)
}
