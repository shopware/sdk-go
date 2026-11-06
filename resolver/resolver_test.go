package resolver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopware/sdk-go/internal/testutil"
	"github.com/shopware/sdk-go/registration"
	"github.com/shopware/sdk-go/resolver"
	"github.com/shopware/sdk-go/sdkerrors"
	"github.com/shopware/sdk-go/signature"
	"github.com/shopware/sdk-go/store"
	"github.com/shopware/sdk-go/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestResolver(t *testing.T, inflightAllowance ...time.Duration) (*resolver.Resolver, store.ShopStore) {
	t.Helper()
	var allowance time.Duration
	if len(inflightAllowance) > 0 {
		allowance = inflightAllowance[0]
	}

	shopStore := store.NewInMemoryStore()

	return resolver.NewResolver(resolver.Config{
		ShopStore:         shopStore,
		WebhookVerifier:   webhook.NewMiddleware(webhook.Config{ShopStore: shopStore}),
		InflightAllowance: allowance,
	}), shopStore
}

// newResolverForStore builds a resolver over an existing store with the default
// policies. The webhook verifier is a real middleware over that same store,
// which is how app.New wires the two together.
func newResolverForStore(t *testing.T, shopStore store.ShopStore) *resolver.Resolver {
	t.Helper()

	return resolver.NewResolver(resolver.Config{
		ShopStore:       shopStore,
		WebhookVerifier: webhook.NewMiddleware(webhook.Config{ShopStore: shopStore}),
	})
}

func newLimitedResolver(t *testing.T, maxBodyBytes int64) *resolver.Resolver {
	t.Helper()
	shopStore := store.NewInMemoryStore()

	return resolver.NewResolver(resolver.Config{
		ShopStore: shopStore,
		WebhookVerifier: webhook.NewMiddleware(webhook.Config{
			ShopStore:    shopStore,
			MaxBodyBytes: maxBodyBytes,
		}),
		MaxBodyBytes: maxBodyBytes,
	})
}

func assertPayloadTooLarge(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	require.ErrorIs(t, err, sdkerrors.ErrPayloadTooLarge)
	var sdkErr *sdkerrors.Error
	require.ErrorAs(t, err, &sdkErr)
	assert.Equal(t, http.StatusRequestEntityTooLarge, sdkErr.Code)
}

func TestResolveFromPost_BodyTooLarge(t *testing.T) {
	r := newLimitedResolver(t, 16)
	body := bytes.Repeat([]byte("a"), 1024)
	req := httptest.NewRequest(http.MethodPost, "/app/action", bytes.NewReader(body))

	_, err := r.ResolveFromPost(context.Background(), req)
	assertPayloadTooLarge(t, err)
}

func TestResolveLifecycleWebhook_BodyTooLarge(t *testing.T) {
	r := newLimitedResolver(t, 16)
	body := bytes.Repeat([]byte("a"), 1024)
	req := httptest.NewRequest(http.MethodPost, "/app/activate", bytes.NewReader(body))

	_, _, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppActivated)
	assertPayloadTooLarge(t, err)
}

func loadWebhookPayloadFixture(t *testing.T) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "order_placed_event.json"))
	require.NoError(t, err)

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &payload))
	// Keep fixtures timestamp-neutral; inject a fresh timestamp for verifier window checks.
	payload["timestamp"] = time.Now().Unix()
	return payload
}

type readFailingCloser struct{}

func (readFailingCloser) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func (readFailingCloser) Close() error {
	return nil
}

func TestResolveFromQuery_Success(t *testing.T) {
	r, shopStore := newTestResolver(t)

	testShop := testutil.NewTestShop()
	require.NoError(t, shopStore.UpdateShop(context.Background(), testShop))

	req := testutil.NewSignedQueryRequestAt("http://example.com/api/auth/token", "shop-id=test-shop-123&shop-url=https://test-shop.com", testShop.ShopSecret, time.Now())

	shop, err := r.ResolveFromQuery(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, shop)
	assert.Equal(t, testShop.ShopID, shop.ShopID)
}

func TestResolveFromQuery_ErrorPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		storeShop   bool
		buildReq    func() *http.Request
		expectedErr string
		errContains string
	}{
		{
			name: "missing shop-id",
			buildReq: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "http://example.com/test?shopware-shop-signature=test", nil)
			},
			expectedErr: sdkerrors.TypeInvalidRequest,
			errContains: "shop-id",
		},
		{
			name: "missing signature",
			buildReq: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "http://example.com/test?shop-id=test-shop-123", nil)
			},
			expectedErr: sdkerrors.TypeInvalidSignature,
			errContains: "signature",
		},
		{
			name: "empty signature",
			buildReq: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "http://example.com/test?shop-id=test-shop-123&shopware-shop-signature=", nil)
			},
			expectedErr: sdkerrors.TypeInvalidSignature,
			errContains: "signature",
		},
		{
			name: "shop not found",
			buildReq: func() *http.Request {
				return testutil.NewSignedQueryRequest("http://example.com/api/auth/token", "shop-id=non-existent-shop&shop-url=https://test.com", "some-secret")
			},
			expectedErr: sdkerrors.TypeShopNotFound,
			errContains: "shop not found",
		},
		{
			name:      "invalid signature",
			storeShop: true,
			buildReq: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "http://example.com/api/auth/token?shop-id=test-shop-123&shop-url=https://test-shop.com&shopware-shop-signature=invalid-signature", nil)
			},
			expectedErr: sdkerrors.TypeInvalidSignature,
			errContains: "signature verification failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, shopStore := newTestResolver(t)

			if tt.storeShop {
				testShop := testutil.NewTestShop()
				require.NoError(t, shopStore.UpdateShop(context.Background(), testShop))
			}

			shop, err := r.ResolveFromQuery(context.Background(), tt.buildReq())

			require.Error(t, err)
			assert.Nil(t, shop)
			sdkErr := testutil.AssertSDKError(t, err, tt.expectedErr)
			assert.Contains(t, sdkErr.Message, tt.errContains)
		})
	}
}

func TestResolveFromQuery_FallbackToPreviousSecret_WithinWindow(t *testing.T) {
	r, shopStore := newTestResolver(t)

	rotatedAt := time.Now().Add(-30 * time.Second)
	testShop := testutil.NewTestShop()
	testShop.PreviousShopSecret = "old-secret"
	testShop.SecretsRotatedAt = &rotatedAt
	require.NoError(t, shopStore.UpdateShop(context.Background(), testShop))

	req := testutil.NewSignedQueryRequestAt("http://example.com/test", "shop-id=test-shop-123&shop-url=https://test-shop.com", "old-secret", time.Now())
	shop, err := r.ResolveFromQuery(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, testShop.ShopID, shop.ShopID)
}

func TestResolveFromQuery_FallbackToPreviousSecret_OutsideWindow(t *testing.T) {
	r, shopStore := newTestResolver(t)

	rotatedAt := time.Now().Add(-120 * time.Second)
	testShop := testutil.NewTestShop()
	testShop.PreviousShopSecret = "old-secret"
	testShop.SecretsRotatedAt = &rotatedAt
	require.NoError(t, shopStore.UpdateShop(context.Background(), testShop))

	req := testutil.NewSignedQueryRequestAt("http://example.com/test", "shop-id=test-shop-123&shop-url=https://test-shop.com", "old-secret", time.Now())
	shop, err := r.ResolveFromQuery(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, shop)
	testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidSignature)
}

func TestResolveFromQuery_FallbackToPreviousSecret_NoPreviousSecret(t *testing.T) {
	r, shopStore := newTestResolver(t)

	testShop := testutil.NewTestShop()
	require.NoError(t, shopStore.UpdateShop(context.Background(), testShop))

	req := testutil.NewSignedQueryRequestAt("http://example.com/test", "shop-id=test-shop-123&shop-url=https://test-shop.com", "wrong-secret", time.Now())
	shop, err := r.ResolveFromQuery(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, shop)
	testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidSignature)
}

func TestResolveFromQuery_CustomAllowance_WithinExtendedWindow(t *testing.T) {
	r, shopStore := newTestResolver(t, 2*time.Minute)

	rotatedAt := time.Now().Add(-90 * time.Second)
	testShop := testutil.NewTestShop()
	testShop.PreviousShopSecret = "old-secret"
	testShop.SecretsRotatedAt = &rotatedAt
	require.NoError(t, shopStore.UpdateShop(context.Background(), testShop))

	req := testutil.NewSignedQueryRequestAt("http://example.com/test", "shop-id=test-shop-123&shop-url=https://test-shop.com", "old-secret", time.Now())
	shop, err := r.ResolveFromQuery(context.Background(), req)

	require.NoError(t, err, "90s elapsed should be within a 120s custom allowance")
	assert.Equal(t, testShop.ShopID, shop.ShopID)
}

func TestResolveFromQuery_CustomAllowance_OutsideExtendedWindow(t *testing.T) {
	r, shopStore := newTestResolver(t, 2*time.Minute)

	rotatedAt := time.Now().Add(-150 * time.Second)
	testShop := testutil.NewTestShop()
	testShop.PreviousShopSecret = "old-secret"
	testShop.SecretsRotatedAt = &rotatedAt
	require.NoError(t, shopStore.UpdateShop(context.Background(), testShop))

	req := testutil.NewSignedQueryRequestAt("http://example.com/test", "shop-id=test-shop-123&shop-url=https://test-shop.com", "old-secret", time.Now())
	shop, err := r.ResolveFromQuery(context.Background(), req)

	require.Error(t, err, "150s elapsed should exceed a 120s custom allowance")
	assert.Nil(t, shop)
	testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidSignature)
}

func TestResolveFromQuery_CurrentSecretStillWorks_AfterRotation(t *testing.T) {
	r, shopStore := newTestResolver(t)

	rotatedAt := time.Now().Add(-30 * time.Second)
	testShop := testutil.NewTestShop()
	testShop.PreviousShopSecret = "old-secret"
	testShop.SecretsRotatedAt = &rotatedAt
	require.NoError(t, shopStore.UpdateShop(context.Background(), testShop))

	req := testutil.NewSignedQueryRequestAt("http://example.com/test", "shop-id=test-shop-123&shop-url=https://test-shop.com", testShop.ShopSecret, time.Now())
	shop, err := r.ResolveFromQuery(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, testShop.ShopID, shop.ShopID)
}

// Test ResolveFromWebhook

func TestResolveFromWebhook_Success(t *testing.T) {
	r, shopStore := newTestResolver(t)

	// Create and store test shop
	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	// Create webhook payload
	payload := loadWebhookPayloadFixture(t)
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	// Sign the request
	sig := signature.ComputeSignature(body, testShop.ShopSecret)

	// Create HTTP request
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Shopware-Shop-Signature", sig)

	// Test
	shop, event, err := r.ResolveFromWebhook(context.Background(), req, "test-trace-123")

	// Assert
	require.NoError(t, err)
	require.NotNil(t, shop)
	require.NotNil(t, event)
	assert.Equal(t, "test-shop-123", shop.ShopID)
	assert.Equal(t, "https://test-shop.com", shop.ShopURL)
	assert.Equal(t, "order.placed", event.Data.Event)
	require.NotNil(t, event.Data.Payload)
}

func TestResolveFromPost_SourcePayloadWithoutWebhookData(t *testing.T) {
	shopStore := store.NewInMemoryStore()
	r := newResolverForStore(t, shopStore)

	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"shopId":     "test-shop-123",
			"url":        "https://test-shop.com",
			"appVersion": "1.2.3",
		},
		"licenseKey":  "valid-license-key",
		"licenseHost": "test-shop.com",
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	sig := signature.ComputeSignature(body, testShop.ShopSecret)
	req := httptest.NewRequest(http.MethodPost, "/service/license/commercial/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Shopware-Shop-Signature", sig)

	shop, err := r.ResolveFromPost(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, shop)
	assert.Equal(t, testShop.ShopID, shop.ShopID)

	bodyAfterResolve, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	assert.Equal(t, body, bodyAfterResolve)
}

func TestResolveFromPost_FallbackToPreviousSecretWithinWindow(t *testing.T) {
	r, shopStore := newTestResolver(t)

	rotatedAt := time.Now().Add(-30 * time.Second)
	testShop := testutil.NewTestShop()
	testShop.PreviousShopSecret = "old-secret"
	testShop.SecretsRotatedAt = &rotatedAt
	require.NoError(t, shopStore.UpdateShop(context.Background(), testShop))

	body := []byte(`{"source":{"shopId":"test-shop-123"},"licenseKey":"current"}`)
	sig := signature.ComputeSignature(body, testShop.PreviousShopSecret)
	req := httptest.NewRequest(http.MethodPost, "/service/license/commercial/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Shopware-Shop-Signature", sig)

	shop, err := r.ResolveFromPost(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, shop)
	assert.Equal(t, testShop.ShopID, shop.ShopID)
}

func TestResolveFromPost_ReadBodyError(t *testing.T) {
	r, _ := newTestResolver(t)
	req := httptest.NewRequest(http.MethodPost, "/service/license/commercial/sync", nil)
	req.Body = readFailingCloser{}

	shop, err := r.ResolveFromPost(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, shop)
	require.ErrorIs(t, err, sdkerrors.ErrInvalidPayload)
	testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidPayload)
}

func TestResolveFromPost_ShopNotFound(t *testing.T) {
	r, _ := newTestResolver(t)
	body := []byte(`{"source":{"shopId":"missing-shop"}}`)
	req := httptest.NewRequest(http.MethodPost, "/service/license/commercial/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	shop, err := r.ResolveFromPost(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, shop)
	require.ErrorIs(t, err, sdkerrors.ErrShopNotFound)
	sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeShopNotFound)
	assert.Contains(t, sdkErr.Message, "missing-shop")
}

func TestResolveFromPost_ShopNotActive(t *testing.T) {
	r, shopStore := newTestResolver(t)

	testShop := testutil.NewTestShop()
	testShop.Active = false
	require.NoError(t, shopStore.UpdateShop(context.Background(), testShop))

	body := []byte(`{"source":{"shopId":"test-shop-123"}}`)
	sig := signature.ComputeSignature(body, testShop.ShopSecret)
	req := httptest.NewRequest(http.MethodPost, "/service/license/commercial/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Shopware-Shop-Signature", sig)

	shop, err := r.ResolveFromPost(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, shop)
	require.ErrorIs(t, err, sdkerrors.ErrShopNotActive)
	testutil.AssertSDKError(t, err, sdkerrors.TypeShopNotActive)
}

func TestResolveFromPost_StorageError(t *testing.T) {
	underlyingErr := errors.New("database unavailable")
	shopStore := testutil.NewFailingStore(underlyingErr)
	r := newResolverForStore(t, shopStore)

	body := []byte(`{"source":{"shopId":"test-shop-123"}}`)
	req := httptest.NewRequest(http.MethodPost, "/service/license/commercial/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	shop, err := r.ResolveFromPost(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, shop)
	require.ErrorIs(t, err, underlyingErr)
	testutil.AssertSDKError(t, err, sdkerrors.TypeStorageError)
}

func TestResolveFromPost_MissingSourceShopID(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing source", body: `{}`},
		{name: "empty source", body: `{"source": {}}`},
		{name: "empty shop id", body: `{"source": {"shopId": ""}}`},
		{name: "non-string shop id", body: `{"source": {"shopId": 1}}`},
		{name: "non-object source", body: `{"source": "invalid"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shopStore := store.NewInMemoryStore()
			r := newResolverForStore(t, shopStore)
			req := httptest.NewRequest(http.MethodPost, "/service/license/commercial/sync", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			shop, err := r.ResolveFromPost(context.Background(), req)

			require.Error(t, err)
			assert.Nil(t, shop)
			require.ErrorIs(t, err, sdkerrors.ErrMissingShopID)

			bodyAfterResolve, readErr := io.ReadAll(req.Body)
			require.NoError(t, readErr)
			assert.Equal(t, tt.body, string(bodyAfterResolve))
		})
	}
}

func TestResolveFromPost_InvalidSignatureChecksRawBody(t *testing.T) {
	shopStore := store.NewInMemoryStore()
	r := newResolverForStore(t, shopStore)

	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	body := []byte(`{"source":{"shopId":"test-shop-123"},"licenseKey":"current"}`)
	signedBody := []byte(`{"source":{"shopId":"test-shop-123"},"licenseKey":"previous"}`)
	sig := signature.ComputeSignature(signedBody, testShop.ShopSecret)
	req := httptest.NewRequest(http.MethodPost, "/service/license/commercial/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Shopware-Shop-Signature", sig)

	shop, err := r.ResolveFromPost(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, shop)
	require.ErrorIs(t, err, sdkerrors.ErrInvalidSignature)

	bodyAfterResolve, readErr := io.ReadAll(req.Body)
	require.NoError(t, readErr)
	assert.Equal(t, body, bodyAfterResolve)
}

func TestResolveFromPost_MissingSignature(t *testing.T) {
	shopStore := store.NewInMemoryStore()
	r := newResolverForStore(t, shopStore)

	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	body := []byte(`{"source":{"shopId":"test-shop-123"}}`)
	req := httptest.NewRequest(http.MethodPost, "/service/license/commercial/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	shop, err := r.ResolveFromPost(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, shop)
	require.ErrorIs(t, err, sdkerrors.ErrMissingSignature)

	bodyAfterResolve, readErr := io.ReadAll(req.Body)
	require.NoError(t, readErr)
	assert.Equal(t, body, bodyAfterResolve)
}

func TestResolveFromPost_InvalidJSONRestoresBody(t *testing.T) {
	body := `{"source":`
	shopStore := store.NewInMemoryStore()
	r := newResolverForStore(t, shopStore)
	req := httptest.NewRequest(http.MethodPost, "/service/license/commercial/sync", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	shop, err := r.ResolveFromPost(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, shop)
	require.ErrorIs(t, err, sdkerrors.ErrInvalidPayload)

	bodyAfterResolve, readErr := io.ReadAll(req.Body)
	require.NoError(t, readErr)
	assert.Equal(t, body, string(bodyAfterResolve))
}

func TestResolveFromWebhook_InvalidSignature(t *testing.T) {
	r, shopStore := newTestResolver(t)

	// Create and store test shop
	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	// Create webhook payload
	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"shopId": "test-shop-123",
			"url":    "https://test-shop.com",
		},
		"data": map[string]interface{}{
			"event": "order.placed",
			"payload": []map[string]interface{}{
				{
					"entity":     "order",
					"operation":  "insert",
					"primaryKey": "order-123",
				},
			},
		},
		"timestamp": time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	// Use INVALID signature
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Shopware-Shop-Signature", "invalid-signature")

	// Test
	shop, event, err := r.ResolveFromWebhook(context.Background(), req, "test-trace-123")

	// Assert
	require.Error(t, err)
	assert.Nil(t, shop)
	assert.Nil(t, event)
	testutil.AssertSDKError(t, err, sdkerrors.TypeVerificationFailed)
}

func TestResolveFromWebhook_MissingSignature(t *testing.T) {
	r, shopStore := newTestResolver(t)

	// Create and store test shop
	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	// Create webhook payload
	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"shopId": "test-shop-123",
			"url":    "https://test-shop.com",
		},
		"data": map[string]interface{}{
			"event": "order.placed",
			"payload": []map[string]interface{}{
				{
					"entity":     "order",
					"operation":  "insert",
					"primaryKey": "order-123",
				},
			},
		},
		"timestamp": time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	// Create request WITHOUT signature header
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))

	// Test
	shop, event, err := r.ResolveFromWebhook(context.Background(), req, "test-trace-123")

	// Assert
	require.Error(t, err)
	assert.Nil(t, shop)
	assert.Nil(t, event)
	testutil.AssertSDKError(t, err, sdkerrors.TypeVerificationFailed)
}

func TestResolveFromWebhook_ShopNotFound(t *testing.T) {
	r, _ := newTestResolver(t)

	// DO NOT store any shop

	// Create webhook payload for non-existent shop
	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"shopId": "non-existent-shop",
			"url":    "https://non-existent.com",
		},
		"data": map[string]interface{}{
			"event": "order.placed",
			"payload": []map[string]interface{}{
				{
					"entity":     "order",
					"operation":  "insert",
					"primaryKey": "order-123",
				},
			},
		},
		"timestamp": time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	// Sign with any secret
	sig := signature.ComputeSignature(body, "some-secret")

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Shopware-Shop-Signature", sig)

	// Test
	shop, event, err := r.ResolveFromWebhook(context.Background(), req, "test-trace-123")

	// Assert
	require.Error(t, err)
	assert.Nil(t, shop)
	assert.Nil(t, event)
	testutil.AssertSDKError(t, err, sdkerrors.TypeShopNotActive)
}

func TestResolveFromWebhook_MalformedBody(t *testing.T) {
	r, shopStore := newTestResolver(t)

	// Create and store test shop
	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	// Create INVALID JSON body
	body := []byte(`{"invalid": "json", "missing": }`)

	// Sign the invalid body
	sig := signature.ComputeSignature(body, testShop.ShopSecret)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Shopware-Shop-Signature", sig)

	// Test
	shop, event, err := r.ResolveFromWebhook(context.Background(), req, "test-trace-123")

	// Assert
	require.Error(t, err)
	assert.Nil(t, shop)
	assert.Nil(t, event)
	testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidPayload)
}

func TestResolveFromWebhook_TraceIDPropagation(t *testing.T) {
	r, shopStore := newTestResolver(t)

	// Create and store test shop
	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	// Create webhook payload
	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"shopId": "test-shop-123",
			"url":    "https://test-shop.com",
		},
		"data": map[string]interface{}{
			"event": "order.placed",
			"payload": []map[string]interface{}{
				{
					"entity":     "order",
					"operation":  "insert",
					"primaryKey": "order-123",
				},
			},
		},
		"timestamp": time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	sig := signature.ComputeSignature(body, testShop.ShopSecret)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Shopware-Shop-Signature", sig)

	// Test with custom trace ID
	customTraceID := "custom-trace-xyz-789"
	shop, event, err := r.ResolveFromWebhook(context.Background(), req, customTraceID)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, shop)
	require.NotNil(t, event)
	// Note: TraceID is not returned in the function signature,
	// but it's passed to the webhook middleware for logging
}

func TestResolveFromRequest_GET_Success(t *testing.T) {
	r, shopStore := newTestResolver(t)

	// Create a test shop
	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	req := testutil.NewSignedQueryRequestAt("http://example.com/module", "shop-id=test-shop-123&shop-url=https://test-shop.com", testShop.ShopSecret, time.Now())

	shop, event, err := r.ResolveFromRequest(context.Background(), req, "test-trace-123")

	// Assert
	require.NoError(t, err)
	require.NotNil(t, shop)
	assert.Nil(t, event) // GET requests don't have events
	assert.Equal(t, "test-shop-123", shop.ShopID)
}

func TestResolveFromRequest_POST_Success(t *testing.T) {
	r, shopStore := newTestResolver(t)

	// Create and store test shop
	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	// Create webhook payload
	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"shopId": "test-shop-123",
			"url":    "https://test-shop.com",
		},
		"data": map[string]interface{}{
			"event": "order.placed",
			"payload": []map[string]interface{}{
				{
					"entity":     "order",
					"operation":  "insert",
					"primaryKey": "order-123",
				},
			},
		},
		"timestamp": time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	sig := signature.ComputeSignature(body, testShop.ShopSecret)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Shopware-Shop-Signature", sig)

	// Test
	shop, event, err := r.ResolveFromRequest(context.Background(), req, "test-trace-123")

	// Assert
	require.NoError(t, err)
	require.NotNil(t, shop)
	require.NotNil(t, event) // POST requests have events
	assert.Equal(t, "test-shop-123", shop.ShopID)
	assert.Equal(t, "order.placed", event.Data.Event)
}

func TestResolveFromRequest_PUT_UsesWebhookFlow(t *testing.T) {
	r, shopStore := newTestResolver(t)

	// Create and store test shop
	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	// Create webhook payload
	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"shopId": "test-shop-123",
			"url":    "https://test-shop.com",
		},
		"data": map[string]interface{}{
			"event": "product.updated",
			"payload": []map[string]interface{}{
				{
					"entity":     "product",
					"operation":  "update",
					"primaryKey": "product-456",
				},
			},
		},
		"timestamp": time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	sig := signature.ComputeSignature(body, testShop.ShopSecret)

	// Test with PUT method (should use webhook flow, not query flow)
	req := httptest.NewRequest(http.MethodPut, "/action", bytes.NewReader(body))
	req.Header.Set("Shopware-Shop-Signature", sig)

	shop, event, err := r.ResolveFromRequest(context.Background(), req, "test-trace-123")

	// Assert
	require.NoError(t, err)
	require.NotNil(t, shop)
	require.NotNil(t, event) // Non-GET requests use webhook flow
	assert.Equal(t, "test-shop-123", shop.ShopID)
	assert.Equal(t, "product.updated", event.Data.Event)
}

func TestResolver_RemoveSignatureFromQuery_ParseError(t *testing.T) {
	r, shopStore := newTestResolver(t)

	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	reqURL := "http://example.com/test?shop-id=test-shop-123&invalid=%ZZ&shopware-shop-signature=test"
	req := httptest.NewRequest(http.MethodGet, reqURL, nil)

	_, err = r.ResolveFromQuery(context.Background(), req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature")
}

func TestResolver_ContextCancellation(t *testing.T) {
	r, shopStore := newTestResolver(t)

	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := testutil.NewSignedQueryRequest("http://example.com/test", "shop-id=test-shop-123&shop-url=https://test-shop.com", testShop.ShopSecret)

	_, err = r.ResolveFromQuery(ctx, req)

	require.Error(t, err)
}

func TestResolver_ResolveFromRequest_WebhookError(t *testing.T) {
	r, _ := newTestResolver(t)

	req := httptest.NewRequest(http.MethodPut, "/test", nil)

	_, _, err := r.ResolveFromRequest(context.Background(), req, "test-trace")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "webhook")
}

func TestResolver_ResolveFromQuery_SignatureAtEnd(t *testing.T) {
	r, shopStore := newTestResolver(t)

	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	req := testutil.NewSignedQueryRequestAt("http://example.com/test", "shop-id=test-shop-123&shop-url=https://test-shop.com", testShop.ShopSecret, time.Now())

	shop, err := r.ResolveFromQuery(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, "test-shop-123", shop.ShopID)
}

// Tests for ResolveLifecycleWebhook

func TestResolveLifecycleWebhook_Success_InactiveShop(t *testing.T) {
	r, shopStore := newTestResolver(t)

	// Create and store an INACTIVE test shop
	testShop := testutil.NewTestShop()
	testShop.Active = false // Shop is inactive
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	// Create lifecycle webhook payload (app.activated)
	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"shopId": testShop.ShopID,
			"url":    testShop.ShopURL,
		},
		"data": map[string]interface{}{
			"event":   "app.activated",
			"payload": []interface{}{},
		},
		"timestamp": time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	// Sign the request
	sig := signature.ComputeSignature(body, testShop.ShopSecret)

	// Create HTTP request
	req := httptest.NewRequest(http.MethodPost, "/app/activate", bytes.NewReader(body))
	req.Header.Set("Shopware-Shop-Signature", sig)

	// Test
	shop, event, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppActivated)

	// Assert - should succeed even though shop is inactive
	require.NoError(t, err)
	require.NotNil(t, shop)
	require.NotNil(t, event)
	assert.Equal(t, testShop.ShopID, shop.ShopID)
	assert.False(t, shop.Active) // Shop should still be inactive (lifecycle handler updates it)
	assert.Equal(t, "app.activated", event.Data.Event)
}

func TestResolveLifecycleWebhook_AfterRegistrationBeforeConfirmation(t *testing.T) {
	r, shopStore := newTestResolver(t)
	verifier := signature.NewVerifier()
	handler := registration.NewHandler(
		registration.Config{AppName: "TestApp", AppSecret: "test-secret"},
		shopStore,
		verifier,
		signature.NewDualVerifier(verifier, 0),
	)

	shopID := "pre-confirm-shop"
	shopURL := "https://pre-confirm-shop.test"
	timestamp := time.Now().Unix()
	rawQuery := fmt.Sprintf("shop-id=%s&shop-url=%s&timestamp=%d", shopID, shopURL, timestamp)

	regResp, err := handler.HandleRegistration(context.Background(), &registration.Request{
		ShopID:       shopID,
		ShopURL:      shopURL,
		Timestamp:    timestamp,
		RawQuery:     rawQuery,
		AppSignature: signature.ComputeSignature([]byte(rawQuery), "test-secret"),
	})
	require.NoError(t, err)

	// The activate webhook can arrive before the shop has confirmed, so the
	// registration secret must already authenticate it.
	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"shopId": shopID,
			"url":    shopURL,
		},
		"data": map[string]interface{}{
			"event":   string(webhook.EventAppActivated),
			"payload": []interface{}{},
		},
		"timestamp": time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/app/activate", bytes.NewReader(body))
	req.Header.Set("Shopware-Shop-Signature", signature.ComputeSignature(body, regResp.Secret))

	shop, event, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppActivated)

	require.NoError(t, err)
	require.NotNil(t, shop)
	require.NotNil(t, event)
	assert.Equal(t, shopID, shop.ShopID)
	assert.False(t, shop.RegistrationConfirmed)
	assert.Equal(t, string(webhook.EventAppActivated), event.Data.Event)
}

func TestResolveLifecycleWebhook_MissingSignature(t *testing.T) {
	r, shopStore := newTestResolver(t)

	// Create and store test shop
	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	// Create lifecycle webhook payload
	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"shopId": testShop.ShopID,
			"url":    testShop.ShopURL,
		},
		"data": map[string]interface{}{
			"event":   "app.deactivated",
			"payload": []interface{}{},
		},
		"timestamp": time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	// Create request WITHOUT signature header
	req := httptest.NewRequest(http.MethodPost, "/app/deactivate", bytes.NewReader(body))

	// Test
	shop, event, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppDeactivated)

	// Assert
	require.Error(t, err)
	assert.Nil(t, shop)
	assert.Nil(t, event)
	assert.Contains(t, err.Error(), "missing signature")
}

func TestResolveLifecycleWebhook_InvalidSignature(t *testing.T) {
	r, shopStore := newTestResolver(t)

	// Create and store test shop
	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	// Create lifecycle webhook payload
	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"shopId": testShop.ShopID,
			"url":    testShop.ShopURL,
		},
		"data": map[string]interface{}{
			"event":   "app.deleted",
			"payload": []interface{}{},
		},
		"timestamp": time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	// Create request with INVALID signature
	req := httptest.NewRequest(http.MethodPost, "/app/delete", bytes.NewReader(body))
	req.Header.Set("Shopware-Shop-Signature", "invalid-signature")

	// Test
	shop, event, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppDeleted)

	// Assert
	require.Error(t, err)
	assert.Nil(t, shop)
	assert.Nil(t, event)
	assert.Contains(t, err.Error(), "invalid signature")
}

func TestResolveLifecycleWebhook_ShopNotFound(t *testing.T) {
	r, _ := newTestResolver(t)

	// DO NOT store any shop

	// Create lifecycle webhook payload for non-existent shop
	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"shopId": "non-existent-shop",
			"url":    "https://non-existent.com",
		},
		"data": map[string]interface{}{
			"event":   "app.activated",
			"payload": []interface{}{},
		},
		"timestamp": time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	// Sign with any secret (doesn't matter, shop won't be found)
	sig := signature.ComputeSignature(body, "some-secret")

	req := httptest.NewRequest(http.MethodPost, "/app/activate", bytes.NewReader(body))
	req.Header.Set("Shopware-Shop-Signature", sig)

	// Test
	shop, event, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppActivated)

	// Assert
	require.Error(t, err)
	assert.Nil(t, shop)
	assert.Nil(t, event)
	assert.Contains(t, err.Error(), "shop not found")
}

func TestResolveLifecycleWebhook_InvalidJSON(t *testing.T) {
	r, shopStore := newTestResolver(t)

	// Create and store test shop
	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	// Create INVALID JSON body
	body := []byte(`{"invalid": "json", "missing": }`)

	// Sign the invalid body
	sig := signature.ComputeSignature(body, testShop.ShopSecret)

	req := httptest.NewRequest(http.MethodPost, "/app/activate", bytes.NewReader(body))
	req.Header.Set("Shopware-Shop-Signature", sig)

	// Test
	shop, event, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppActivated)

	// Assert
	require.Error(t, err)
	assert.Nil(t, shop)
	assert.Nil(t, event)
	assert.Contains(t, err.Error(), "invalid JSON")
}

func TestResolveLifecycleWebhook_MissingShopID(t *testing.T) {
	r, _ := newTestResolver(t)

	// Create lifecycle webhook payload WITHOUT shop ID
	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"url": "https://test.com",
		},
		"data": map[string]interface{}{
			"event":   "app.activated",
			"payload": []interface{}{},
		},
		"timestamp": time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	sig := signature.ComputeSignature(body, "some-secret")

	req := httptest.NewRequest(http.MethodPost, "/app/activate", bytes.NewReader(body))
	req.Header.Set("Shopware-Shop-Signature", sig)

	// Test
	shop, event, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppActivated)

	// Assert
	require.Error(t, err)
	assert.Nil(t, shop)
	assert.Nil(t, event)
	assert.Contains(t, err.Error(), "missing shop-id parameter")
}

func TestResolveLifecycleWebhook_NilBody(t *testing.T) {
	r, _ := newTestResolver(t)

	// Create request with nil body
	req := httptest.NewRequest(http.MethodPost, "/app/activate", nil)
	req.Body = nil
	req.Header.Set("Shopware-Shop-Signature", "some-sig")

	// Test
	shop, event, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppActivated)

	// Assert
	require.Error(t, err)
	assert.Nil(t, shop)
	assert.Nil(t, event)
	assert.Contains(t, err.Error(), "request body")
}

func TestResolveLifecycleWebhook_EventHeadersPopulated(t *testing.T) {
	r, shopStore := newTestResolver(t)

	// Create and store test shop
	testShop := testutil.NewTestShop()
	err := shopStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	// Create lifecycle webhook payload
	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"shopId": testShop.ShopID,
			"url":    testShop.ShopURL,
		},
		"data": map[string]interface{}{
			"event":   "app.activated",
			"payload": []interface{}{},
		},
		"timestamp": time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	// Sign the request
	sig := signature.ComputeSignature(body, testShop.ShopSecret)

	// Create HTTP request with additional headers
	req := httptest.NewRequest(http.MethodPost, "/app/activate", bytes.NewReader(body))
	req.Header.Set("Shopware-Shop-Signature", sig)
	req.Header.Set("Sw-Version", "6.5.0")
	req.Header.Set("User-Agent", "Shopware/6.5.0")

	// Test
	_, event, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppActivated)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, sig, event.Headers.ShopwareShopSignature)
	assert.Equal(t, "6.5.0", event.Headers.ShopwareVersion)
	assert.Equal(t, "Shopware/6.5.0", event.Headers.UserAgent)
	assert.NotEmpty(t, event.RawBody)
}

func TestResolveFromQuery_StorageError(t *testing.T) {
	storageErr := errors.New("connection refused")
	shopStore := testutil.NewFailingStore(storageErr)
	r := newResolverForStore(t, shopStore)

	q := "shop-id=shop-1&shopware-shop-signature=abc123"
	req := httptest.NewRequest(http.MethodGet, "/module/test?"+q, nil)

	shop, err := r.ResolveFromQuery(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, shop)

	sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeStorageError)
	assert.Equal(t, http.StatusInternalServerError, sdkErr.Code)
	assert.ErrorIs(t, err, storageErr)
}

func TestResolveLifecycleWebhook_StorageError(t *testing.T) {
	storageErr := errors.New("connection refused")
	shopStore := testutil.NewFailingStore(storageErr)
	r := newResolverForStore(t, shopStore)

	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"shopId": "shop-1",
			"url":    "https://shop-1.example.com",
		},
		"data": map[string]interface{}{
			"event":   "app.activated",
			"payload": []interface{}{},
		},
		"timestamp": time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	sig := signature.ComputeSignature(body, "any-secret")

	req := httptest.NewRequest(http.MethodPost, "/app/activate", bytes.NewReader(body))
	req.Header.Set("Shopware-Shop-Signature", sig)

	shop, event, err := r.ResolveLifecycleWebhook(context.Background(), req, webhook.EventAppActivated)

	require.Error(t, err)
	assert.Nil(t, shop)
	assert.Nil(t, event)

	sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeStorageError)
	assert.Equal(t, http.StatusInternalServerError, sdkErr.Code)
	assert.ErrorIs(t, err, storageErr)
}
