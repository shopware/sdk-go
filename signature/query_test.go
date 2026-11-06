package signature

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemoveSignatureFromQuery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{
			name:     "signature at end",
			query:    "shop-id=test&shop-url=https://test.com&shopware-shop-signature=abc123",
			expected: "shop-id=test&shop-url=https://test.com",
		},
		{
			name:     "signature at beginning",
			query:    "shopware-shop-signature=abc123&shop-id=test&shop-url=https://test.com",
			expected: "shop-id=test&shop-url=https://test.com",
		},
		{
			name:     "signature in middle",
			query:    "shop-id=test&shopware-shop-signature=abc123&shop-url=https://test.com",
			expected: "shop-id=test&shop-url=https://test.com",
		},
		{
			name:     "no signature",
			query:    "shop-id=test&shop-url=https://test.com",
			expected: "shop-id=test&shop-url=https://test.com",
		},
		{
			name:     "empty query",
			query:    "",
			expected: "",
		},
		{
			name:     "only signature",
			query:    "shopware-shop-signature=abc123",
			expected: "",
		},
		{
			name:     "signature with special characters",
			query:    "shop-id=test&shopware-shop-signature=abc%2B123&shop-url=https://test.com",
			expected: "shop-id=test&shop-url=https://test.com",
		},
		{
			name:     "multiple parameters with similar names",
			query:    "shop-id=test&shopware-shop-signature=abc123&shopware-shop-id=123",
			expected: "shop-id=test&shopware-shop-id=123",
		},
		{
			name:     "invalid query string (parse error)",
			query:    "shop-id=test&invalid=%ZZ&shopware-shop-signature=test",
			expected: "shop-id=test&invalid=%ZZ&shopware-shop-signature=test", // Returns original on parse error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := RemoveSignatureFromQuery(tt.query)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRemoveSignatureFromQuery_PreservesOrder(t *testing.T) {
	// This test ensures that parameter order is preserved exactly
	query := "a=1&b=2&c=3&shopware-shop-signature=sig&d=4&e=5"
	expected := "a=1&b=2&c=3&d=4&e=5"

	result := RemoveSignatureFromQuery(query)
	assert.Equal(t, expected, result)
}

func TestRemoveSignatureFromQuery_PreservesEncoding(t *testing.T) {
	// Ensure that URL encoding is preserved for other parameters
	query := "shop-id=test&shop-url=https%3A%2F%2Ftest.com&shopware-shop-signature=sig"
	expected := "shop-id=test&shop-url=https%3A%2F%2Ftest.com"

	result := RemoveSignatureFromQuery(query)
	assert.Equal(t, expected, result)
}

func TestFindInString(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		substr   string
		expected int
	}{
		{
			name:     "found at beginning",
			s:        "hello world",
			substr:   "hello",
			expected: 0,
		},
		{
			name:     "found at end",
			s:        "hello world",
			substr:   "world",
			expected: 6,
		},
		{
			name:     "found in middle",
			s:        "hello world",
			substr:   "lo wo",
			expected: 3,
		},
		{
			name:     "not found",
			s:        "hello world",
			substr:   "xyz",
			expected: -1,
		},
		{
			name:     "empty substring",
			s:        "hello world",
			substr:   "",
			expected: 0,
		},
		{
			name:     "empty string",
			s:        "",
			substr:   "hello",
			expected: -1,
		},
		{
			name:     "substring longer than string",
			s:        "hi",
			substr:   "hello",
			expected: -1,
		},
		{
			name:     "exact match",
			s:        "hello",
			substr:   "hello",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findInString(tt.s, tt.substr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRemoveSignatureFromQuery_WithURLEscaping(t *testing.T) {
	// Test that the function correctly handles URL-escaped signature values
	sigValue := "abc+123/xyz="
	escapedSig := url.QueryEscape(sigValue)
	query := "shop-id=test&shopware-shop-signature=" + escapedSig + "&other=value"

	result := RemoveSignatureFromQuery(query)

	// The signature should be removed
	assert.NotContains(t, result, "shopware-shop-signature")
	assert.Contains(t, result, "shop-id=test")
	assert.Contains(t, result, "other=value")
}

func TestPrepareQueryForSignature(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{
			name:     "decodes URL-encoded shop-url",
			query:    "shop-id=test&shop-url=https%3A%2F%2Fexample.com&timestamp=1234567890&shopware-shop-signature=sig123",
			expected: "shop-id=test&shop-url=https://example.com&timestamp=1234567890",
		},
		{
			name:     "preserves already decoded values",
			query:    "shop-id=test&shop-url=https://example.com&timestamp=1234567890",
			expected: "shop-id=test&shop-url=https://example.com&timestamp=1234567890",
		},
		{
			name:     "preserves parameter order",
			query:    "z=last&a=first&m=middle&shopware-shop-signature=sig",
			expected: "z=last&a=first&m=middle",
		},
		{
			name:     "decodes multiple encoded values",
			query:    "shop-id=test%2B123&shop-url=https%3A%2F%2Fexample.com%2Fpath&name=hello%20world",
			expected: "shop-id=test+123&shop-url=https://example.com/path&name=hello world",
		},
		{
			name:     "handles empty query",
			query:    "",
			expected: "",
		},
		{
			name:     "handles query with only signature",
			query:    "shopware-shop-signature=sig123",
			expected: "",
		},
		{
			name:     "handles special characters in values",
			query:    "param1=value%26with%26ampersands&param2=value%3Dwith%3Dequals",
			expected: "param1=value&with&ampersands&param2=value=with=equals",
		},
		{
			name:     "handles plus signs (space encoding)",
			query:    "name=hello+world&shop-id=test",
			expected: "name=hello world&shop-id=test",
		},
		{
			name:     "real-world Shopware registration query",
			query:    "shop-id=abc123&shop-url=https%3A%2F%2Fmy-shop.shopware.store&timestamp=1699999999&shopware-shop-signature=abc123def456",
			expected: "shop-id=abc123&shop-url=https://my-shop.shopware.store&timestamp=1699999999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PrepareQueryForSignature(tt.query)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPrepareQueryForSignature_SignatureVerification(t *testing.T) {
	// This test verifies the actual use case: Shopware signs with decoded values
	// The SDK must prepare the query string the same way for verification to succeed

	// Simulated raw query from Shopware (URL-encoded as it comes over HTTP)
	rawQuery := "shop-id=test-shop&shop-url=https%3A%2F%2Fexample.shopware.com&timestamp=1699999999&shopware-shop-signature=somesig"

	// What Shopware actually signed (decoded values, no signature)
	whatShopwareSigned := "shop-id=test-shop&shop-url=https://example.shopware.com&timestamp=1699999999"

	// Our function should produce the same string
	prepared := PrepareQueryForSignature(rawQuery)

	assert.Equal(t, whatShopwareSigned, prepared,
		"PrepareQueryForSignature should produce the same string that Shopware signed")
}

func TestPrepareQueryForSignature_PreservesOrderWithMixedEncoding(t *testing.T) {
	// Test that order is preserved even when some values are encoded and some aren't
	query := "first=1&encoded=hello%20world&third=3&plain=test&shopware-shop-signature=sig"
	expected := "first=1&encoded=hello world&third=3&plain=test"

	result := PrepareQueryForSignature(query)
	assert.Equal(t, expected, result)
}

func TestPrepareQueryForSignature_InvalidQueryString(t *testing.T) {
	// Test that invalid query strings (that fail url.ParseQuery) return the original
	// after signature removal. The %ZZ is an invalid percent-encoding.
	query := "shop-id=test&invalid=%ZZ"

	// Should return the query as-is since it can't be parsed
	result := PrepareQueryForSignature(query)
	assert.Equal(t, query, result)
}

func TestPrepareQueryForSignature_KeyWithoutValue(t *testing.T) {
	// Test handling of keys without equals sign (bare keys)
	query := "shop-id=test&barekey&another=value"
	expected := "shop-id=test&barekey&another=value"

	result := PrepareQueryForSignature(query)
	assert.Equal(t, expected, result)
}

func TestPrepareQueryForSignature_EmptyPairs(t *testing.T) {
	// Test handling of empty pairs (consecutive ampersands)
	query := "shop-id=test&&another=value"
	expected := "shop-id=test&another=value"

	result := PrepareQueryForSignature(query)
	assert.Equal(t, expected, result)
}

func TestPrepareQueryForSignature_MultipleValuesForSameKey(t *testing.T) {
	// Test handling of multiple values for the same key
	query := "key=value1&key=value2&other=test"
	expected := "key=value1&key=value2&other=test"

	result := PrepareQueryForSignature(query)
	assert.Equal(t, expected, result)
}
