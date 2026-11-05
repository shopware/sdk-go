package resolver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopware/sdk-go/internal/testutil"
	"github.com/shopware/sdk-go/resolver"
	"github.com/shopware/sdk-go/sdkerrors"
	"github.com/shopware/sdk-go/signature"
	"github.com/shopware/sdk-go/store"
	"github.com/shopware/sdk-go/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const queryBase = "shop-id=test-shop-123&shop-url=https://test-shop.com"

// newPolicyResolver builds a resolver whose query policy is the one under test;
// everything else stays at its default. For a fully default resolver use
// newTestResolver from resolver_test.go.
func newPolicyResolver(t *testing.T, policy resolver.QueryPolicy) (*resolver.Resolver, store.ShopStore) {
	t.Helper()

	shopStore := store.NewInMemoryStore()

	return resolver.NewResolver(resolver.Config{
		ShopStore:       shopStore,
		WebhookVerifier: webhook.NewMiddleware(webhook.Config{ShopStore: shopStore}),
		QueryPolicy:     policy,
	}), shopStore
}

func TestDefaultQueryPolicy_IsStrict(t *testing.T) {
	t.Parallel()

	policy := resolver.DefaultQueryPolicy()

	assert.Equal(t, resolver.TimestampRequire, policy.Timestamp)
	assert.Equal(t, signature.ReplayWindowSeconds*time.Second, policy.MaxAge)
	assert.Equal(t, signature.ClockSkewSeconds*time.Second, policy.ClockSkew)
	assert.False(t, policy.AllowInactiveShop, "inactive shops must be rejected by default")
}

func TestQueryPolicy_ZeroValueMatchesDefault(t *testing.T) {
	t.Parallel()

	// The zero value must be the strictest policy so a partially filled
	// QueryPolicy can never silently weaken authentication.
	r, _ := newPolicyResolver(t, resolver.QueryPolicy{})

	assert.Equal(t, resolver.DefaultQueryPolicy(), r.QueryPolicy())
}

// TestResolveFromQuery_Freshness is the regression test for SEC-REPLAY-QUERY-L79:
// a correctly signed query with a stale timestamp used to authenticate forever.
func TestResolveFromQuery_Freshness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		timestamp time.Time
		wantErr   bool
	}{
		{name: "fresh request", timestamp: time.Now()},
		{name: "just inside the window", timestamp: time.Now().Add(-4 * time.Minute)},
		{name: "inside the clock skew", timestamp: time.Now().Add(30 * time.Second)},
		{name: "replayed audit snapshot", timestamp: time.Unix(1, 0), wantErr: true},
		{name: "outside the window", timestamp: time.Now().Add(-10 * time.Minute), wantErr: true},
		{name: "far in the future", timestamp: time.Now().Add(time.Hour), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, shopStore := newPolicyResolver(t, resolver.QueryPolicy{})
			shop := testutil.NewTestShop()
			require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

			req := testutil.NewSignedQueryRequestAt("http://example.com/module", queryBase, shop.ShopSecret, tt.timestamp)
			resolved, err := r.ResolveFromQuery(context.Background(), req)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, resolved)
				testutil.AssertSDKError(t, err, sdkerrors.TypeTimestampExpired)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, shop.ShopID, resolved.ShopID)
		})
	}
}

func TestResolveFromQuery_TimestampModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mode    resolver.TimestampMode
		stale   bool
		wantErr bool
	}{
		{name: "require rejects missing timestamp", mode: resolver.TimestampRequire, wantErr: true},
		{name: "verify-if-present accepts missing timestamp", mode: resolver.TimestampVerifyIfPresent},
		{name: "verify-if-present rejects stale timestamp", mode: resolver.TimestampVerifyIfPresent, stale: true, wantErr: true},
		{name: "ignore accepts missing timestamp", mode: resolver.TimestampIgnore},
		{name: "ignore accepts stale timestamp", mode: resolver.TimestampIgnore, stale: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, shopStore := newPolicyResolver(t, resolver.QueryPolicy{Timestamp: tt.mode})
			shop := testutil.NewTestShop()
			require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

			var req = testutil.NewSignedQueryRequest("http://example.com/module", queryBase, shop.ShopSecret)
			if tt.stale {
				req = testutil.NewSignedQueryRequestAt("http://example.com/module", queryBase, shop.ShopSecret, time.Unix(1, 0))
			}

			resolved, err := r.ResolveFromQuery(context.Background(), req)

			if tt.wantErr {
				require.Error(t, err)
				testutil.AssertSDKError(t, err, sdkerrors.TypeTimestampExpired)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, shop.ShopID, resolved.ShopID)
		})
	}
}

func TestResolveFromQuery_InactiveShop(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		allowInactiveShop bool
		wantErr           bool
	}{
		{name: "inactive shop is rejected by default", wantErr: true},
		{name: "inactive shop is allowed when opted in", allowInactiveShop: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, shopStore := newPolicyResolver(t, resolver.QueryPolicy{AllowInactiveShop: tt.allowInactiveShop})
			shop := testutil.NewTestShop(testutil.WithShopActive(false))
			require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

			req := testutil.NewSignedQueryRequestAt("http://example.com/module", queryBase, shop.ShopSecret, time.Now())
			resolved, err := r.ResolveFromQuery(context.Background(), req)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, resolved)
				testutil.AssertSDKError(t, err, sdkerrors.TypeShopNotActive)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, shop.ShopID, resolved.ShopID)
		})
	}
}

func TestResolveFromQueryWithPolicy_OverridesResolverPolicy(t *testing.T) {
	t.Parallel()

	r, shopStore := newPolicyResolver(t, resolver.QueryPolicy{})
	shop := testutil.NewTestShop()
	require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

	req := testutil.NewSignedQueryRequest("http://example.com/legacy", queryBase, shop.ShopSecret)

	// The resolver-wide policy requires a timestamp...
	_, err := r.ResolveFromQuery(context.Background(), req)
	require.Error(t, err)

	// ...while a per-route policy can opt out.
	resolved, err := r.ResolveFromQueryWithPolicy(context.Background(), req, resolver.QueryPolicy{
		Timestamp: resolver.TimestampIgnore,
	})
	require.NoError(t, err)
	assert.Equal(t, shop.ShopID, resolved.ShopID)
}

func TestResolveFromQuery_CustomWindow(t *testing.T) {
	t.Parallel()

	r, shopStore := newPolicyResolver(t, resolver.QueryPolicy{MaxAge: 10 * time.Second})
	shop := testutil.NewTestShop()
	require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

	within := testutil.NewSignedQueryRequestAt("http://example.com/module", queryBase, shop.ShopSecret, time.Now().Add(-5*time.Second))
	resolved, err := r.ResolveFromQuery(context.Background(), within)
	require.NoError(t, err, "5s old must pass a 10s window")
	assert.Equal(t, shop.ShopID, resolved.ShopID)

	outside := testutil.NewSignedQueryRequestAt("http://example.com/module", queryBase, shop.ShopSecret, time.Now().Add(-30*time.Second))
	_, err = r.ResolveFromQuery(context.Background(), outside)
	require.Error(t, err, "30s old must fail a 10s window")
	testutil.AssertSDKError(t, err, sdkerrors.TypeTimestampExpired)
}

// TestResolveFromQuery_SignatureCheckedBeforeFreshness pins the check order: an
// unsigned request must never reveal whether its timestamp would have been
// accepted, because an unverified timestamp is attacker-controlled data.
func TestResolveFromQuery_SignatureCheckedBeforeFreshness(t *testing.T) {
	t.Parallel()

	r, shopStore := newPolicyResolver(t, resolver.QueryPolicy{})
	shop := testutil.NewTestShop()
	require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

	req := testutil.NewSignedQueryRequestAt("http://example.com/module", queryBase, "wrong-secret", time.Unix(1, 0))

	_, err := r.ResolveFromQuery(context.Background(), req)

	require.Error(t, err)
	testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidSignature)
}

// TestResolveFromRequest_GET_AppliesPolicy guards the generic entry point, which
// dispatches GET requests to the signed query path.
func TestResolveFromRequest_GET_AppliesPolicy(t *testing.T) {
	t.Parallel()

	r, shopStore := newPolicyResolver(t, resolver.QueryPolicy{})
	shop := testutil.NewTestShop()
	require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

	req := testutil.NewSignedQueryRequestAt("http://example.com/module", queryBase, shop.ShopSecret, time.Unix(1, 0))

	_, _, err := r.ResolveFromRequest(context.Background(), req, "trace-id")

	require.Error(t, err)
	testutil.AssertSDKError(t, err, sdkerrors.TypeTimestampExpired)
}

// --- signed JSON POST -------------------------------------------------------

// signedPost builds a signed POST request. Pass a nil timestamp to omit the
// field, mirroring the payment/gateway payloads Shopware sends without one.
func signedPost(t *testing.T, shop *store.Shop, timestamp *int64) *http.Request {
	t.Helper()

	payload := map[string]any{
		"source": map[string]any{
			"shopId": shop.ShopID,
			"url":    shop.ShopURL,
		},
		"licenseKey": "valid-license-key",
	}
	if timestamp != nil {
		payload["timestamp"] = *timestamp
	}

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/service/license/sync", bytes.NewReader(body))
	req.Header.Set(signature.ShopSignatureHeader, signature.ComputeSignature(body, shop.ShopSecret))

	return req
}

func timestampPtr(v int64) *int64 { return &v }

func TestDefaultPostPolicy_ChecksOnlyWhenPresent(t *testing.T) {
	t.Parallel()

	policy := resolver.DefaultPostPolicy()

	// Require would reject every payment, tax provider and gateway request,
	// because Shopware does not stamp those payloads.
	assert.Equal(t, resolver.TimestampVerifyIfPresent, policy.Timestamp)
	assert.Equal(t, signature.ReplayWindowSeconds*time.Second, policy.MaxAge)
	assert.Equal(t, signature.ClockSkewSeconds*time.Second, policy.ClockSkew)
}

func TestPostPolicy_ZeroValueMatchesDefault(t *testing.T) {
	t.Parallel()

	r, _ := newTestResolver(t)

	assert.Equal(t, resolver.DefaultPostPolicy(), r.PostPolicy())
}

func TestResolveFromPost_Freshness(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name      string
		timestamp *int64
		wantErr   bool
	}{
		{name: "no timestamp is accepted by default", timestamp: nil},
		{name: "zero timestamp reads as absent", timestamp: timestampPtr(0)},
		{name: "fresh timestamp", timestamp: timestampPtr(now.Unix())},
		{name: "stale timestamp is rejected", timestamp: timestampPtr(now.Add(-10 * time.Minute).Unix()), wantErr: true},
		{name: "future timestamp is rejected", timestamp: timestampPtr(now.Add(time.Hour).Unix()), wantErr: true},
		{name: "replayed epoch is rejected", timestamp: timestampPtr(1), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, shopStore := newTestResolver(t)
			shop := testutil.NewTestShop()
			require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

			resolved, err := r.ResolveFromPost(context.Background(), signedPost(t, shop, tt.timestamp))

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, resolved)
				testutil.AssertSDKError(t, err, sdkerrors.TypeTimestampExpired)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, shop.ShopID, resolved.ShopID)
		})
	}
}

func TestResolveFromPostWithPolicy_StrictMode(t *testing.T) {
	t.Parallel()

	r, shopStore := newTestResolver(t)
	shop := testutil.NewTestShop()
	require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

	strict := resolver.PostPolicy{Timestamp: resolver.TimestampRequire}

	// An explicit TimestampRequire must be honoured, not downgraded to the
	// path's laxer default.
	_, err := r.ResolveFromPostWithPolicy(context.Background(), signedPost(t, shop, nil), strict)
	require.Error(t, err)
	testutil.AssertSDKError(t, err, sdkerrors.TypeTimestampExpired)

	resolved, err := r.ResolveFromPostWithPolicy(
		context.Background(),
		signedPost(t, shop, timestampPtr(time.Now().Unix())),
		strict,
	)
	require.NoError(t, err)
	assert.Equal(t, shop.ShopID, resolved.ShopID)
}

func TestResolveFromPost_SignatureCheckedBeforeFreshness(t *testing.T) {
	t.Parallel()

	r, shopStore := newTestResolver(t)
	shop := testutil.NewTestShop()
	require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

	// Signed with the wrong secret and carrying a long-expired timestamp: the
	// signature failure must win, so an unauthenticated caller learns nothing
	// about the freshness check.
	req := signedPost(t, &store.Shop{
		ShopID:     shop.ShopID,
		ShopURL:    shop.ShopURL,
		ShopSecret: "wrong-secret",
	}, timestampPtr(1))

	_, err := r.ResolveFromPost(context.Background(), req)

	require.Error(t, err)
	testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidSignature)
}

// --- cross-path parity ------------------------------------------------------

// signedWebhookBody builds a signed webhook request. Pass a nil timestamp to
// omit the field.
func signedWebhookBody(t *testing.T, shop *store.Shop, timestamp *int64) *http.Request {
	t.Helper()

	payload := loadWebhookPayloadFixture(t)
	payload["source"] = map[string]any{"shopId": shop.ShopID, "url": shop.ShopURL}
	if timestamp == nil {
		delete(payload, "timestamp")
	} else {
		payload["timestamp"] = *timestamp
	}

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set(signature.ShopSignatureHeader, signature.ComputeSignature(body, shop.ShopSecret))

	return req
}

// signedLifecycleBody builds a signed lifecycle request carrying the given event.
// Pass a nil timestamp to omit the field, an empty event to omit data.event.
func signedLifecycleBody(
	t *testing.T,
	shop *store.Shop,
	event webhook.LifecycleEvent,
	timestamp *int64,
) *http.Request {
	t.Helper()

	payload := loadWebhookPayloadFixture(t)
	payload["source"] = map[string]any{"shopId": shop.ShopID, "url": shop.ShopURL}
	payload["data"] = map[string]any{"event": string(event), "payload": []any{}}
	if timestamp == nil {
		delete(payload, "timestamp")
	} else {
		payload["timestamp"] = *timestamp
	}

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/app/lifecycle", bytes.NewReader(body))
	req.Header.Set(signature.ShopSignatureHeader, signature.ComputeSignature(body, shop.ShopSecret))

	return req
}

// TestFreshnessParityAcrossPaths makes the SDK's per-path behaviour executable
// instead of something you have to derive by reading four call sites. Each path
// is exercised with a correctly signed request that is fresh, stale, and without
// a timestamp at all.
//
// The differences are deliberate:
//   - Query, webhook and lifecycle require a timestamp, because Shopware always
//     sends one. Lifecycle routes are registered as ordinary webhooks in the
//     manifest, so they travel the same stamped-and-re-stamped delivery path.
//   - Signed POST only checks it when present: payment, tax provider and gateway
//     payloads carry none, so requiring one would reject them.
//
// Lifecycle still differs on shop state — it must serve inactive shops — which
// TestActiveShopParityAcrossPaths covers.
func TestFreshnessParityAcrossPaths(t *testing.T) {
	t.Parallel()

	type outcome struct {
		fresh, stale, missing bool // true = accepted
	}

	tests := []struct {
		name   string
		invoke func(t *testing.T, r *resolver.Resolver, shop *store.Shop, ts *int64) error
		want   outcome
	}{
		{
			name: "query",
			invoke: func(t *testing.T, r *resolver.Resolver, shop *store.Shop, ts *int64) error {
				t.Helper()
				req := testutil.NewSignedQueryRequest("http://example.com/module", queryBase, shop.ShopSecret)
				if ts != nil {
					req = testutil.NewSignedQueryRequestAt(
						"http://example.com/module", queryBase, shop.ShopSecret, time.Unix(*ts, 0))
				}
				_, err := r.ResolveFromQuery(context.Background(), req)
				return err
			},
			want: outcome{fresh: true, stale: false, missing: false},
		},
		{
			name: "post",
			invoke: func(t *testing.T, r *resolver.Resolver, shop *store.Shop, ts *int64) error {
				t.Helper()
				_, err := r.ResolveFromPost(context.Background(), signedPost(t, shop, ts))
				return err
			},
			want: outcome{fresh: true, stale: false, missing: true},
		},
		{
			name: "webhook",
			invoke: func(t *testing.T, r *resolver.Resolver, shop *store.Shop, ts *int64) error {
				t.Helper()
				_, _, err := r.ResolveFromWebhook(context.Background(), signedWebhookBody(t, shop, ts), "trace-id")
				return err
			},
			want: outcome{fresh: true, stale: false, missing: false},
		},
		{
			name: "lifecycle",
			invoke: func(t *testing.T, r *resolver.Resolver, shop *store.Shop, ts *int64) error {
				t.Helper()
				req := signedLifecycleBody(t, shop, webhook.EventAppActivated, ts)
				_, _, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppActivated)
				return err
			},
			want: outcome{fresh: true, stale: false, missing: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cases := []struct {
				label     string
				timestamp *int64
				accepted  bool
			}{
				{label: "fresh", timestamp: timestampPtr(time.Now().Unix()), accepted: tt.want.fresh},
				{label: "stale", timestamp: timestampPtr(1), accepted: tt.want.stale},
				{label: "missing", timestamp: nil, accepted: tt.want.missing},
			}

			for _, c := range cases {
				t.Run(c.label, func(t *testing.T) {
					t.Parallel()

					r, shopStore := newTestResolver(t)
					shop := testutil.NewTestShop()
					require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

					err := tt.invoke(t, r, shop, c.timestamp)

					if c.accepted {
						require.NoError(t, err)
						return
					}

					require.Error(t, err)
					testutil.AssertSDKError(t, err, sdkerrors.TypeTimestampExpired)
				})
			}
		})
	}
}

// TestActiveShopParityAcrossPaths pins the other half of the matrix: which paths
// refuse to serve a deactivated shop. Lifecycle deliberately does not, because
// activate/deactivate/delete have to work while the shop is inactive.
func TestActiveShopParityAcrossPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		invoke   func(t *testing.T, r *resolver.Resolver, shop *store.Shop) error
		accepted bool
	}{
		{
			name: "query",
			invoke: func(t *testing.T, r *resolver.Resolver, shop *store.Shop) error {
				t.Helper()
				req := testutil.NewSignedQueryRequestAt(
					"http://example.com/module", queryBase, shop.ShopSecret, time.Now())
				_, err := r.ResolveFromQuery(context.Background(), req)
				return err
			},
		},
		{
			name: "post",
			invoke: func(t *testing.T, r *resolver.Resolver, shop *store.Shop) error {
				t.Helper()
				_, err := r.ResolveFromPost(context.Background(), signedPost(t, shop, nil))
				return err
			},
		},
		{
			name: "webhook",
			invoke: func(t *testing.T, r *resolver.Resolver, shop *store.Shop) error {
				t.Helper()
				req := signedWebhookBody(t, shop, timestampPtr(time.Now().Unix()))
				_, _, err := r.ResolveFromWebhook(context.Background(), req, "trace-id")
				return err
			},
		},
		{
			name: "lifecycle",
			invoke: func(t *testing.T, r *resolver.Resolver, shop *store.Shop) error {
				t.Helper()
				req := signedLifecycleBody(t, shop, webhook.EventAppActivated, timestampPtr(time.Now().Unix()))
				_, _, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppActivated)
				return err
			},
			accepted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, shopStore := newTestResolver(t)
			shop := testutil.NewTestShop(testutil.WithShopActive(false))
			require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

			err := tt.invoke(t, r, shop)

			if tt.accepted {
				require.NoError(t, err, "lifecycle must serve inactive shops")
				return
			}

			require.Error(t, err)
		})
	}
}
