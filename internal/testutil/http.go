package testutil

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"time"

	"github.com/shopware/sdk-go/signature"
)

// NewSignedRequest creates an HTTP request with proper HMAC signature
func NewSignedRequest(method, url string, body []byte, secret string) *http.Request {
	req := httptest.NewRequestWithContext(context.Background(), method, url, bytes.NewReader(body))
	sig := signature.ComputeSignature(body, secret)
	req.Header.Set(signature.ShopSignatureHeader, sig)
	return req
}

// NewSignedQueryRequest creates a signed GET request with query parameters.
// The query is used verbatim, so callers that need to pass the resolver's
// freshness check must include a current `timestamp` parameter themselves —
// see NewSignedQueryRequestAt.
func NewSignedQueryRequest(url, query, secret string) *http.Request {
	sig := signature.ComputeSignature([]byte(query), secret)
	fullURL := url + "?" + query + "&shopware-shop-signature=" + sig
	return httptest.NewRequestWithContext(context.Background(), http.MethodGet, fullURL, nil)
}

// NewSignedQueryRequestAt creates a signed GET request with a `timestamp`
// parameter set to ts, mirroring how Shopware signs module and action button
// URLs. Pass time.Now() for a request that passes the freshness check.
func NewSignedQueryRequestAt(url, query, secret string, ts time.Time) *http.Request {
	return NewSignedQueryRequest(url, WithTimestamp(query, ts), secret)
}

// WithTimestamp appends a `timestamp` parameter to a query string.
func WithTimestamp(query string, ts time.Time) string {
	stamp := "timestamp=" + strconv.FormatInt(ts.Unix(), 10)
	if query == "" {
		return stamp
	}
	return query + "&" + stamp
}
