package adminapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/shopware/app-sdk-go/clients/adminapi/tokenstorage"
)

const (
	// DefaultTimeout is the timeout of the HTTP client the Client creates
	// when Config.HTTPClient is nil.
	DefaultTimeout = 30 * time.Second

	// DefaultMaxResponseBytes is the response size limit used when
	// Config.MaxResponseBytes is zero.
	DefaultMaxResponseBytes = 32 << 20 // 32 MiB
)

// Config configures a Client. BaseURL and Credentials are required.
type Config struct {
	// BaseURL is the shop URL without "/api", for example
	// "https://shop.example.com". It may contain a path, but no query or
	// fragment. A trailing slash is removed.
	BaseURL string

	// Credentials decide how the client logs in: IntegrationCredentials,
	// PasswordCredentials, RefreshTokenCredentials or your own type.
	Credentials Credentials

	// HTTPClient sends the requests. If nil, a client with DefaultTimeout is
	// used. Set it to add timeouts, a proxy, instrumentation or retries (via
	// its Transport). The Client works on a copy; if CheckRedirect is nil,
	// the copy does not follow redirects.
	HTTPClient *http.Client

	// Headers are sent with every request, including token requests. Names
	// are case-insensitive. The client keeps a copy.
	Headers map[string]string

	// UserAgent replaces DefaultUserAgent.
	UserAgent string

	// TokenStorage keeps tokens between requests and clients. If nil, an
	// in-memory storage is used. tokenstorage.NoOp is rejected together with
	// RefreshTokenCredentials, because the rotated refresh token would be
	// lost.
	TokenStorage tokenstorage.TokenStorage

	// TokenStorageKey is the key of this client's token in TokenStorage. If
	// empty, a key is derived from BaseURL and the credentials' identity, so
	// different shops and principals never share a token.
	TokenStorageKey string

	// Locker, if set, serializes token refreshes and invalidations across
	// clients and processes. Useful with admin login or refresh tokens and a
	// shared TokenStorage.
	Locker tokenstorage.Locker

	// MaxResponseBytes limits the size of a response body. If zero,
	// DefaultMaxResponseBytes is used. math.MaxInt64 means no practical
	// limit.
	MaxResponseBytes int64
}

func (c Config) validate() (string, error) {
	if isNil(c.Credentials) {
		return "", errors.New("adminapi: Config.Credentials is required")
	}
	if c.BaseURL == "" {
		return "", errors.New("adminapi: Config.BaseURL is required")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return "", fmt.Errorf("adminapi: invalid Config.BaseURL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("adminapi: Config.BaseURL must use http or https, got %q", c.BaseURL)
	}
	if u.Host == "" {
		return "", fmt.Errorf("adminapi: Config.BaseURL has no host: %q", c.BaseURL)
	}
	// Request paths are appended to BaseURL, so a query or fragment would
	// swallow them ("https://shop/?a=b" + "/api/oauth/token"). The raw string
	// is checked because an empty fragment ("https://shop/#") leaves no trace
	// in url.URL. After a successful parse, '?' and '#' only start a query or
	// fragment; in a path they must be escaped.
	if strings.ContainsAny(c.BaseURL, "?#") {
		return "", fmt.Errorf("adminapi: Config.BaseURL must not contain a query or fragment: %q", c.BaseURL)
	}
	if c.MaxResponseBytes < 0 {
		return "", errors.New("adminapi: Config.MaxResponseBytes must not be negative")
	}
	if usesRefreshToken(c.Credentials) && tokenstorage.Discards(c.TokenStorage) {
		// Shopware revokes the refresh token on the first exchange; without
		// storage the new one is lost and every later request fails.
		return "", errors.New("adminapi: RefreshTokenCredentials need a TokenStorage that keeps tokens, not tokenstorage.NoOp (also not wrapped in Scoped)")
	}
	return strings.TrimRight(c.BaseURL, "/"), nil
}

// isNil reports whether credentials is nil, including a nil pointer stored
// in the interface (var c *IntegrationCredentials; Config{Credentials: c}).
// Such a value is not == nil, but calling its methods would panic.
func isNil(credentials Credentials) bool {
	if credentials == nil {
		return true
	}
	v := reflect.ValueOf(credentials)
	switch v.Kind() { //nolint:exhaustive // only these kinds can be nil
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}

func usesRefreshToken(credentials Credentials) bool {
	switch credentials.(type) {
	case RefreshTokenCredentials, *RefreshTokenCredentials:
		return true
	default:
		return false
	}
}
