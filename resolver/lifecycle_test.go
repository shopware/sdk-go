package resolver_test

import (
	"context"
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

// newLifecycleResolver builds a resolver with the given lifecycle policy and a
// confirmed, active shop already in the store.
func newLifecycleResolver(
	t *testing.T,
	policy resolver.LifecyclePolicy,
) (*resolver.Resolver, *store.Shop) {
	t.Helper()

	shopStore := store.NewInMemoryStore()
	r := resolver.NewResolver(resolver.Config{
		ShopStore:       shopStore,
		WebhookVerifier: webhook.NewMiddleware(webhook.Config{ShopStore: shopStore}),
		LifecyclePolicy: policy,
	})

	shop := testutil.NewTestShop()
	require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

	return r, shop
}

func freshTimestamp() *int64 {
	ts := time.Now().Unix()
	return &ts
}

func TestDefaultLifecyclePolicy_IsStrict(t *testing.T) {
	t.Parallel()

	p := resolver.DefaultLifecyclePolicy()

	assert.Equal(t, resolver.TimestampRequire, p.Timestamp)
	assert.Equal(t, signature.DefaultReplayWindow, p.MaxAge)
	assert.Equal(t, signature.DefaultClockSkew, p.ClockSkew)
}

func TestLifecyclePolicy_ZeroValueMatchesDefault(t *testing.T) {
	t.Parallel()

	r, _ := newLifecycleResolver(t, resolver.LifecyclePolicy{})

	assert.Equal(t, resolver.DefaultLifecyclePolicy(), r.LifecyclePolicy())
}

// A correctly signed body for one transition must not authorize another.
func TestResolveLifecycleWebhook_CrossRouteReplayRejected(t *testing.T) {
	t.Parallel()

	events := []webhook.LifecycleEvent{
		webhook.EventAppActivated,
		webhook.EventAppDeactivated,
		webhook.EventAppDeleted,
	}

	for _, signed := range events {
		for _, expected := range events {
			if signed == expected {
				continue
			}

			t.Run(string(signed)+"_at_"+string(expected), func(t *testing.T) {
				t.Parallel()

				r, shop := newLifecycleResolver(t, resolver.LifecyclePolicy{})
				req := signedLifecycleBody(t, shop, signed, freshTimestamp())

				_, _, err := r.ResolveLifecycleWebhook(context.Background(), req, expected)

				require.Error(t, err)
				testutil.AssertSDKError(t, err, sdkerrors.TypeEventMismatch)
			})
		}
	}
}

func TestResolveLifecycleWebhook_MatchingEventAccepted(t *testing.T) {
	t.Parallel()

	events := []webhook.LifecycleEvent{
		webhook.EventAppActivated,
		webhook.EventAppDeactivated,
		webhook.EventAppDeleted,
	}

	for _, event := range events {
		t.Run(string(event), func(t *testing.T) {
			t.Parallel()

			r, shop := newLifecycleResolver(t, resolver.LifecyclePolicy{})
			req := signedLifecycleBody(t, shop, event, freshTimestamp())

			resolved, resolvedEvent, err := r.ResolveLifecycleWebhook(context.Background(), req, event)

			require.NoError(t, err)
			assert.Equal(t, shop.ShopID, resolved.ShopID)
			assert.Equal(t, string(event), resolvedEvent.Data.Event)
		})
	}
}

// An unknown event is not a special case: it simply is not the expected one.
func TestResolveLifecycleWebhook_UnknownEventRejected(t *testing.T) {
	t.Parallel()

	r, shop := newLifecycleResolver(t, resolver.LifecyclePolicy{})
	req := signedLifecycleBody(t, shop, "app.something.new", freshTimestamp())

	_, _, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppDeleted)

	require.Error(t, err)
	testutil.AssertSDKError(t, err, sdkerrors.TypeEventMismatch)
}

// A body with no data.event must not slip through as "matches anything".
func TestResolveLifecycleWebhook_MissingEventRejected(t *testing.T) {
	t.Parallel()

	r, shop := newLifecycleResolver(t, resolver.LifecyclePolicy{})
	req := signedLifecycleBody(t, shop, "", freshTimestamp())

	_, _, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppDeleted)

	require.Error(t, err)
	testutil.AssertSDKError(t, err, sdkerrors.TypeEventMismatch)
}

// An empty expectation is a programming error, not a wildcard.
func TestResolveLifecycleWebhook_EmptyExpectationRejected(t *testing.T) {
	t.Parallel()

	r, shop := newLifecycleResolver(t, resolver.LifecyclePolicy{})
	req := signedLifecycleBody(t, shop, webhook.EventAppDeleted, freshTimestamp())

	_, _, err := r.ResolveLifecycleWebhook(context.Background(), req, "")

	require.Error(t, err)
	testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidRequest)
}

func TestResolveLifecycleWebhook_Freshness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		timestamp *int64
		wantType  string
	}{
		{name: "fresh", timestamp: freshTimestamp()},
		{
			name:      "stale",
			timestamp: timestampPtr(time.Now().Add(-10 * time.Minute).Unix()),
			wantType:  sdkerrors.TypeTimestampExpired,
		},
		{
			name:      "future beyond skew",
			timestamp: timestampPtr(time.Now().Add(10 * time.Minute).Unix()),
			wantType:  sdkerrors.TypeTimestampExpired,
		},
		{name: "missing", timestamp: nil, wantType: sdkerrors.TypeTimestampExpired},
		// An explicit epoch must count as "no timestamp", not as a very old one,
		// so a body cannot dodge the check by sending zero.
		{name: "zero", timestamp: timestampPtr(0), wantType: sdkerrors.TypeTimestampExpired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, shop := newLifecycleResolver(t, resolver.LifecyclePolicy{})
			req := signedLifecycleBody(t, shop, webhook.EventAppActivated, tt.timestamp)

			_, _, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppActivated)

			if tt.wantType == "" {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			testutil.AssertSDKError(t, err, tt.wantType)
		})
	}
}

// A resolver-wide policy must actually reach the lifecycle path, not just sit in
// the config.
func TestResolveLifecycleWebhook_CustomWindow(t *testing.T) {
	t.Parallel()

	r, shop := newLifecycleResolver(t, resolver.LifecyclePolicy{MaxAge: 30 * time.Second})
	require.Equal(t, 30*time.Second, r.LifecyclePolicy().MaxAge)

	// Two minutes old: inside the 5-minute default, outside this resolver's window.
	req := signedLifecycleBody(
		t, shop, webhook.EventAppActivated,
		timestampPtr(time.Now().Add(-2*time.Minute).Unix()),
	)

	_, _, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppActivated)

	require.Error(t, err)
	testutil.AssertSDKError(t, err, sdkerrors.TypeTimestampExpired)
}

func TestResolveLifecycleWebhookWithPolicy_OverridesResolverPolicy(t *testing.T) {
	t.Parallel()

	r, shop := newLifecycleResolver(t, resolver.LifecyclePolicy{})
	req := signedLifecycleBody(t, shop, webhook.EventAppActivated, nil)

	// The resolver-wide policy requires a timestamp; this call does not.
	_, _, err := r.ResolveLifecycleWebhookWithPolicy(
		context.Background(), req, webhook.EventAppActivated,
		resolver.LifecyclePolicy{Timestamp: resolver.TimestampVerifyIfPresent},
	)

	require.NoError(t, err)
}

// Relaxing freshness must not relax the binding — it is not a policy field.
func TestResolveLifecycleWebhookWithPolicy_BindingStillEnforced(t *testing.T) {
	t.Parallel()

	r, shop := newLifecycleResolver(t, resolver.LifecyclePolicy{})
	req := signedLifecycleBody(t, shop, webhook.EventAppActivated, nil)

	_, _, err := r.ResolveLifecycleWebhookWithPolicy(
		context.Background(), req, webhook.EventAppDeleted,
		resolver.LifecyclePolicy{Timestamp: resolver.TimestampIgnore},
	)

	require.Error(t, err)
	testutil.AssertSDKError(t, err, sdkerrors.TypeEventMismatch)
}

// A bad signature wins over both later checks.
func TestResolveLifecycleWebhook_SignatureCheckedFirst(t *testing.T) {
	t.Parallel()

	r, shop := newLifecycleResolver(t, resolver.LifecyclePolicy{})

	// Wrong secret, wrong event, long-expired timestamp: still invalid_signature.
	req := signedLifecycleBody(t, &store.Shop{
		ShopID:     shop.ShopID,
		ShopURL:    shop.ShopURL,
		ShopSecret: "wrong-secret",
	}, webhook.EventAppActivated, timestampPtr(1))

	_, _, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppDeleted)

	require.Error(t, err)
	testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidSignature)
}

// A body that is both mismatched and stale reports the mismatch.
func TestResolveLifecycleWebhook_MismatchWinsOverStaleness(t *testing.T) {
	t.Parallel()

	r, shop := newLifecycleResolver(t, resolver.LifecyclePolicy{})
	req := signedLifecycleBody(t, shop, webhook.EventAppActivated, timestampPtr(1))

	_, _, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppDeleted)

	require.Error(t, err)
	testutil.AssertSDKError(t, err, sdkerrors.TypeEventMismatch)
}

// Lifecycle routes exist to serve shops the other paths reject.
func TestResolveLifecycleWebhook_InactiveShopStillServed(t *testing.T) {
	t.Parallel()

	shopStore := store.NewInMemoryStore()
	r := resolver.NewResolver(resolver.Config{
		ShopStore:       shopStore,
		WebhookVerifier: webhook.NewMiddleware(webhook.Config{ShopStore: shopStore}),
	})

	shop := testutil.NewTestShop(testutil.WithShopActive(false))
	require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

	req := signedLifecycleBody(t, shop, webhook.EventAppActivated, freshTimestamp())

	_, _, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppActivated)

	require.NoError(t, err)
}
