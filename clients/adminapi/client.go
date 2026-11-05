package adminapi

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"net/http"
	"slices"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/shopware/sdk-go/clients/adminapi/tokenstorage"
)

// Client is an authenticated client for the Shopware Admin API.
//
// It logs in on the first request, keeps the token in the configured
// TokenStorage, refreshes it before it expires and retries a request once
// when the shop answers 401. A Client is safe for concurrent use.
type Client struct {
	baseURL     string
	credentials Credentials
	httpClient  *http.Client
	headers     map[string]string
	userAgent   string
	maxBody     int64

	storage  tokenstorage.TokenStorage
	tokenKey string
	locker   tokenstorage.Locker

	// tokenGroup collapses concurrent token fetches into one request.
	tokenGroup singleflight.Group

	// tokenMu serializes every read-check-write of the stored token (refresh
	// and invalidation), so none of them writes back a stale token. It is a
	// channel so that waiting for it respects the context.
	tokenMu chan struct{}

	// pending is a token the shop issued but the storage failed to keep.
	// Shopware revoked the previous refresh token when it issued this one,
	// so it is retried before another exchange. Guarded by tokenMu.
	pending *pendingToken

	now func() time.Time
}

// pendingToken is an issued token that is not stored yet, with the stored
// token it replaces. It is stored later only if the storage still holds that
// token, so it never overwrites a newer one.
type pendingToken struct {
	token    tokenstorage.Token
	replaces tokenstorage.Token
}

// NewClient returns a Client for config. It returns an error when BaseURL or
// Credentials are missing or invalid.
func NewClient(config Config) (*Client, error) {
	baseURL, err := config.validate()
	if err != nil {
		return nil, err
	}

	c := &Client{
		baseURL:     baseURL,
		credentials: config.Credentials,
		httpClient:  newHTTPClient(config.HTTPClient),
		headers:     canonicalHeaders(config.Headers),
		userAgent:   config.UserAgent,
		maxBody:     config.MaxResponseBytes,
		storage:     config.TokenStorage,
		tokenKey:    config.TokenStorageKey,
		locker:      config.Locker,
		tokenMu:     make(chan struct{}, 1),
		now:         time.Now,
	}
	if c.userAgent == "" {
		c.userAgent = DefaultUserAgent
	}
	if c.maxBody == 0 {
		c.maxBody = DefaultMaxResponseBytes
	}
	if c.storage == nil {
		c.storage = tokenstorage.NewInMemory()
	}
	if c.tokenKey == "" {
		c.tokenKey = defaultTokenKey(baseURL, config.Credentials.Identity())
	}
	return c, nil
}

// canonicalHeaders copies headers with canonical names, so names that differ
// only in case become one header, and later changes to the caller's map do
// not affect the client. Of names that differ only in case, the one that
// sorts last wins, so the result does not depend on map order.
func canonicalHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for _, key := range slices.Sorted(maps.Keys(headers)) {
		out[http.CanonicalHeaderKey(key)] = headers[key]
	}
	return out
}

// newHTTPClient copies hc (or creates a client) and turns off following
// redirects unless the caller configured a redirect policy.
func newHTTPClient(hc *http.Client) *http.Client {
	var c http.Client
	if hc != nil {
		c = *hc
	} else {
		c.Timeout = DefaultTimeout
	}
	if c.CheckRedirect == nil {
		c.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return &c
}

// defaultTokenKey hashes the shop URL and the principal, so keys never
// reveal client IDs or user names and never collide between shops. The NUL
// separator is unambiguous because a validated BaseURL cannot contain NUL
// (url.Parse rejects control characters).
func defaultTokenKey(baseURL, identity string) string {
	sum := sha256.Sum256([]byte(baseURL + "\x00" + identity))
	return "v1:" + hex.EncodeToString(sum[:])
}
