package signature

import (
	"net/url"
	"strings"
)

// RemoveSignatureFromQuery removes the shopware-shop-signature parameter from a query string
// while preserving the exact format and order of remaining parameters.
// This is critical because signatures are computed on the original query string format.
func RemoveSignatureFromQuery(query string) string {
	values, err := url.ParseQuery(query)
	if err != nil {
		return query
	}

	// If signature parameter doesn't exist, return original query
	if !values.Has("shopware-shop-signature") {
		return query
	}

	// Get the signature value(s) to remove
	sigValues := values["shopware-shop-signature"]
	values.Del("shopware-shop-signature")

	// Rebuild the query string by removing only the signature parameter
	// We use string manipulation to preserve the original parameter order
	result := query
	for _, sigVal := range sigValues {
		// Try different formats: &param=value, param=value&, or param=value (only param)
		patterns := []string{
			"&shopware-shop-signature=" + url.QueryEscape(sigVal),
			"shopware-shop-signature=" + url.QueryEscape(sigVal) + "&",
			"shopware-shop-signature=" + url.QueryEscape(sigVal),
		}

		for _, pattern := range patterns {
			if idx := findInString(result, pattern); idx != -1 {
				result = result[:idx] + result[idx+len(pattern):]
				break
			}
		}
	}

	return result
}

// PrepareQueryForSignature removes the signature parameter and returns the query string
// with URL-decoded values while preserving the original parameter order.
// Shopware signs the query string with decoded values in original order.
func PrepareQueryForSignature(query string) string {
	// First, remove the signature parameter while preserving order
	queryWithoutSig := RemoveSignatureFromQuery(query)

	// Parse the query to get decoded values
	values, err := url.ParseQuery(queryWithoutSig)
	if err != nil {
		return queryWithoutSig
	}

	// Split the query by & to preserve original order
	pairs := strings.Split(queryWithoutSig, "&")
	var result []string

	for _, pair := range pairs {
		if pair == "" {
			continue
		}
		eqIdx := strings.Index(pair, "=")
		if eqIdx == -1 {
			result = append(result, pair)
			continue
		}
		key := pair[:eqIdx]
		// Get the decoded value from parsed values
		if decodedValues, ok := values[key]; ok && len(decodedValues) > 0 {
			result = append(result, key+"="+decodedValues[0])
			// Remove used value for multi-value params
			values[key] = decodedValues[1:]
		}
	}

	return strings.Join(result, "&")
}

// findInString returns the index of substr in s, or -1 if not found
func findInString(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	if len(substr) > len(s) {
		return -1
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
