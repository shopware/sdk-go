package webhook_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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

func createTestShop() *store.Shop {
	return testutil.NewTestShop()
}

func createTestWebhookPayload(t testing.TB) map[string]interface{} {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "order_placed_event.json"))
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &result))
	// Keep fixtures timestamp-neutral; inject a fresh timestamp for verifier window checks.
	result["timestamp"] = time.Now().Unix()

	return result
}

func TestVerifyWebhook_Success(t *testing.T) {
	// Setup
	shopStore := store.NewInMemoryStore()
	middleware := webhook.NewMiddleware(webhook.Config{ShopStore: shopStore})

	// Store test shop
	shop := createTestShop()
	err := shopStore.UpdateShop(context.Background(), shop)
	require.NoError(t, err)

	// Create webhook payload
	payload := createTestWebhookPayload(t)
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := testutil.NewSignedRequest(http.MethodPost, "/webhook", body, shop.ShopSecret)
	req.Header.Set("Sw-Version", "6.4.0.0")

	// Test
	verified, err := middleware.VerifyWebhook(context.Background(), req, "test-trace-123")

	// Assert
	require.NoError(t, err)
	require.NotNil(t, verified)
	assert.Equal(t, "test-trace-123", verified.TraceID)
	assert.Equal(t, "test-shop-123", verified.Shop.ShopID)
	assert.Equal(t, "test-shop-123", verified.Event.Source.ShopID)
	assert.Equal(t, "order.placed", verified.Event.Data.Event)
	require.NotNil(t, verified.Event.Data.Payload)
}

func TestVerifyWebhook_BodyTooLarge(t *testing.T) {
	shopStore := store.NewInMemoryStore()
	// Small limit so the oversized body is rejected before any auth work.
	middleware := webhook.NewMiddleware(webhook.Config{ShopStore: shopStore, MaxBodyBytes: 16})

	shop := createTestShop()
	require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

	body := bytes.Repeat([]byte("a"), 1024)
	req := testutil.NewSignedRequest(http.MethodPost, "/webhook", body, shop.ShopSecret)

	_, err := middleware.VerifyWebhook(context.Background(), req, "trace-too-large")
	require.Error(t, err)
	require.ErrorIs(t, err, sdkerrors.ErrPayloadTooLarge)

	var sdkErr *sdkerrors.Error
	require.ErrorAs(t, err, &sdkErr)
	assert.Equal(t, http.StatusRequestEntityTooLarge, sdkErr.Code)
}

func TestVerifyWebhook_ErrorPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupStore   func(t testing.TB, s store.ShopStore)
		buildReq     func(body []byte, secret string) *http.Request
		expectedType string
		msgContains  string
	}{
		{
			name: "invalid signature",
			setupStore: func(t testing.TB, s store.ShopStore) {
				require.NoError(t, s.UpdateShop(context.Background(), createTestShop()))
			},
			buildReq: func(body []byte, _ string) *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
				req.Header.Set(signature.ShopSignatureHeader, "invalid-signature-value")
				return req
			},
			expectedType: sdkerrors.TypeVerificationFailed,
			msgContains:  "Invalid signature",
		},
		{
			name: "missing signature",
			setupStore: func(t testing.TB, s store.ShopStore) {
				require.NoError(t, s.UpdateShop(context.Background(), createTestShop()))
			},
			buildReq: func(body []byte, _ string) *http.Request {
				return httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
			},
			expectedType: sdkerrors.TypeVerificationFailed,
		},
		{
			name: "empty signature",
			setupStore: func(t testing.TB, s store.ShopStore) {
				require.NoError(t, s.UpdateShop(context.Background(), createTestShop()))
			},
			buildReq: func(body []byte, _ string) *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
				req.Header.Set(signature.ShopSignatureHeader, "")
				return req
			},
			expectedType: sdkerrors.TypeVerificationFailed,
		},
		{
			name: "shop not found",
			buildReq: func(body []byte, _ string) *http.Request {
				return testutil.NewSignedRequest(http.MethodPost, "/webhook", body, "some-secret")
			},
			expectedType: sdkerrors.TypeShopNotActive,
			msgContains:  "Shop not found",
		},
		{
			name: "shop inactive",
			setupStore: func(t testing.TB, s store.ShopStore) {
				shop := createTestShop()
				shop.Active = false
				require.NoError(t, s.UpdateShop(context.Background(), shop))
			},
			buildReq: func(body []byte, secret string) *http.Request {
				return testutil.NewSignedRequest(http.MethodPost, "/webhook", body, secret)
			},
			expectedType: sdkerrors.TypeShopNotActive,
			msgContains:  "not active",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			shopStore := store.NewInMemoryStore()
			middleware := webhook.NewMiddleware(webhook.Config{ShopStore: shopStore})

			shop := createTestShop()
			if tt.setupStore != nil {
				tt.setupStore(t, shopStore)
			}

			body, err := json.Marshal(createTestWebhookPayload(t))
			require.NoError(t, err)

			req := tt.buildReq(body, shop.ShopSecret)

			verified, err := middleware.VerifyWebhook(context.Background(), req, "test-trace-123")

			require.Error(t, err)
			assert.Nil(t, verified)

			sdkErr := testutil.AssertSDKError(t, err, tt.expectedType)
			if tt.msgContains != "" {
				assert.Contains(t, sdkErr.Message, tt.msgContains)
			}
		})
	}
}

func TestVerifyWebhook_MalformedBody(t *testing.T) {
	// Setup
	shopStore := store.NewInMemoryStore()
	middleware := webhook.NewMiddleware(webhook.Config{ShopStore: shopStore})

	// Store test shop
	shop := createTestShop()
	err := shopStore.UpdateShop(context.Background(), shop)
	require.NoError(t, err)

	// Create INVALID JSON body
	invalidBody := []byte(`{"invalid": "json", "missing": }`)

	req := testutil.NewSignedRequest(http.MethodPost, "/webhook", invalidBody, shop.ShopSecret)

	// Test
	verified, err := middleware.VerifyWebhook(context.Background(), req, "test-trace-123")

	// Assert
	require.Error(t, err)
	assert.Nil(t, verified)

	sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidPayload)
	assert.Contains(t, sdkErr.Message, "invalid JSON")
}

func TestVerifyWebhook_EmptyBody(t *testing.T) {
	// Setup
	shopStore := store.NewInMemoryStore()
	middleware := webhook.NewMiddleware(webhook.Config{ShopStore: shopStore})

	// Store test shop
	shop := createTestShop()
	err := shopStore.UpdateShop(context.Background(), shop)
	require.NoError(t, err)

	// Create empty body
	emptyBody := []byte(`{}`)

	req := testutil.NewSignedRequest(http.MethodPost, "/webhook", emptyBody, shop.ShopSecret)

	// Test
	verified, err := middleware.VerifyWebhook(context.Background(), req, "test-trace-123")

	// Assert
	require.Error(t, err)
	assert.Nil(t, verified)

	testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidPayload)
}

func TestVerifyWebhook_MissingShopID(t *testing.T) {
	// Setup
	shopStore := store.NewInMemoryStore()
	middleware := webhook.NewMiddleware(webhook.Config{ShopStore: shopStore})

	// Store test shop
	shop := createTestShop()
	err := shopStore.UpdateShop(context.Background(), shop)
	require.NoError(t, err)

	// Create payload WITHOUT shop ID
	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"shopId": "", // Missing
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

	req := testutil.NewSignedRequest(http.MethodPost, "/webhook", body, shop.ShopSecret)

	// Test
	verified, err := middleware.VerifyWebhook(context.Background(), req, "test-trace-123")

	// Assert
	require.Error(t, err)
	assert.Nil(t, verified)

	sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidPayload)
	assert.Contains(t, sdkErr.Message, "shop ID")
}

func TestVerifyWebhook_TraceIDPropagation(t *testing.T) {
	// Setup
	shopStore := store.NewInMemoryStore()
	middleware := webhook.NewMiddleware(webhook.Config{ShopStore: shopStore})

	// Store test shop
	shop := createTestShop()
	err := shopStore.UpdateShop(context.Background(), shop)
	require.NoError(t, err)

	// Create webhook payload
	payload := createTestWebhookPayload(t)
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := testutil.NewSignedRequest(http.MethodPost, "/webhook", body, shop.ShopSecret)

	// Test with custom trace ID
	customTraceID := "custom-trace-id-xyz-789"
	verified, err := middleware.VerifyWebhook(context.Background(), req, customTraceID)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, customTraceID, verified.TraceID)
}

func TestVerifyWebhook_EventParsing(t *testing.T) {
	// Setup
	shopStore := store.NewInMemoryStore()
	middleware := webhook.NewMiddleware(webhook.Config{ShopStore: shopStore})

	// Store test shop
	shop := createTestShop()
	err := shopStore.UpdateShop(context.Background(), shop)
	require.NoError(t, err)

	// Create webhook payload with specific details
	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"shopId":         "test-shop-123",
			"url":            "https://test-shop.com",
			"appVersion":     "2.0.1",
			"inAppPurchases": "{\"hasActiveSubscription\":true}",
		},
		"data": map[string]interface{}{
			"event": "product.written",
			"payload": []map[string]interface{}{
				{
					"entity":        "product",
					"operation":     "update",
					"primaryKey":    "product-456",
					"updatedFields": []string{"price", "stock"},
				},
			},
		},
		"timestamp": time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := testutil.NewSignedRequest(http.MethodPost, "/webhook", body, shop.ShopSecret)
	req.Header.Set("Sw-Version", "6.5.0.0")
	req.Header.Set("User-Agent", "Shopware/6.5")

	// Test
	verified, err := middleware.VerifyWebhook(context.Background(), req, "trace-123")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "test-shop-123", verified.Event.Source.ShopID)
	assert.Equal(t, "https://test-shop.com", verified.Event.Source.URL)
	assert.Equal(t, "2.0.1", verified.Event.Source.AppVersion)
	require.NotNil(t, verified.Event.Source.InAppPurchases)
	assert.Equal(t, "{\"hasActiveSubscription\":true}", *verified.Event.Source.InAppPurchases)
	assert.Equal(t, "product.written", verified.Event.Data.Event)

	var changes []webhook.EntityChange
	require.NoError(t, verified.Event.Data.UnmarshalPayload(&changes))
	require.Len(t, changes, 1)
	assert.Equal(t, "product", changes[0].Entity)
	assert.Equal(t, "update", changes[0].Operation)
	assert.Equal(t, "product-456", changes[0].PrimaryKey)
	assert.Equal(t, []string{"price", "stock"}, changes[0].UpdatedFields)

	// Verify headers were captured
	assert.NotEmpty(t, verified.Event.Headers.ShopwareShopSignature)
	assert.Equal(t, "6.5.0.0", verified.Event.Headers.ShopwareVersion)
	assert.Equal(t, "Shopware/6.5", verified.Event.Headers.UserAgent)
}

func TestVerifyWebhook_MultiplePayloadChanges(t *testing.T) {
	// Setup
	shopStore := store.NewInMemoryStore()
	middleware := webhook.NewMiddleware(webhook.Config{ShopStore: shopStore})

	// Store test shop
	shop := createTestShop()
	err := shopStore.UpdateShop(context.Background(), shop)
	require.NoError(t, err)

	// Create webhook payload with MULTIPLE entity changes
	payload := map[string]interface{}{
		"source": map[string]interface{}{
			"shopId": "test-shop-123",
			"url":    "https://test-shop.com",
		},
		"data": map[string]interface{}{
			"event": "order.written",
			"payload": []map[string]interface{}{
				{
					"entity":        "order",
					"operation":     "update",
					"primaryKey":    "order-1",
					"updatedFields": []string{"status"},
				},
				{
					"entity":     "order_line_item",
					"operation":  "insert",
					"primaryKey": "line-item-1",
				},
				{
					"entity":        "order_delivery",
					"operation":     "update",
					"primaryKey":    "delivery-1",
					"updatedFields": []string{"tracking_code"},
				},
			},
		},
		"timestamp": time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := testutil.NewSignedRequest(http.MethodPost, "/webhook", body, shop.ShopSecret)

	// Test
	verified, err := middleware.VerifyWebhook(context.Background(), req, "trace-123")

	// Assert
	require.NoError(t, err)

	var changes []webhook.EntityChange
	require.NoError(t, verified.Event.Data.UnmarshalPayload(&changes))
	require.Len(t, changes, 3)

	assert.Equal(t, "order", changes[0].Entity)
	assert.Equal(t, "update", changes[0].Operation)
	assert.Equal(t, "order-1", changes[0].PrimaryKey)

	assert.Equal(t, "order_line_item", changes[1].Entity)
	assert.Equal(t, "insert", changes[1].Operation)

	assert.Equal(t, "order_delivery", changes[2].Entity)
	assert.Equal(t, "update", changes[2].Operation)
}

func TestVerifyWebhook_NilRequestBody(t *testing.T) {
	// Setup
	shopStore := store.NewInMemoryStore()
	middleware := webhook.NewMiddleware(webhook.Config{ShopStore: shopStore})

	// Store test shop
	shop := createTestShop()
	err := shopStore.UpdateShop(context.Background(), shop)
	require.NoError(t, err)

	// Create HTTP request with ACTUALLY NIL body (not empty body)
	req := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Path: "/webhook"},
		Header: http.Header{
			signature.ShopSignatureHeader: []string{"some-signature"},
		},
		Body: nil, // Explicitly nil body
	}

	// Test
	verified, err := middleware.VerifyWebhook(context.Background(), req, "test-trace-123")

	// Assert
	require.Error(t, err)
	assert.Nil(t, verified)

	sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidPayload)
	assert.Contains(t, sdkErr.Message, "failed to read request body")
	assert.Contains(t, sdkErr.Message, "request body is nil")
}

// errorReader is a custom reader that always returns an error
type errorReader struct{}

func (e *errorReader) Read(_ []byte) (int, error) {
	return 0, errors.New("simulated read error")
}

func (e *errorReader) Close() error {
	return nil
}

func TestVerifyWebhook_ReadBodyError(t *testing.T) {
	// Setup
	shopStore := store.NewInMemoryStore()
	middleware := webhook.NewMiddleware(webhook.Config{ShopStore: shopStore})

	// Store test shop
	shop := createTestShop()
	err := shopStore.UpdateShop(context.Background(), shop)
	require.NoError(t, err)

	// Create HTTP request with a body that fails to read
	req := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Path: "/webhook"},
		Header: http.Header{
			signature.ShopSignatureHeader: []string{"some-signature"},
		},
		Body: &errorReader{}, // Body that fails on read
	}

	// Test
	verified, err := middleware.VerifyWebhook(context.Background(), req, "test-trace-123")

	// Assert
	require.Error(t, err)
	assert.Nil(t, verified)

	sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidPayload)
	assert.Contains(t, sdkErr.Message, "failed to read request body")
	assert.Contains(t, sdkErr.Message, "simulated read error")
}

func TestVerifyWebhook_ExpiredTimestamp(t *testing.T) {
	// Setup
	shopStore := store.NewInMemoryStore()
	middleware := webhook.NewMiddleware(webhook.Config{ShopStore: shopStore})

	// Store test shop
	shop := createTestShop()
	err := shopStore.UpdateShop(context.Background(), shop)
	require.NoError(t, err)

	// Create webhook payload with EXPIRED timestamp (6 minutes ago)
	payload := createTestWebhookPayload(t)
	payload["timestamp"] = time.Now().Add(-6 * time.Minute).Unix()
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := testutil.NewSignedRequest(http.MethodPost, "/webhook", body, shop.ShopSecret)

	// Test
	verified, err := middleware.VerifyWebhook(context.Background(), req, "trace-123")

	// Assert
	require.Error(t, err)
	assert.Nil(t, verified)

	// Freshness failures now report timestamp_expired, matching the signed
	// query path, instead of the generic verification_failed.
	testutil.AssertSDKError(t, err, sdkerrors.TypeTimestampExpired)
}
