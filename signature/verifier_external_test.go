package signature_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"

	"github.com/shopware/sdk-go/signature"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func computeTestHMAC(t testing.TB, data, secret string) string {
	t.Helper()
	h := hmac.New(sha256.New, []byte(secret))
	_, err := h.Write([]byte(data))
	require.NoError(t, err)
	return hex.EncodeToString(h.Sum(nil))
}

func TestVerifyRegistration(t *testing.T) {
	t.Parallel()
	verifier := signature.NewVerifier()
	appSecret := "test-app-secret"

	tests := []struct {
		name      string
		signature string
		rawQuery  string
		secret    string
		wantErr   bool
	}{
		{
			name:      "valid signature",
			signature: computeTestHMAC(t, "shop-id=test&shop-url=https://test.com&timestamp=123", appSecret),
			rawQuery:  "shop-id=test&shop-url=https://test.com&timestamp=123",
			secret:    appSecret,
			wantErr:   false,
		},
		{
			name:      "invalid signature",
			signature: "invalid-signature",
			rawQuery:  "shop-id=test&shop-url=https://test.com&timestamp=123",
			secret:    appSecret,
			wantErr:   true,
		},
		{
			name:      "empty signature",
			signature: "",
			rawQuery:  "shop-id=test&shop-url=https://test.com&timestamp=123",
			secret:    appSecret,
			wantErr:   true,
		},
		{
			name:      "wrong secret",
			signature: computeTestHMAC(t, "shop-id=test&shop-url=https://test.com&timestamp=123", appSecret),
			rawQuery:  "shop-id=test&shop-url=https://test.com&timestamp=123",
			secret:    "wrong-secret",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := verifier.VerifyRegistration(tt.signature, tt.rawQuery, tt.secret)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVerifyWebhook(t *testing.T) {
	t.Parallel()
	verifier := signature.NewVerifier()
	shopSecret := "test-shop-secret"
	testBody := []byte(`{"test":"data"}`)

	tests := []struct {
		name      string
		signature string
		body      []byte
		secret    string
		wantErr   bool
	}{
		{
			name:      "valid signature",
			signature: computeTestHMAC(t, string(testBody), shopSecret),
			body:      testBody,
			secret:    shopSecret,
			wantErr:   false,
		},
		{
			name:      "invalid signature",
			signature: "invalid-signature",
			body:      testBody,
			secret:    shopSecret,
			wantErr:   true,
		},
		{
			name:      "empty signature",
			signature: "",
			body:      testBody,
			secret:    shopSecret,
			wantErr:   true,
		},
		{
			name:      "wrong secret",
			signature: computeTestHMAC(t, string(testBody), shopSecret),
			body:      testBody,
			secret:    "wrong-secret",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := verifier.VerifyWebhook(tt.signature, tt.body, tt.secret)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVerifyTimestamp(t *testing.T) {
	t.Parallel()
	verifier := signature.NewVerifier()
	now := time.Now()

	tests := []struct {
		name      string
		timestamp string
		wantErr   bool
	}{
		{
			name:      "current timestamp",
			timestamp: strconv.FormatInt(now.Unix(), 10),
			wantErr:   false,
		},
		{
			name:      "recent timestamp (1 minute ago)",
			timestamp: strconv.FormatInt(now.Add(-1*time.Minute).Unix(), 10),
			wantErr:   false,
		},
		{
			name:      "expired timestamp (10 minutes ago)",
			timestamp: strconv.FormatInt(now.Add(-10*time.Minute).Unix(), 10),
			wantErr:   true,
		},
		{
			name:      "future timestamp (1 minute ahead)",
			timestamp: strconv.FormatInt(now.Add(1*time.Minute).Unix(), 10),
			wantErr:   false,
		},
		{
			name:      "future timestamp (10 minutes ahead)",
			timestamp: strconv.FormatInt(now.Add(10*time.Minute).Unix(), 10),
			wantErr:   true,
		},
		{
			name:      "empty timestamp",
			timestamp: "",
			wantErr:   true,
		},
		{
			name:      "invalid timestamp format",
			timestamp: "not-a-number",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := verifier.VerifyTimestamp(tt.timestamp)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGenerateProof(t *testing.T) {
	verifier := signature.NewVerifier()

	shopID := "test-shop-123"
	shopURL := "https://test-shop.com"
	appName := "TestApp"
	appSecret := "test-secret"

	proof1 := verifier.GenerateProof(shopID, shopURL, appName, appSecret)
	proof2 := verifier.GenerateProof(shopID, shopURL, appName, appSecret)

	assert.Equal(t, proof1, proof2)
	assert.Len(t, proof1, 64)

	proof3 := verifier.GenerateProof("different-shop", shopURL, appName, appSecret)
	assert.NotEqual(t, proof1, proof3)
}

func TestGenerateShopSecret(t *testing.T) {
	verifier := signature.NewVerifier()

	secret1 := verifier.GenerateShopSecret()
	secret2 := verifier.GenerateShopSecret()

	assert.NotEqual(t, secret1, secret2)
	assert.Len(t, secret1, 64)

	_, err := hex.DecodeString(secret1)
	require.NoError(t, err)
}

func TestComputeSignature(t *testing.T) {
	t.Parallel()
	largeData := make([]byte, 10000)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}
	binaryData := make([]byte, 256)
	for i := range binaryData {
		binaryData[i] = byte(i)
	}

	tests := []struct {
		name   string
		data   []byte
		secret string
	}{
		{name: "standard data", data: []byte("test data"), secret: "test-secret"},
		{name: "empty data", data: []byte(""), secret: "test-secret"},
		{name: "empty secret", data: []byte("test data"), secret: ""},
		{name: "json payload", data: []byte(`{"key":"value"}`), secret: "json-secret"},
		{name: "multiline data", data: []byte("line1\nline2\nline3"), secret: "multiline-secret"},
		{name: "whitespace in data", data: []byte("  data with spaces  "), secret: "space-secret"},
		{name: "large payload", data: largeData, secret: "test-secret"},
		{name: "unicode data", data: []byte("Hello 世界 🌍"), secret: "test-secret"},
		{name: "binary data", data: binaryData, secret: "test-secret"},
		{name: "special characters in secret", data: []byte("test data"), secret: "secret!@#$%^&*(){}[]|\\:;\"'<>,.?/~`"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sig := signature.ComputeSignature(tt.data, tt.secret)

			assert.Len(t, sig, 64)
			_, err := hex.DecodeString(sig)
			require.NoError(t, err)

			assert.Equal(t, sig, signature.ComputeSignature(tt.data, tt.secret))

			verifier := signature.NewVerifier()
			require.NoError(t, verifier.VerifyWebhook(sig, tt.data, tt.secret))
		})
	}

	t.Run("different data produces different signatures", func(t *testing.T) {
		t.Parallel()
		assert.NotEqual(t,
			signature.ComputeSignature([]byte("data1"), "secret"),
			signature.ComputeSignature([]byte("data2"), "secret"),
		)
	})

	t.Run("different secrets produce different signatures", func(t *testing.T) {
		t.Parallel()
		assert.NotEqual(t,
			signature.ComputeSignature([]byte("data"), "secret1"),
			signature.ComputeSignature([]byte("data"), "secret2"),
		)
	})

	t.Run("matches manual HMAC", func(t *testing.T) {
		t.Parallel()
		data := []byte("test data")
		secret := "test-secret"
		assert.Equal(t, computeTestHMAC(t, string(data), secret), signature.ComputeSignature(data, secret))
	})
}
