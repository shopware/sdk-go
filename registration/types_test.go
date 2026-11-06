package registration_test

import (
	"encoding/json"
	"testing"

	"github.com/shopware/sdk-go/internal/testutil"
	"github.com/shopware/sdk-go/registration"
	"github.com/shopware/sdk-go/sdkerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test struct field tags

func TestRequest_JSONTags(t *testing.T) {
	// This test verifies the JSON struct tags by marshaling/unmarshaling
	req := &registration.Request{
		ShopID:    "test-id",
		ShopURL:   "https://test.com",
		Timestamp: 1234567890,
		RawQuery:  "should-not-be-in-json",
	}

	// Marshal to JSON
	data, err := json.Marshal(req)
	require.NoError(t, err)

	// RawQuery has json:"-" tag so it should not be serialized
	assert.NotContains(t, string(data), "should-not-be-in-json")
	assert.Contains(t, string(data), "test-id")
	assert.Contains(t, string(data), "https://test.com")
}

func TestConfirmationRequest_JSONTags(t *testing.T) {
	// Verify JSON tags and non-serialized fields
	req := &registration.ConfirmationRequest{
		ShopID:    "test-id",
		ShopURL:   "https://test.com",
		APIKey:    "api-key",
		SecretKey: "secret-key",
		Timestamp: "1234567890",
		Signature: "should-not-be-in-json-sig",
		RawBody:   []byte("should-not-be-in-json-body"),
	}

	// Marshal to JSON
	data, err := json.Marshal(req)
	require.NoError(t, err)

	// Signature and RawBody have json:"-" tags so they should not be serialized
	assert.NotContains(t, string(data), "should-not-be-in-json-sig")
	assert.NotContains(t, string(data), "should-not-be-in-json-body")
	assert.Contains(t, string(data), "test-id")
	assert.Contains(t, string(data), "api-key")
}

// Table-driven test for comprehensive validation

func TestRequest_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		request     registration.Request
		expectError bool
		expectedMsg string
	}{
		{
			name: "valid request",
			request: registration.Request{
				ShopID:    "shop-123",
				ShopURL:   "https://shop.com",
				Timestamp: 1234567890,
			},
		},
		{
			name: "missing shop-id",
			request: registration.Request{
				ShopURL:   "https://shop.com",
				Timestamp: 1234567890,
			},
			expectError: true,
			expectedMsg: "shop-id is required",
		},
		{
			name: "missing shop-url",
			request: registration.Request{
				ShopID:    "shop-123",
				Timestamp: 1234567890,
			},
			expectError: true,
			expectedMsg: "shop-url is required",
		},
		{
			name: "missing timestamp",
			request: registration.Request{
				ShopID:  "shop-123",
				ShopURL: "https://shop.com",
			},
			expectError: true,
			expectedMsg: "timestamp is required",
		},
		{
			name:        "all fields missing",
			request:     registration.Request{},
			expectError: true,
			expectedMsg: "shop-id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.request.Validate()

			if tt.expectError {
				sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidRequest)
				assert.Equal(t, tt.expectedMsg, sdkErr.Message)
				assert.Equal(t, registration.StatusBadRequest, sdkErr.Code)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfirmationRequest_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		request     registration.ConfirmationRequest
		expectError bool
		expectedMsg string
	}{
		{
			name: "valid request",
			request: registration.ConfirmationRequest{
				ShopID:    "shop-123",
				ShopURL:   "https://shop.com",
				APIKey:    "api-key",
				SecretKey: "secret-key",
				Timestamp: "1234567890",
			},
		},
		{
			name: "missing shopId",
			request: registration.ConfirmationRequest{
				ShopURL:   "https://shop.com",
				APIKey:    "api-key",
				SecretKey: "secret-key",
				Timestamp: "1234567890",
			},
			expectError: true,
			expectedMsg: "shopId is required",
		},
		{
			name: "missing shopUrl",
			request: registration.ConfirmationRequest{
				ShopID:    "shop-123",
				APIKey:    "api-key",
				SecretKey: "secret-key",
				Timestamp: "1234567890",
			},
			expectError: true,
			expectedMsg: "shopUrl is required",
		},
		{
			name: "missing apiKey",
			request: registration.ConfirmationRequest{
				ShopID:    "shop-123",
				ShopURL:   "https://shop.com",
				SecretKey: "secret-key",
				Timestamp: "1234567890",
			},
			expectError: true,
			expectedMsg: "apiKey is required",
		},
		{
			name: "missing secretKey",
			request: registration.ConfirmationRequest{
				ShopID:    "shop-123",
				ShopURL:   "https://shop.com",
				APIKey:    "api-key",
				Timestamp: "1234567890",
			},
			expectError: true,
			expectedMsg: "secretKey is required",
		},
		{
			name:        "all fields missing",
			request:     registration.ConfirmationRequest{},
			expectError: true,
			expectedMsg: "shopId is required",
		},
		{
			name: "only timestamp provided",
			request: registration.ConfirmationRequest{
				Timestamp: "1234567890",
			},
			expectError: true,
			expectedMsg: "shopId is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.request.Validate()

			if tt.expectError {
				sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidRequest)
				assert.Equal(t, tt.expectedMsg, sdkErr.Message)
				assert.Equal(t, registration.StatusBadRequest, sdkErr.Code)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
