package webhook_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/shopware/sdk-go/internal/testutil"
	"github.com/shopware/sdk-go/sdkerrors"
	"github.com/shopware/sdk-go/signature"
	"github.com/shopware/sdk-go/store"
	"github.com/shopware/sdk-go/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPolicyMiddleware wires a middleware with an explicit policy and stores a shop for it.
func newPolicyMiddleware(t *testing.T, policy webhook.Policy) (*webhook.Middleware, *store.Shop) {
	t.Helper()

	shopStore := store.NewInMemoryStore()
	middleware := webhook.NewMiddleware(webhook.Config{ShopStore: shopStore, Policy: policy})

	shop := testutil.NewTestShop()
	require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

	return middleware, shop
}

// signedWebhook builds a signed webhook request. Pass a nil timestamp to omit
// the field entirely.
func signedWebhook(t *testing.T, shop *store.Shop, timestamp *int64) *http.Request {
	t.Helper()

	payload := createTestWebhookPayload(t)
	if timestamp == nil {
		delete(payload, "timestamp")
	} else {
		payload["timestamp"] = *timestamp
	}

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	return testutil.NewSignedRequest(http.MethodPost, "/webhook", body, shop.ShopSecret)
}

func ptr(v int64) *int64 { return &v }

func TestDefaultPolicy_RequiresTimestamp(t *testing.T) {
	t.Parallel()

	policy := webhook.DefaultPolicy()

	assert.Equal(t, signature.TimestampRequire, policy.Freshness.Mode)
	assert.Equal(t, signature.ReplayWindowSeconds*time.Second, policy.Freshness.MaxAge)
	assert.Equal(t, signature.ClockSkewSeconds*time.Second, policy.Freshness.ClockSkew)
}

func TestPolicy_ZeroValueRequiresTimestamp(t *testing.T) {
	t.Parallel()

	// The zero value must be the strict policy, so an unset field cannot weaken
	// webhook authentication.
	middleware, _ := newPolicyMiddleware(t, webhook.Policy{})

	assert.Equal(t, webhook.DefaultPolicy(), middleware.Policy())
}

// TestVerifyWebhook_Freshness pins the freshness behaviour of the webhook path.
// Shopware re-stamps and re-signs the payload on every delivery attempt, so
// requiring a recent timestamp does not interfere with its retry ladder.
func TestVerifyWebhook_Freshness(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name      string
		timestamp *int64
		wantErr   bool
	}{
		{name: "fresh timestamp", timestamp: ptr(now.Unix())},
		{name: "inside the window", timestamp: ptr(now.Add(-4 * time.Minute).Unix())},
		{name: "inside the clock skew", timestamp: ptr(now.Add(30 * time.Second).Unix())},
		{name: "outside the window", timestamp: ptr(now.Add(-10 * time.Minute).Unix()), wantErr: true},
		{name: "far in the future", timestamp: ptr(now.Add(time.Hour).Unix()), wantErr: true},
		{name: "replayed epoch", timestamp: ptr(1), wantErr: true},
		{name: "zero counts as missing", timestamp: ptr(0), wantErr: true},
		{name: "missing field", timestamp: nil, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			middleware, shop := newPolicyMiddleware(t, webhook.Policy{})
			req := signedWebhook(t, shop, tt.timestamp)

			verified, err := middleware.VerifyWebhook(context.Background(), req, "trace-id")

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, verified)
				testutil.AssertSDKError(t, err, sdkerrors.TypeTimestampExpired)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, shop.ShopID, verified.Shop.ShopID)
		})
	}
}

func TestVerifyWebhook_TimestampModes(t *testing.T) {
	t.Parallel()

	stale := time.Now().Add(-time.Hour).Unix()

	tests := []struct {
		name      string
		mode      signature.TimestampMode
		timestamp *int64
		wantErr   bool
	}{
		{name: "require rejects missing", mode: signature.TimestampRequire, timestamp: nil, wantErr: true},
		{name: "verify-if-present accepts missing", mode: signature.TimestampVerifyIfPresent, timestamp: nil},
		{name: "verify-if-present rejects stale", mode: signature.TimestampVerifyIfPresent, timestamp: &stale, wantErr: true},
		{name: "ignore accepts missing", mode: signature.TimestampIgnore, timestamp: nil},
		{name: "ignore accepts stale", mode: signature.TimestampIgnore, timestamp: &stale},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			middleware, shop := newPolicyMiddleware(t, webhook.Policy{
				Freshness: signature.FreshnessPolicy{Mode: tt.mode},
			})
			req := signedWebhook(t, shop, tt.timestamp)

			verified, err := middleware.VerifyWebhook(context.Background(), req, "trace-id")

			if tt.wantErr {
				require.Error(t, err)
				testutil.AssertSDKError(t, err, sdkerrors.TypeTimestampExpired)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, shop.ShopID, verified.Shop.ShopID)
		})
	}
}

func TestVerifyWebhook_CustomWindow(t *testing.T) {
	t.Parallel()

	middleware, shop := newPolicyMiddleware(t, webhook.Policy{
		Freshness: signature.FreshnessPolicy{MaxAge: 10 * time.Second},
	})

	within := signedWebhook(t, shop, ptr(time.Now().Add(-5*time.Second).Unix()))
	_, err := middleware.VerifyWebhook(context.Background(), within, "trace-id")
	require.NoError(t, err, "5s old must pass a 10s window")

	outside := signedWebhook(t, shop, ptr(time.Now().Add(-30*time.Second).Unix()))
	_, err = middleware.VerifyWebhook(context.Background(), outside, "trace-id")
	require.Error(t, err, "30s old must fail a 10s window")
	testutil.AssertSDKError(t, err, sdkerrors.TypeTimestampExpired)
}

// TestVerifyWebhook_SignatureCheckedBeforeFreshness mirrors the signed query
// test of the same name: an unsigned request must never reveal whether its
// timestamp would have been accepted, because until the body is authenticated
// the timestamp is attacker-controlled.
func TestVerifyWebhook_SignatureCheckedBeforeFreshness(t *testing.T) {
	t.Parallel()

	// The shop is stored, but the request is signed with the wrong secret.
	middleware, _ := newPolicyMiddleware(t, webhook.Policy{})

	payload := createTestWebhookPayload(t)
	payload["timestamp"] = 1
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := testutil.NewSignedRequest(http.MethodPost, "/webhook", body, "wrong-secret")

	_, verifyErr := middleware.VerifyWebhook(context.Background(), req, "trace-id")

	require.Error(t, verifyErr)
	testutil.AssertSDKError(t, verifyErr, sdkerrors.TypeVerificationFailed)
}
