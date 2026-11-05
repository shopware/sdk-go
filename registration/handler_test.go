package registration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/shopware/sdk-go/internal/testutil"
	"github.com/shopware/sdk-go/registration"
	"github.com/shopware/sdk-go/sdkerrors"
	"github.com/shopware/sdk-go/signature"
	"github.com/shopware/sdk-go/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	testAppSecret = "test-secret"
	testShopID    = "test-shop"
	testShopURL   = "https://shop.test"
)

func newMockShopStore(t *testing.T) *testutil.MockShopStore {
	t.Helper()
	mockStore := new(testutil.MockShopStore)
	t.Cleanup(func() {
		mockStore.AssertExpectations(t)
	})
	return mockStore
}

func signedRegistrationRequest(shopID, shopURL string) *registration.Request {
	ts := time.Now().Unix()
	rawQuery := fmt.Sprintf("shop-id=%s&shop-url=%s&timestamp=%d", shopID, shopURL, ts)
	return &registration.Request{
		ShopID:       shopID,
		ShopURL:      shopURL,
		Timestamp:    ts,
		RawQuery:     rawQuery,
		AppSignature: signature.ComputeSignature([]byte(rawQuery), testAppSecret),
	}
}

// withShopSignature adds the proof Shopware sends on a re-registration: the query signed
// with the shop's current secret.
func withShopSignature(req *registration.Request, shopSecret string) *registration.Request {
	req.ShopSignature = signature.ComputeSignature([]byte(req.RawQuery), shopSecret)
	return req
}

// confirmationRequest builds a confirmation signed with pendingSecret. Pass a non-empty
// previousSecret to add the previous-secret proof required for a re-registration.
func confirmationRequest(t *testing.T, shopURL, pendingSecret, previousSecret string) *registration.ConfirmationRequest {
	t.Helper()

	req := registration.ConfirmationRequest{
		ShopID:    testShopID,
		ShopURL:   shopURL,
		APIKey:    "api-key",
		SecretKey: "secret-key",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}
	body, err := json.Marshal(req)
	require.NoError(t, err)

	req.RawBody = body
	req.Signature = signature.ComputeSignature(body, pendingSecret)
	if previousSecret != "" {
		req.PreviousSignature = signature.ComputeSignature(body, previousSecret)
	}

	return &req
}

// registerAndConfirm drives testShopID through a full first-time registration so that
// later re-registrations exercise the confirmed-shop path. Returns the active shop secret.
func registerAndConfirm(t *testing.T, handler registration.HandlerInterface) string {
	t.Helper()

	regResp, err := handler.HandleRegistration(context.Background(), signedRegistrationRequest(testShopID, testShopURL))
	require.NoError(t, err)

	_, err = handler.HandleConfirmation(context.Background(), confirmationRequest(t, testShopURL, regResp.Secret, ""))
	require.NoError(t, err)

	return regResp.Secret
}

func newStrictHandler(t *testing.T, shopStore store.ShopStore) registration.HandlerInterface {
	t.Helper()

	verifier := signature.NewVerifier()

	return registration.NewHandler(
		registration.Config{AppName: "TestApp", AppSecret: testAppSecret},
		shopStore, verifier, signature.NewDualVerifier(verifier, 0),
	)
}

func TestHandleRegistration_Success(t *testing.T) {
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, shopStore, verifier, signature.NewDualVerifier(verifier, 0))

	req := signedRegistrationRequest("test-shop-123", "https://test-shop.com")

	resp, err := handler.HandleRegistration(context.Background(), req)

	// Assert
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Proof)
	assert.NotEmpty(t, resp.Secret)
	assert.Len(t, resp.Secret, 64) // 32 bytes hex = 64 chars

	// Verify shop was stored
	shop, getErr := shopStore.GetShopByID(context.Background(), "test-shop-123")
	require.NoError(t, getErr)
	assert.Equal(t, "test-shop-123", shop.ShopID)
	assert.Equal(t, "https://test-shop.com", shop.ShopURL)
	assert.False(t, shop.Active)
	assert.Equal(t, resp.Secret, shop.ShopSecret)
	assert.Equal(t, resp.Secret, shop.PendingShopSecret)
}

func TestHandleRegistration_ValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		request      *registration.Request
		expectedType string
		msgContains  string
		// causeContains is part of the wrapped error, which names the reason.
		causeContains string
	}{
		{
			name: "missing shop-id",
			request: &registration.Request{
				ShopURL:   "https://test-shop.com",
				Timestamp: time.Now().Unix(),
			},
			expectedType: sdkerrors.TypeInvalidRequest,
			msgContains:  "shop-id",
		},
		{
			name: "missing shop-url",
			request: &registration.Request{
				ShopID:    "test-shop-123",
				Timestamp: time.Now().Unix(),
			},
			expectedType: sdkerrors.TypeInvalidRequest,
			msgContains:  "shop-url",
		},
		{
			name: "missing timestamp",
			request: &registration.Request{
				ShopID:  "test-shop-123",
				ShopURL: "https://test-shop.com",
			},
			expectedType: sdkerrors.TypeInvalidRequest,
			msgContains:  "timestamp",
		},
		{
			name: "expired timestamp",
			request: &registration.Request{
				ShopID:    "test-shop-123",
				ShopURL:   "https://test-shop.com",
				Timestamp: time.Now().Add(-6 * time.Minute).Unix(),
			},
			expectedType: sdkerrors.TypeTimestampExpired,
		},
		{
			name: "invalid shop URL",
			request: &registration.Request{
				ShopID:    "test-shop-123",
				ShopURL:   "://invalid-url-missing-scheme",
				Timestamp: time.Now().Unix(),
			},
			expectedType: sdkerrors.TypeInvalidRequest,
			msgContains:  "Invalid shop URL format",
		},
		{
			name: "shop URL with query",
			request: &registration.Request{
				ShopID:    "test-shop-123",
				ShopURL:   "https://shop.example.com/?tenant=x",
				Timestamp: time.Now().Unix(),
			},
			expectedType:  sdkerrors.TypeInvalidRequest,
			msgContains:   "Invalid shop URL format",
			causeContains: "query or fragment",
		},
		{
			name: "shop URL with empty query",
			request: &registration.Request{
				ShopID:    "test-shop-123",
				ShopURL:   "https://shop.example.com/?",
				Timestamp: time.Now().Unix(),
			},
			expectedType:  sdkerrors.TypeInvalidRequest,
			msgContains:   "Invalid shop URL format",
			causeContains: "query or fragment",
		},
		{
			name: "shop URL with fragment",
			request: &registration.Request{
				ShopID:    "test-shop-123",
				ShopURL:   "https://shop.example.com/#admin",
				Timestamp: time.Now().Unix(),
			},
			expectedType:  sdkerrors.TypeInvalidRequest,
			msgContains:   "Invalid shop URL format",
			causeContains: "query or fragment",
		},
		{
			name: "shop URL with empty fragment",
			request: &registration.Request{
				ShopID:    "test-shop-123",
				ShopURL:   "https://shop.example.com#",
				Timestamp: time.Now().Unix(),
			},
			expectedType:  sdkerrors.TypeInvalidRequest,
			msgContains:   "Invalid shop URL format",
			causeContains: "query or fragment",
		},
		{
			name: "shop URL with no scheme",
			request: &registration.Request{
				ShopID:    "test-shop-123",
				ShopURL:   "shop.example.com",
				Timestamp: time.Now().Unix(),
			},
			expectedType:  sdkerrors.TypeInvalidRequest,
			msgContains:   "Invalid shop URL format",
			causeContains: "http or https",
		},
		{
			name: "shop URL with ftp",
			request: &registration.Request{
				ShopID:    "test-shop-123",
				ShopURL:   "ftp://shop.example.com",
				Timestamp: time.Now().Unix(),
			},
			expectedType:  sdkerrors.TypeInvalidRequest,
			msgContains:   "Invalid shop URL format",
			causeContains: "http or https",
		},
		{
			name: "shop URL with no host",
			request: &registration.Request{
				ShopID:    "test-shop-123",
				ShopURL:   "https://",
				Timestamp: time.Now().Unix(),
			},
			expectedType:  sdkerrors.TypeInvalidRequest,
			msgContains:   "Invalid shop URL format",
			causeContains: "no host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			shopStore := store.NewInMemoryStore()
			verifier := signature.NewVerifier()
			handler := registration.NewHandler(
				registration.Config{AppName: "TestApp", AppSecret: "test-secret"},
				shopStore, verifier, signature.NewDualVerifier(verifier, 0),
			)

			resp, err := handler.HandleRegistration(context.Background(), tt.request)

			require.Error(t, err)
			assert.Nil(t, resp)

			sdkErr := testutil.AssertSDKError(t, err, tt.expectedType)
			if tt.msgContains != "" {
				assert.Contains(t, sdkErr.Message, tt.msgContains)
			}
			if tt.causeContains != "" {
				require.Error(t, sdkErr.Err)
				assert.Contains(t, sdkErr.Err.Error(), tt.causeContains)
			}
			_, getErr := shopStore.GetShopByID(context.Background(), tt.request.ShopID)
			require.ErrorIs(t, getErr, store.ErrShopNotFound, "a rejected registration stores nothing")
		})
	}
}

func TestHandleRegistration_URLSanitization(t *testing.T) {
	tests := []struct {
		name        string
		inputURL    string
		expectedURL string
	}{
		{
			name:        "removes trailing slash",
			inputURL:    "https://test-shop.com/",
			expectedURL: "https://test-shop.com",
		},
		{
			name:        "removes double slashes",
			inputURL:    "https://test-shop.com//path",
			expectedURL: "https://test-shop.com/path",
		},
		{
			name:        "removes trailing slash with path",
			inputURL:    "https://test-shop.com/path/",
			expectedURL: "https://test-shop.com/path",
		},
		{
			name:        "handles multiple double slashes",
			inputURL:    "https://test-shop.com//path//to//resource",
			expectedURL: "https://test-shop.com/path/to/resource",
		},
		{
			name:        "clean URL unchanged",
			inputURL:    "https://test-shop.com/path",
			expectedURL: "https://test-shop.com/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shopStore := store.NewInMemoryStore()
			verifier := signature.NewVerifier()

			config := registration.Config{
				AppName:   "TestApp",
				AppSecret: "test-secret",
			}

			handler := registration.NewHandler(config, shopStore, verifier, signature.NewDualVerifier(verifier, 0))

			req := signedRegistrationRequest("test-shop", tt.inputURL)

			resp, err := handler.HandleRegistration(context.Background(), req)

			require.NoError(t, err)
			assert.NotNil(t, resp)

			shop, getErr := shopStore.GetShopByID(context.Background(), "test-shop")
			require.NoError(t, getErr)
			assert.Equal(t, tt.expectedURL, shop.ShopURL)

			expectedProof := verifier.GenerateProof("test-shop", tt.inputURL, "TestApp", "test-secret")
			assert.Equal(t, expectedProof, resp.Proof)
		})
	}
}

func TestHandleRegistration_ProofUsesRawURL(t *testing.T) {
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, shopStore, verifier, signature.NewDualVerifier(verifier, 0))

	rawURL := "https://kalbacher-shop.de/"

	req := signedRegistrationRequest("test-shop", rawURL)

	resp, err := handler.HandleRegistration(context.Background(), req)
	require.NoError(t, err)

	proofWithRawURL := verifier.GenerateProof("test-shop", rawURL, "TestApp", "test-secret")
	proofWithSanitizedURL := verifier.GenerateProof("test-shop", "https://kalbacher-shop.de", "TestApp", "test-secret")

	assert.Equal(t, proofWithRawURL, resp.Proof)
	assert.NotEqual(t, proofWithSanitizedURL, resp.Proof)

	shop, err := shopStore.GetShopByID(context.Background(), "test-shop")
	require.NoError(t, err)
	assert.Equal(t, "https://kalbacher-shop.de", shop.ShopURL)
}

func TestHandleConfirmation_Success(t *testing.T) {
	// Setup
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, shopStore, verifier, signature.NewDualVerifier(verifier, 0))

	regReq := signedRegistrationRequest("test-shop-123", "https://test-shop.com")

	regResp, regErr := handler.HandleRegistration(context.Background(), regReq)
	require.NoError(t, regErr)

	confirmReq := registration.ConfirmationRequest{
		ShopID:    "test-shop-123",
		ShopURL:   "https://test-shop.com",
		APIKey:    "test-api-key",
		SecretKey: "test-secret-key",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}

	// Sign the confirmation request body
	body, err := json.Marshal(confirmReq)
	require.NoError(t, err)
	confirmReq.RawBody = body
	confirmReq.Signature = signature.ComputeSignature(body, regResp.Secret)

	// Test
	resp, err := handler.HandleConfirmation(context.Background(), &confirmReq)

	// Assert
	require.NoError(t, err)
	assert.True(t, resp.Success)

	// Verify shop was updated (credentials stored, but not yet activated - activation happens via separate webhook)
	shop, getErr := shopStore.GetShopByID(context.Background(), "test-shop-123")
	require.NoError(t, getErr)
	assert.False(t, shop.Active)
	assert.Equal(t, "test-api-key", shop.ClientID)
	assert.Equal(t, "test-secret-key", shop.ClientSecret)
}

func TestHandleConfirmation_InvalidSignature(t *testing.T) {
	// Setup
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, shopStore, verifier, signature.NewDualVerifier(verifier, 0))

	regReq := signedRegistrationRequest("test-shop-123", "https://test-shop.com")

	_, regErr := handler.HandleRegistration(context.Background(), regReq)
	require.NoError(t, regErr)

	confirmReq := registration.ConfirmationRequest{
		ShopID:    "test-shop-123",
		ShopURL:   "https://test-shop.com",
		APIKey:    "test-api-key",
		SecretKey: "test-secret-key",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}

	body, err := json.Marshal(confirmReq)
	require.NoError(t, err)
	confirmReq.RawBody = body
	confirmReq.Signature = "invalid-signature"

	// Test
	resp, err := handler.HandleConfirmation(context.Background(), &confirmReq)

	// Assert
	require.Error(t, err)
	assert.Nil(t, resp)

	var sdkErr2 *sdkerrors.Error
	require.ErrorAs(t, err, &sdkErr2)
	assert.Equal(t, sdkerrors.TypeInvalidSignature, sdkErr2.Type)

	shop, getErr := shopStore.GetShopByID(context.Background(), "test-shop-123")
	require.NoError(t, getErr)
	assert.Equal(t, "test-shop-123", shop.ShopID)
}

func TestHandleConfirmation_MissingPendingSecret(t *testing.T) {
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()

	handler := registration.NewHandler(
		registration.Config{AppName: "TestApp", AppSecret: testAppSecret},
		shopStore,
		verifier,
		signature.NewDualVerifier(verifier, 0),
	)

	shop := &store.Shop{
		ShopID:                "test-shop-123",
		ShopURL:               "https://test-shop.com",
		ShopSecret:            "current-secret",
		RegistrationConfirmed: true,
	}
	require.NoError(t, shopStore.UpdateShop(context.Background(), shop))

	confirmReq := registration.ConfirmationRequest{
		ShopID:    "test-shop-123",
		ShopURL:   "https://test-shop.com",
		APIKey:    "test-api-key",
		SecretKey: "test-secret-key",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}
	body, err := json.Marshal(confirmReq)
	require.NoError(t, err)
	confirmReq.RawBody = body
	confirmReq.Signature = signature.ComputeSignature(body, shop.ShopSecret)

	resp, err := handler.HandleConfirmation(context.Background(), &confirmReq)

	require.Error(t, err)
	assert.Nil(t, resp)
	require.ErrorIs(t, err, signature.ErrMissingPendingShopSecret)
	require.ErrorIs(t, err, sdkerrors.ErrRegistrationState)

	sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeRegistrationState)
	assert.Equal(t, registration.StatusConflict, sdkErr.Code)
	assert.Equal(t, "Registration confirmation is not pending", sdkErr.Message)
}

func TestHandleConfirmation_ShopNotFound(t *testing.T) {
	// Setup
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, shopStore, verifier, signature.NewDualVerifier(verifier, 0))

	// Test confirmation for non-existent shop
	confirmReq := registration.ConfirmationRequest{
		ShopID:    "non-existent-shop",
		ShopURL:   "https://test-shop.com",
		APIKey:    "test-api-key",
		SecretKey: "test-secret-key",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}

	body, err := json.Marshal(confirmReq)
	require.NoError(t, err)
	confirmReq.RawBody = body
	confirmReq.Signature = "some-signature"

	// Test
	resp, err := handler.HandleConfirmation(context.Background(), &confirmReq)

	// Assert
	require.Error(t, err)
	assert.Nil(t, resp)

	testutil.AssertSDKError(t, err, sdkerrors.TypeShopNotFound)
}

func TestHandleConfirmation_MissingAPIKey(t *testing.T) {
	// Setup
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, shopStore, verifier, signature.NewDualVerifier(verifier, 0))

	// Test confirmation with missing API key
	confirmReq := registration.ConfirmationRequest{
		ShopID:    "test-shop-123",
		ShopURL:   "https://test-shop.com",
		APIKey:    "", // Missing
		SecretKey: "test-secret-key",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}

	resp, err := handler.HandleConfirmation(context.Background(), &confirmReq)

	require.Error(t, err)
	assert.Nil(t, resp)

	sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidRequest)
	assert.Contains(t, sdkErr.Message, "apiKey")
}

func TestHandleConfirmation_MissingSecretKey(t *testing.T) {
	// Setup
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, shopStore, verifier, signature.NewDualVerifier(verifier, 0))

	// Test confirmation with missing secret key
	confirmReq := registration.ConfirmationRequest{
		ShopID:    "test-shop-123",
		ShopURL:   "https://test-shop.com",
		APIKey:    "test-api-key",
		SecretKey: "", // Missing
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}

	// Test
	resp, err := handler.HandleConfirmation(context.Background(), &confirmReq)

	// Assert
	require.Error(t, err)
	assert.Nil(t, resp)

	sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidRequest)
	assert.Contains(t, sdkErr.Message, "secretKey")
}

func TestHandleConfirmation_StoresCredentials(t *testing.T) {
	// Setup
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, shopStore, verifier, signature.NewDualVerifier(verifier, 0))

	regReq := signedRegistrationRequest("test-shop-123", "https://test-shop.com")

	regResp, err := handler.HandleRegistration(context.Background(), regReq)
	require.NoError(t, err)

	shop, err := shopStore.GetShopByID(context.Background(), "test-shop-123")
	require.NoError(t, err)
	assert.False(t, shop.Active)
	assert.False(t, shop.RegistrationConfirmed)

	confirmReq := registration.ConfirmationRequest{
		ShopID:    "test-shop-123",
		ShopURL:   "https://test-shop.com",
		APIKey:    "test-api-key",
		SecretKey: "test-secret-key",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}

	body, err := json.Marshal(confirmReq)
	require.NoError(t, err)
	confirmReq.RawBody = body
	confirmReq.Signature = signature.ComputeSignature(body, regResp.Secret)

	_, err = handler.HandleConfirmation(context.Background(), &confirmReq)
	require.NoError(t, err)

	shop, err = shopStore.GetShopByID(context.Background(), "test-shop-123")
	require.NoError(t, err)
	assert.False(t, shop.Active)
	assert.True(t, shop.RegistrationConfirmed)
	assert.Equal(t, "test-api-key", shop.ClientID)
	assert.Equal(t, "test-secret-key", shop.ClientSecret)
}

func TestHandleRegistration_StoreShopError(t *testing.T) {
	mockStore := newMockShopStore(t)
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, mockStore, verifier, signature.NewDualVerifier(verifier, 0))

	mockStore.On("GetShopByID", mock.Anything, "test-shop-123").
		Return(nil, store.ErrShopNotFound)
	mockStore.On("UpdateShop", mock.Anything, mock.AnythingOfType("*store.Shop")).
		Return(errors.New("database connection failed"))

	req := signedRegistrationRequest("test-shop-123", "https://test-shop.com")

	resp, err := handler.HandleRegistration(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeStorageError)
	assert.Contains(t, sdkErr.Message, "Failed to store shop registration")

	mockStore.AssertExpectations(t)
}

func TestHandleRegistration_TransientLookupError(t *testing.T) {
	mockStore := newMockShopStore(t)
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, mockStore, verifier, signature.NewDualVerifier(verifier, 0))

	mockStore.On("GetShopByID", mock.Anything, "test-shop-123").
		Return(nil, errors.New("connection timeout"))

	req := signedRegistrationRequest("test-shop-123", "https://test-shop.com")

	resp, err := handler.HandleRegistration(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeStorageError)
	assert.Contains(t, sdkErr.Message, "Failed to look up shop during registration")

	mockStore.AssertNotCalled(t, "UpdateShop", mock.Anything, mock.Anything)
}

func TestHandleRegistration_ReRegistration_StoreError(t *testing.T) {
	mockStore := newMockShopStore(t)
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, mockStore, verifier, signature.NewDualVerifier(verifier, 0))

	existingShop := testutil.NewTestShop(testutil.WithShopActive(true))
	mockStore.On("GetShopByID", mock.Anything, "test-shop-123").Return(existingShop, nil)
	mockStore.On("UpdateShop", mock.Anything, mock.AnythingOfType("*store.Shop")).
		Return(errors.New("database write failed"))

	req := withShopSignature(
		signedRegistrationRequest("test-shop-123", "https://test-shop-new.com"),
		existingShop.ShopSecret,
	)

	resp, err := handler.HandleRegistration(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeStorageError)
	assert.Contains(t, sdkErr.Message, "Failed to store shop registration")

	mockStore.AssertExpectations(t)
}

func TestHandleRegistration_ReRegistration_PreservesExistingData(t *testing.T) {
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, shopStore, verifier, signature.NewDualVerifier(verifier, 0))

	regReq := signedRegistrationRequest("test-shop-123", "https://test-shop.com")

	firstResp, err := handler.HandleRegistration(context.Background(), regReq)
	require.NoError(t, err)

	confirmReq := registration.ConfirmationRequest{
		ShopID:    "test-shop-123",
		ShopURL:   "https://test-shop.com",
		APIKey:    "original-api-key",
		SecretKey: "original-secret-key",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}
	body, err := json.Marshal(confirmReq)
	require.NoError(t, err)
	confirmReq.RawBody = body
	confirmReq.Signature = signature.ComputeSignature(body, firstResp.Secret)

	_, err = handler.HandleConfirmation(context.Background(), &confirmReq)
	require.NoError(t, err)

	shop, err := shopStore.GetShopByID(context.Background(), "test-shop-123")
	require.NoError(t, err)
	assert.Equal(t, "original-api-key", shop.ClientID)
	assert.Equal(t, "original-secret-key", shop.ClientSecret)
	originalSecret := shop.ShopSecret

	reRegReq := withShopSignature(
		signedRegistrationRequest("test-shop-123", "https://test-shop-new.com"),
		originalSecret,
	)

	reRegResp, err := handler.HandleRegistration(context.Background(), reRegReq)
	require.NoError(t, err)

	shop, err = shopStore.GetShopByID(context.Background(), "test-shop-123")
	require.NoError(t, err)
	assert.Equal(t, originalSecret, shop.ShopSecret)
	assert.Equal(t, "original-api-key", shop.ClientID)
	assert.Equal(t, "original-secret-key", shop.ClientSecret)
	assert.Equal(t, reRegResp.Secret, shop.PendingShopSecret)
	assert.Equal(t, "https://test-shop-new.com", shop.PendingShopURL)
	assert.True(t, shop.RegistrationConfirmed)
}

func TestHandleConfirmation_ReRegistration_PromotesPendingSecret(t *testing.T) {
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, shopStore, verifier, signature.NewDualVerifier(verifier, 0))

	regReq := signedRegistrationRequest("test-shop-123", "https://test-shop.com")
	firstResp, err := handler.HandleRegistration(context.Background(), regReq)
	require.NoError(t, err)

	confirmReq := registration.ConfirmationRequest{
		ShopID:    "test-shop-123",
		ShopURL:   "https://test-shop.com",
		APIKey:    "original-api-key",
		SecretKey: "original-secret-key",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}
	body, err := json.Marshal(confirmReq)
	require.NoError(t, err)
	confirmReq.RawBody = body
	confirmReq.Signature = signature.ComputeSignature(body, firstResp.Secret)
	_, err = handler.HandleConfirmation(context.Background(), &confirmReq)
	require.NoError(t, err)

	reRegReq := withShopSignature(
		signedRegistrationRequest("test-shop-123", "https://test-shop-new.com"),
		firstResp.Secret,
	)
	reRegResp, err := handler.HandleRegistration(context.Background(), reRegReq)
	require.NoError(t, err)

	shop, err := shopStore.GetShopByID(context.Background(), "test-shop-123")
	require.NoError(t, err)
	assert.True(t, shop.RegistrationConfirmed)

	// Confirm re-registration with the new (pending) secret plus the previous-secret proof
	reConfirmReq := registration.ConfirmationRequest{
		ShopID:    "test-shop-123",
		ShopURL:   "https://test-shop-new.com",
		APIKey:    "new-api-key",
		SecretKey: "new-secret-key",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}
	reBody, err := json.Marshal(reConfirmReq)
	require.NoError(t, err)
	reConfirmReq.RawBody = reBody
	reConfirmReq.Signature = signature.ComputeSignature(reBody, reRegResp.Secret)
	reConfirmReq.PreviousSignature = signature.ComputeSignature(reBody, firstResp.Secret)

	resp, err := handler.HandleConfirmation(context.Background(), &reConfirmReq)
	require.NoError(t, err)
	assert.True(t, resp.Success)

	// Verify pending was promoted: secret, URL, and credentials updated; pending fields cleared
	shop, err = shopStore.GetShopByID(context.Background(), "test-shop-123")
	require.NoError(t, err)
	assert.True(t, shop.RegistrationConfirmed)
	assert.Equal(t, reRegResp.Secret, shop.ShopSecret)
	assert.Equal(t, "https://test-shop-new.com", shop.ShopURL)
	assert.Equal(t, "new-api-key", shop.ClientID)
	assert.Equal(t, "new-secret-key", shop.ClientSecret)
	assert.Empty(t, shop.PendingShopSecret)
	assert.Empty(t, shop.PendingShopURL)
}

func TestHandleConfirmation_ExpiredTimestamp(t *testing.T) {
	// Setup
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, shopStore, verifier, signature.NewDualVerifier(verifier, 0))

	regReq := signedRegistrationRequest("test-shop-123", "https://test-shop.com")

	_, regErr := handler.HandleRegistration(context.Background(), regReq)
	require.NoError(t, regErr)

	confirmReq := registration.ConfirmationRequest{
		ShopID:    "test-shop-123",
		ShopURL:   "https://test-shop.com",
		APIKey:    "test-api-key",
		SecretKey: "test-secret-key",
		Timestamp: strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10),
	}

	body, err := json.Marshal(confirmReq)
	require.NoError(t, err)
	confirmReq.RawBody = body
	confirmReq.Signature = "some-signature"

	// Test
	resp, err := handler.HandleConfirmation(context.Background(), &confirmReq)

	// Assert
	require.Error(t, err)
	assert.Nil(t, resp)

	var sdkErr2 *sdkerrors.Error
	require.ErrorAs(t, err, &sdkErr2)
	assert.Equal(t, sdkerrors.TypeTimestampExpired, sdkErr2.Type)
}

func TestHandleConfirmation_InvalidSignature_NoDeleteAttempted(t *testing.T) {
	mockStore := newMockShopStore(t)
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, mockStore, verifier, signature.NewDualVerifier(verifier, 0))

	shop := testutil.NewTestShop(
		testutil.WithShopActive(false),
		testutil.WithRegistrationConfirmed(false),
	)
	shop.PendingShopSecret = "pending-secret"
	mockStore.On("GetShopByID", mock.Anything, "test-shop-123").Return(shop, nil)

	confirmReq := registration.ConfirmationRequest{
		ShopID:    "test-shop-123",
		ShopURL:   "https://test-shop.com",
		APIKey:    "test-api-key",
		SecretKey: "test-secret-key",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}

	body, err := json.Marshal(confirmReq)
	require.NoError(t, err)
	confirmReq.RawBody = body
	confirmReq.Signature = "invalid-signature"

	resp, err := handler.HandleConfirmation(context.Background(), &confirmReq)

	require.Error(t, err)
	assert.Nil(t, resp)

	testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidSignature)

	// DeleteShop must NOT be called on signature failure
	mockStore.AssertNotCalled(t, "DeleteShop", mock.Anything, mock.Anything)
}

func TestHandleConfirmation_StoreShopErrorDuringUpdate(t *testing.T) {
	// Setup
	mockStore := newMockShopStore(t)
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, mockStore, verifier, signature.NewDualVerifier(verifier, 0))

	// Mock GetShop to return an inactive shop (not yet activated)
	shop := testutil.NewTestShop(
		testutil.WithShopActive(false),
		testutil.WithRegistrationConfirmed(false),
	)
	shop.PendingShopSecret = "pending-secret"
	mockStore.On("GetShopByID", mock.Anything, "test-shop-123").Return(shop, nil)

	// Mock StoreShop (for update) to fail
	mockStore.On("UpdateShop", mock.Anything, mock.AnythingOfType("*store.Shop")).
		Return(errors.New("database update failed"))

	// Test confirmation with valid signature
	confirmReq := registration.ConfirmationRequest{
		ShopID:    "test-shop-123",
		ShopURL:   "https://test-shop.com",
		APIKey:    "test-api-key",
		SecretKey: "test-secret-key",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}

	body, err := json.Marshal(confirmReq)
	require.NoError(t, err)
	confirmReq.RawBody = body
	confirmReq.Signature = signature.ComputeSignature(body, shop.PendingShopSecret)

	// Test
	resp, err := handler.HandleConfirmation(context.Background(), &confirmReq)

	// Assert
	require.Error(t, err)
	assert.Nil(t, resp)

	sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeStorageError)
	assert.Contains(t, sdkErr.Message, "Failed to confirm shop registration")

	mockStore.AssertExpectations(t)
}

// A first registration has no prior secret, so a shop signature it happens to carry was
// never verified against anything. The ratchet must not record proof that did not happen.
func TestHandleRegistration_NewShop_DoesNotRatchet(t *testing.T) {
	shopStore := store.NewInMemoryStore()
	handler := newStrictHandler(t, shopStore)

	req := withShopSignature(signedRegistrationRequest("new-shop", "https://shop.test"), "irrelevant-secret")

	resp, err := handler.HandleRegistration(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	shop, err := shopStore.GetShopByID(context.Background(), "new-shop")
	require.NoError(t, err)
	assert.False(t, shop.HasVerifiedWithDoubleSignature)
}

func TestHandleConfirmation_PromotesPendingSecret(t *testing.T) {
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, shopStore, verifier, signature.NewDualVerifier(verifier, 0))

	regReq := signedRegistrationRequest("test-shop", "https://shop.test")
	regResp, err := handler.HandleRegistration(context.Background(), regReq)
	require.NoError(t, err)

	confirmReq := registration.ConfirmationRequest{
		ShopID:    "test-shop",
		ShopURL:   "https://shop.test",
		APIKey:    "api-key",
		SecretKey: "secret-key",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}
	body, err := json.Marshal(confirmReq)
	require.NoError(t, err)
	confirmReq.RawBody = body
	confirmReq.Signature = signature.ComputeSignature(body, regResp.Secret)

	_, err = handler.HandleConfirmation(context.Background(), &confirmReq)
	require.NoError(t, err)

	shop, err := shopStore.GetShopByID(context.Background(), "test-shop")
	require.NoError(t, err)
	assert.Equal(t, regResp.Secret, shop.ShopSecret)
	assert.Empty(t, shop.PendingShopSecret)
}

func TestHandleConfirmation_ReRegistration_RotatesPreviousSecret(t *testing.T) {
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, shopStore, verifier, signature.NewDualVerifier(verifier, 0))

	regReq := signedRegistrationRequest("test-shop", "https://shop.test")
	regResp, err := handler.HandleRegistration(context.Background(), regReq)
	require.NoError(t, err)

	confirmReq := registration.ConfirmationRequest{
		ShopID:    "test-shop",
		ShopURL:   "https://shop.test",
		APIKey:    "api-key",
		SecretKey: "secret-key",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}
	body, err := json.Marshal(confirmReq)
	require.NoError(t, err)
	confirmReq.RawBody = body
	confirmReq.Signature = signature.ComputeSignature(body, regResp.Secret)
	_, err = handler.HandleConfirmation(context.Background(), &confirmReq)
	require.NoError(t, err)

	oldSecret := regResp.Secret

	reRegReq := withShopSignature(signedRegistrationRequest("test-shop", "https://shop-new.test"), oldSecret)
	reRegResp, err := handler.HandleRegistration(context.Background(), reRegReq)
	require.NoError(t, err)

	reConfirmReq := registration.ConfirmationRequest{
		ShopID:    "test-shop",
		ShopURL:   "https://shop-new.test",
		APIKey:    "new-api",
		SecretKey: "new-secret",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}
	reBody, err := json.Marshal(reConfirmReq)
	require.NoError(t, err)
	reConfirmReq.RawBody = reBody
	reConfirmReq.Signature = signature.ComputeSignature(reBody, reRegResp.Secret)
	reConfirmReq.PreviousSignature = signature.ComputeSignature(reBody, oldSecret)

	_, err = handler.HandleConfirmation(context.Background(), &reConfirmReq)
	require.NoError(t, err)

	shop, err := shopStore.GetShopByID(context.Background(), "test-shop")
	require.NoError(t, err)
	assert.Equal(t, reRegResp.Secret, shop.ShopSecret)
	assert.Equal(t, oldSecret, shop.PreviousShopSecret)
	assert.NotNil(t, shop.SecretsRotatedAt)
	assert.Equal(t, "https://shop-new.test", shop.ShopURL)
	assert.Empty(t, shop.PendingShopSecret)
}

func TestHandleRegistration_MultipleReRegistrations_LastWins(t *testing.T) {
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()

	config := registration.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret",
	}

	handler := registration.NewHandler(config, shopStore, verifier, signature.NewDualVerifier(verifier, 0))

	// Initial registration + confirmation
	regReq := signedRegistrationRequest("test-shop", "https://shop.test")
	regResp, err := handler.HandleRegistration(context.Background(), regReq)
	require.NoError(t, err)

	confirmReq := registration.ConfirmationRequest{
		ShopID:    "test-shop",
		ShopURL:   "https://shop.test",
		APIKey:    "api-key",
		SecretKey: "secret-key",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}
	body, err := json.Marshal(confirmReq)
	require.NoError(t, err)
	confirmReq.RawBody = body
	confirmReq.Signature = signature.ComputeSignature(body, regResp.Secret)
	_, err = handler.HandleConfirmation(context.Background(), &confirmReq)
	require.NoError(t, err)

	activeSecret := regResp.Secret

	// First re-registration
	reReg1 := withShopSignature(signedRegistrationRequest("test-shop", "https://shop-v2.test"), activeSecret)
	reResp1, err := handler.HandleRegistration(context.Background(), reReg1)
	require.NoError(t, err)

	shop, err := shopStore.GetShopByID(context.Background(), "test-shop")
	require.NoError(t, err)
	assert.Equal(t, activeSecret, shop.ShopSecret, "active secret unchanged after first re-reg")
	assert.Equal(t, reResp1.Secret, shop.PendingShopSecret)

	// Second re-registration overwrites the first pending
	reReg2 := withShopSignature(signedRegistrationRequest("test-shop", "https://shop-v3.test"), activeSecret)
	reResp2, err := handler.HandleRegistration(context.Background(), reReg2)
	require.NoError(t, err)

	shop, err = shopStore.GetShopByID(context.Background(), "test-shop")
	require.NoError(t, err)
	assert.Equal(t, activeSecret, shop.ShopSecret, "active secret still unchanged")
	assert.Equal(t, reResp2.Secret, shop.PendingShopSecret, "pending is from second re-reg")
	assert.NotEqual(t, reResp1.Secret, shop.PendingShopSecret, "first re-reg pending was overwritten")
	assert.Equal(t, "https://shop-v3.test", shop.PendingShopURL)

	// Confirming with the first re-registration secret must fail
	failConfirm := registration.ConfirmationRequest{
		ShopID:    "test-shop",
		ShopURL:   "https://shop-v2.test",
		APIKey:    "api-v2",
		SecretKey: "secret-v2",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}
	failBody, err := json.Marshal(failConfirm)
	require.NoError(t, err)
	failConfirm.RawBody = failBody
	failConfirm.Signature = signature.ComputeSignature(failBody, reResp1.Secret)

	resp, err := handler.HandleConfirmation(context.Background(), &failConfirm)
	require.Error(t, err, "confirming with stale first re-reg secret must fail")
	assert.Nil(t, resp)

	// Confirming with the second (latest) re-registration secret succeeds
	okConfirm := registration.ConfirmationRequest{
		ShopID:    "test-shop",
		ShopURL:   "https://shop-v3.test",
		APIKey:    "api-v3",
		SecretKey: "secret-v3",
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}
	okBody, err := json.Marshal(okConfirm)
	require.NoError(t, err)
	okConfirm.RawBody = okBody
	okConfirm.Signature = signature.ComputeSignature(okBody, reResp2.Secret)
	okConfirm.PreviousSignature = signature.ComputeSignature(okBody, activeSecret)

	resp, err = handler.HandleConfirmation(context.Background(), &okConfirm)
	require.NoError(t, err)
	assert.True(t, resp.Success)

	shop, err = shopStore.GetShopByID(context.Background(), "test-shop")
	require.NoError(t, err)
	assert.Equal(t, reResp2.Secret, shop.ShopSecret)
	assert.Equal(t, "https://shop-v3.test", shop.ShopURL)
	assert.Equal(t, "api-v3", shop.ClientID)
	assert.Empty(t, shop.PendingShopSecret)
	assert.Equal(t, activeSecret, shop.PreviousShopSecret)
}

func TestHandleRegistration_InvalidAppSignature_NewShop(t *testing.T) {
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	handler := registration.NewHandler(
		registration.Config{AppName: "TestApp", AppSecret: testAppSecret},
		shopStore, verifier, dv,
	)

	req := &registration.Request{
		ShopID:       "new-shop",
		ShopURL:      "https://new-shop.test",
		Timestamp:    time.Now().Unix(),
		RawQuery:     "shop-id=new-shop&shop-url=https://new-shop.test&timestamp=1",
		AppSignature: "invalid-signature",
	}

	resp, err := handler.HandleRegistration(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidSignature)
}

func TestHandleRegistration_InvalidAppSignature_ReRegistration(t *testing.T) {
	shopStore := store.NewInMemoryStore()
	verifier := signature.NewVerifier()
	dv := signature.NewDualVerifier(verifier, 0)

	handler := registration.NewHandler(
		registration.Config{AppName: "TestApp", AppSecret: testAppSecret},
		shopStore, verifier, dv,
	)

	firstReq := signedRegistrationRequest("test-shop", "https://shop.test")
	_, err := handler.HandleRegistration(context.Background(), firstReq)
	require.NoError(t, err)

	reRegReq := &registration.Request{
		ShopID:       "test-shop",
		ShopURL:      "https://shop-v2.test",
		Timestamp:    time.Now().Unix(),
		RawQuery:     "shop-id=test-shop&shop-url=https://shop-v2.test&timestamp=1",
		AppSignature: "invalid-signature",
	}

	resp, err := handler.HandleRegistration(context.Background(), reRegReq)

	require.Error(t, err)
	assert.Nil(t, resp)

	testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidSignature)
}

// Re-registering a confirmed shop demands proof of its current secret, and only a proof
// that was actually checked flips the ratchet.
func TestHandleRegistration_ConfirmedShop_RequiresShopSignature(t *testing.T) {
	t.Run("valid shop signature accepted and ratchets", func(t *testing.T) {
		shopStore := store.NewInMemoryStore()
		handler := newStrictHandler(t, shopStore)
		activeSecret := registerAndConfirm(t, handler)

		req := withShopSignature(signedRegistrationRequest("test-shop", "https://shop-v2.test"), activeSecret)
		_, err := handler.HandleRegistration(context.Background(), req)
		require.NoError(t, err)

		shop, err := shopStore.GetShopByID(context.Background(), "test-shop")
		require.NoError(t, err)
		assert.True(t, shop.HasVerifiedWithDoubleSignature)
		assert.Equal(t, "https://shop-v2.test", shop.PendingShopURL)
	})

	t.Run("absent shop signature rejected without side effects", func(t *testing.T) {
		shopStore := store.NewInMemoryStore()
		handler := newStrictHandler(t, shopStore)
		activeSecret := registerAndConfirm(t, handler)

		before, err := shopStore.GetShopByID(context.Background(), "test-shop")
		require.NoError(t, err)

		req := signedRegistrationRequest("test-shop", "https://attacker.evil")
		resp, err := handler.HandleRegistration(context.Background(), req)
		require.Error(t, err)
		assert.Nil(t, resp)
		testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidSignature)

		after, err := shopStore.GetShopByID(context.Background(), "test-shop")
		require.NoError(t, err)
		assert.Equal(t, activeSecret, after.ShopSecret)
		assert.Equal(t, before.ShopURL, after.ShopURL)
		assert.Equal(t, before.PendingShopSecret, after.PendingShopSecret)
		assert.Equal(t, before.PendingShopURL, after.PendingShopURL)
	})

	t.Run("invalid shop signature rejected", func(t *testing.T) {
		shopStore := store.NewInMemoryStore()
		handler := newStrictHandler(t, shopStore)
		registerAndConfirm(t, handler)

		req := withShopSignature(signedRegistrationRequest("test-shop", "https://attacker.evil"), "wrong-secret")
		_, err := handler.HandleRegistration(context.Background(), req)
		require.Error(t, err)
		testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidSignature)
	})
}

// An unconfirmed shop has no established secret yet, so its re-registration stays open —
// this is the ordinary "registration retried before confirmation" case.
func TestHandleRegistration_UnconfirmedShop_NoShopSignatureRequired(t *testing.T) {
	shopStore := store.NewInMemoryStore()
	handler := newStrictHandler(t, shopStore)

	_, err := handler.HandleRegistration(context.Background(), signedRegistrationRequest("test-shop", "https://shop.test"))
	require.NoError(t, err)

	_, err = handler.HandleRegistration(context.Background(), signedRegistrationRequest("test-shop", "https://shop-v2.test"))
	require.NoError(t, err)

	shop, err := shopStore.GetShopByID(context.Background(), "test-shop")
	require.NoError(t, err)
	assert.False(t, shop.HasVerifiedWithDoubleSignature)
}

// The pending secret cannot authorize its own promotion: the app handed it out, so it
// proves nothing about who is confirming.
func TestHandleConfirmation_ReRegistration_RequiresPreviousSignature(t *testing.T) {
	newFixture := func(t *testing.T) (store.ShopStore, registration.HandlerInterface, string, string) {
		t.Helper()

		shopStore := store.NewInMemoryStore()
		handler := newStrictHandler(t, shopStore)
		activeSecret := registerAndConfirm(t, handler)

		reReg := withShopSignature(signedRegistrationRequest("test-shop", "https://shop-v2.test"), activeSecret)
		resp, err := handler.HandleRegistration(context.Background(), reReg)
		require.NoError(t, err)

		return shopStore, handler, activeSecret, resp.Secret
	}

	t.Run("both signatures accepted and promote the pending secret", func(t *testing.T) {
		shopStore, handler, activeSecret, pendingSecret := newFixture(t)

		req := confirmationRequest(t, "https://shop-v2.test", pendingSecret, activeSecret)
		_, err := handler.HandleConfirmation(context.Background(), req)
		require.NoError(t, err)

		shop, err := shopStore.GetShopByID(context.Background(), "test-shop")
		require.NoError(t, err)
		assert.Equal(t, pendingSecret, shop.ShopSecret)
		assert.Equal(t, activeSecret, shop.PreviousShopSecret)
		assert.True(t, shop.HasVerifiedWithDoubleSignature)
	})

	t.Run("absent previous signature rejected without promoting anything", func(t *testing.T) {
		shopStore, handler, activeSecret, pendingSecret := newFixture(t)

		before, err := shopStore.GetShopByID(context.Background(), "test-shop")
		require.NoError(t, err)

		req := confirmationRequest(t, "https://attacker.evil", pendingSecret, "")
		resp, err := handler.HandleConfirmation(context.Background(), req)
		require.Error(t, err)
		assert.Nil(t, resp)
		testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidSignature)

		after, err := shopStore.GetShopByID(context.Background(), "test-shop")
		require.NoError(t, err)
		assert.Equal(t, activeSecret, after.ShopSecret, "the active secret must survive a rejected confirmation")
		assert.Equal(t, before.ShopURL, after.ShopURL)
		assert.Equal(t, before.ClientID, after.ClientID)
		assert.Equal(t, before.ClientSecret, after.ClientSecret)
	})

	t.Run("invalid previous signature rejected", func(t *testing.T) {
		_, handler, _, pendingSecret := newFixture(t)

		req := confirmationRequest(t, "https://shop-v2.test", pendingSecret, "wrong-secret")
		_, err := handler.HandleConfirmation(context.Background(), req)
		require.Error(t, err)
		testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidSignature)
	})
}

// The escape hatch waives the requirement for legacy shops only, and a shop that already
// proved knowledge of its secret can never be downgraded back into it.
func TestHandleRegistration_AllowUnsignedReregistration(t *testing.T) {
	newLegacyHandler := func(t *testing.T, shopStore store.ShopStore) registration.HandlerInterface {
		t.Helper()

		verifier := signature.NewVerifier()

		return registration.NewHandler(
			registration.Config{AppName: "TestApp", AppSecret: testAppSecret, AllowUnsignedReregistration: true},
			shopStore, verifier, signature.NewDualVerifier(verifier, 0),
		)
	}

	t.Run("legacy shop re-registers without a shop signature", func(t *testing.T) {
		shopStore := store.NewInMemoryStore()
		handler := newLegacyHandler(t, shopStore)
		registerAndConfirm(t, handler)

		_, err := handler.HandleRegistration(context.Background(), signedRegistrationRequest("test-shop", "https://shop-v2.test"))
		require.NoError(t, err)
	})

	t.Run("a supplied signature still ratchets the shop back to strict", func(t *testing.T) {
		shopStore := store.NewInMemoryStore()
		handler := newLegacyHandler(t, shopStore)
		activeSecret := registerAndConfirm(t, handler)

		req := withShopSignature(signedRegistrationRequest("test-shop", "https://shop-v2.test"), activeSecret)
		_, err := handler.HandleRegistration(context.Background(), req)
		require.NoError(t, err)

		shop, err := shopStore.GetShopByID(context.Background(), "test-shop")
		require.NoError(t, err)
		require.True(t, shop.HasVerifiedWithDoubleSignature)

		// Ratcheted: the hatch no longer applies, so an unsigned attempt now fails.
		_, err = handler.HandleRegistration(context.Background(), signedRegistrationRequest("test-shop", "https://attacker.evil"))
		require.Error(t, err)
		testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidSignature)
	})
}

func TestHandleRegistration_QueryNormalization(t *testing.T) {
	shopID := "shop-1"
	shopURL := "https://shop.example.com"
	ts := time.Now().Unix()
	tsStr := strconv.FormatInt(ts, 10)
	decodedQuery := fmt.Sprintf("shop-id=%s&shop-url=%s&timestamp=%s", shopID, shopURL, tsStr)

	tests := []struct {
		name     string
		rawQuery string
	}{
		{
			name:     "url-encoded query",
			rawQuery: fmt.Sprintf("shop-id=%s&shop-url=%s&timestamp=%s", shopID, url.QueryEscape(shopURL), tsStr),
		},
		{
			name:     "query with signature param included",
			rawQuery: decodedQuery + "&shopware-shop-signature=somesig",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shopStore := store.NewInMemoryStore()
			verifier := signature.NewVerifier()
			handler := registration.NewHandler(
				registration.Config{AppName: "TestApp", AppSecret: testAppSecret},
				shopStore, verifier, signature.NewDualVerifier(verifier, 0),
			)

			req := &registration.Request{
				ShopID:       shopID,
				ShopURL:      shopURL,
				Timestamp:    ts,
				RawQuery:     tt.rawQuery,
				AppSignature: signature.ComputeSignature([]byte(decodedQuery), testAppSecret),
			}

			resp, err := handler.HandleRegistration(context.Background(), req)

			require.NoError(t, err)
			assert.NotEmpty(t, resp.Proof)
			assert.NotEmpty(t, resp.Secret)
		})
	}
}

func TestHandleRegistration_QueryNormalization_Reregistration(t *testing.T) {
	shopID := "shop-1"
	shopURL := "https://shop.example.com"
	ts := time.Now().Unix()
	tsStr := strconv.FormatInt(ts, 10)
	decodedQuery := fmt.Sprintf("shop-id=%s&shop-url=%s&timestamp=%s", shopID, shopURL, tsStr)

	tests := []struct {
		name     string
		rawQuery string
	}{
		{
			name:     "url-encoded parameters",
			rawQuery: fmt.Sprintf("shop-id=%s&shop-url=%s&timestamp=%s", url.QueryEscape(shopID), url.QueryEscape(shopURL), url.QueryEscape(tsStr)),
		},
		{
			name:     "mixed encoding",
			rawQuery: fmt.Sprintf("shop-id=%s&shop-url=%s&timestamp=%s", shopID, url.QueryEscape(shopURL), tsStr),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shopStore := store.NewInMemoryStore()
			verifier := signature.NewVerifier()
			handler := registration.NewHandler(
				registration.Config{AppName: "TestApp", AppSecret: testAppSecret},
				shopStore, verifier, signature.NewDualVerifier(verifier, 0),
			)

			initialReq := &registration.Request{
				ShopID:       shopID,
				ShopURL:      shopURL,
				Timestamp:    ts,
				RawQuery:     decodedQuery,
				AppSignature: signature.ComputeSignature([]byte(decodedQuery), testAppSecret),
			}

			initialResp, err := handler.HandleRegistration(context.Background(), initialReq)
			require.NoError(t, err)
			assert.NotEmpty(t, initialResp.Proof)
			assert.NotEmpty(t, initialResp.Secret)

			reregReq := &registration.Request{
				ShopID:       shopID,
				ShopURL:      shopURL,
				Timestamp:    ts,
				RawQuery:     tt.rawQuery,
				AppSignature: signature.ComputeSignature([]byte(decodedQuery), testAppSecret),
			}

			reregResp, err := handler.HandleRegistration(context.Background(), reregReq)
			require.NoError(t, err)
			assert.NotEmpty(t, reregResp.Proof)
			assert.NotEmpty(t, reregResp.Secret)
		})
	}
}
