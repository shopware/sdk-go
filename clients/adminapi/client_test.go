package adminapi

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/shopware/app-sdk-go/clients/adminapi/internal/assert"
	"github.com/shopware/app-sdk-go/clients/adminapi/tokenstorage"
)

func TestNewClientValidatesConfig(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   string
	}{
		{"missing credentials", Config{BaseURL: "https://shop.example.com"}, "Credentials is required"},
		{"missing base URL", Config{Credentials: integration}, "BaseURL is required"},
		{"unsupported scheme", Config{BaseURL: "ftp://shop.example.com", Credentials: integration}, "http or https"},
		{"no host", Config{BaseURL: "https://", Credentials: integration}, "no host"},
		{"invalid URL", Config{BaseURL: "https://shop example.com/%zz", Credentials: integration}, "invalid Config.BaseURL"},
		{"query", Config{BaseURL: "https://shop.example.com/?tenant=x", Credentials: integration}, "query or fragment"},
		{"empty query", Config{BaseURL: "https://shop.example.com/?", Credentials: integration}, "query or fragment"},
		{"fragment", Config{BaseURL: "https://shop.example.com/#admin", Credentials: integration}, "query or fragment"},
		{"empty fragment", Config{BaseURL: "https://shop.example.com#", Credentials: integration}, "query or fragment"},
		{
			"negative response limit",
			Config{BaseURL: "https://shop.example.com", Credentials: integration, MaxResponseBytes: -1},
			"must not be negative",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClient(tt.config)
			if err == nil {
				t.Fatalf("NewClient(%q) returned no error, want %q", tt.config.BaseURL, tt.want)
			}
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestNewClientRejectsNilCredentialPointers(t *testing.T) {
	var (
		integrationCreds *IntegrationCredentials
		passwordCreds    *PasswordCredentials
		refreshCreds     *RefreshTokenCredentials
	)
	for name, creds := range map[string]Credentials{
		"integration": integrationCreds,
		"password":    passwordCreds,
		"refresh":     refreshCreds,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewClient(Config{BaseURL: "https://shop.example.com", Credentials: creds})
			if err == nil {
				t.Fatal("NewClient accepted a nil credentials pointer")
			}
			assert.Contains(t, err.Error(), "Credentials is required")
		})
	}

	// A non-nil pointer is fine.
	_, err := NewClient(Config{BaseURL: "https://shop.example.com", Credentials: &integration})
	assert.NoError(t, err)
}

func TestNewClientRejectsRefreshTokenWithoutStorage(t *testing.T) {
	refresh := RefreshTokenCredentials{ClientID: "administration", RefreshToken: "r"}
	noop := tokenstorage.NewNoOp()
	for name, config := range map[string]Config{
		"value types":         {Credentials: refresh, TokenStorage: noop},
		"pointer credentials": {Credentials: &refresh, TokenStorage: noop},
		"pointer storage":     {Credentials: refresh, TokenStorage: &noop},
		"scoped storage":      {Credentials: refresh, TokenStorage: tokenstorage.NewScoped(noop, "app")},
		"nested scoped":       {Credentials: refresh, TokenStorage: tokenstorage.NewScoped(tokenstorage.NewScoped(noop, "app"), "env")},
	} {
		t.Run(name, func(t *testing.T) {
			config.BaseURL = "https://shop.example.com"
			_, err := NewClient(config)
			if err == nil {
				t.Fatal("NewClient accepted RefreshTokenCredentials with tokenstorage.NoOp")
			}
			assert.Contains(t, err.Error(), "RefreshTokenCredentials need a TokenStorage")
		})
	}

	for name, config := range map[string]Config{
		"refresh token with default storage": {Credentials: refresh},
		"refresh token with memory":          {Credentials: refresh, TokenStorage: tokenstorage.NewInMemory()},
		"refresh token with scoped memory":   {Credentials: refresh, TokenStorage: tokenstorage.NewScoped(tokenstorage.NewInMemory(), "app")},
		"password without storage":           {Credentials: PasswordCredentials{Username: "u", Password: "p"}, TokenStorage: noop},
		"integration without storage":        {Credentials: integration, TokenStorage: noop},
	} {
		t.Run(name, func(t *testing.T) {
			config.BaseURL = "https://shop.example.com"
			_, err := NewClient(config)
			assert.NoError(t, err)
		})
	}
}

func TestNoOpFetchesATokenForEveryRequest(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{
		Credentials:  PasswordCredentials{Username: "admin", Password: "shopware"},
		TokenStorage: tokenstorage.NewNoOp(),
	})

	for range 3 {
		_, err := c.Get(t.Context(), "/a")
		assert.NoError(t, err)
	}

	// As documented on tokenstorage.NoOp: one login per request, each with
	// the password.
	calls := shop.tokenCalls()
	assert.Len(t, calls, 3)
	for _, call := range calls {
		assert.Equal(t, call["grant_type"], "password")
	}
}

func TestNewClientAcceptsPathAndEscapedCharacters(t *testing.T) {
	// A shop below a path, with escaped '?' and '#' in it, is a valid base URL.
	for _, base := range []string{"https://shop.example.com/shop", "https://shop.example.com/a%3Fb%23c/"} {
		_, err := NewClient(Config{BaseURL: base, Credentials: integration})
		assert.NoError(t, err, base)
	}
}

func TestNewClientDefaults(t *testing.T) {
	c, err := NewClient(Config{BaseURL: "https://shop.example.com/", Credentials: integration})
	assert.NoError(t, err)

	assert.Equal(t, c.baseURL, "https://shop.example.com")
	assert.Equal(t, c.userAgent, DefaultUserAgent)
	assert.Equal(t, c.maxBody, int64(DefaultMaxResponseBytes))
	assert.Equal(t, c.httpClient.Timeout, DefaultTimeout)
	_, isMemory := c.storage.(*tokenstorage.InMemory)
	assert.True(t, isMemory, "default storage is in-memory")
	assert.True(t, strings.HasPrefix(c.tokenKey, "v1:"), "default key is versioned")
	assert.True(t, strings.HasPrefix(DefaultUserAgent, "shopware-app-sdk-go-adminapi/"))
}

func TestDefaultTokenKeyDependsOnShopAndIdentity(t *testing.T) {
	key := defaultTokenKey("https://a.example.com", "integration:id")

	assert.Equal(t, key, defaultTokenKey("https://a.example.com", "integration:id"))
	assert.NotEqual(t, key, defaultTokenKey("https://b.example.com", "integration:id"))
	assert.NotEqual(t, key, defaultTokenKey("https://a.example.com", "integration:other"))
	assert.NotContains(t, key, "integration", "the key must not reveal the identity")
}

func TestTokenStorageKeyOverridesDefault(t *testing.T) {
	storage := tokenstorage.NewInMemory()
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{TokenStorage: storage, TokenStorageKey: "shop-1"})

	assert.NoError(t, c.Authenticate(t.Context()))

	token, err := storage.Get(t.Context(), "shop-1")
	assert.NoError(t, err)
	assert.Equal(t, token.AccessToken, "access-1")
}

func TestNewClientCopiesHTTPClientAndDisablesRedirects(t *testing.T) {
	own := &http.Client{}
	c, err := NewClient(Config{BaseURL: "https://shop.example.com", Credentials: integration, HTTPClient: own})
	assert.NoError(t, err)

	assert.True(t, c.httpClient != own, "the client works on a copy")
	assert.True(t, own.CheckRedirect == nil, "the caller's client is not changed")
	assert.NotNil(t, c.httpClient.CheckRedirect)
	assert.True(t, errors.Is(c.httpClient.CheckRedirect(nil, nil), http.ErrUseLastResponse))
}

func TestNewClientKeepsCustomRedirectPolicy(t *testing.T) {
	policyErr := errors.New("custom policy")
	own := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return policyErr }}
	c, err := NewClient(Config{BaseURL: "https://shop.example.com", Credentials: integration, HTTPClient: own})
	assert.NoError(t, err)

	assert.True(t, errors.Is(c.httpClient.CheckRedirect(nil, nil), policyErr))
}

func TestPathEscapesSegments(t *testing.T) {
	assert.Equal(t, Path("product", "abc"), "/product/abc")
	assert.Equal(t, Path("_action", "extension", "../../x?y#z"), "/_action/extension/..%2F..%2Fx%3Fy%23z")
	assert.Equal(t, Path("product", ".."), "/product/%2E%2E", "a dot segment cannot leave the entity")
	assert.Equal(t, Path("product", "."), "/product/%2E")
	assert.Equal(t, Path("product", "..."), "/product/...", "only exact dot segments are special")
	assert.Equal(t, Path("product", "a.b"), "/product/a.b")
}

func TestPathDotSegmentsReachTheServerEncoded(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{})

	_, err := c.Get(t.Context(), Path("product", ".."))
	assert.NoError(t, err)

	calls := shop.apiCalls()
	assert.Len(t, calls, 1)
	assert.Equal(t, calls[0].URL.EscapedPath(), "/api/product/%2E%2E", "the server sees no dot segment")
	assert.Equal(t, Path(), "/")
}

func TestResponseJSON(t *testing.T) {
	resp := &Response{Body: []byte(`{"total":3}`)}
	var v struct {
		Total int `json:"total"`
	}
	assert.NoError(t, resp.JSON(&v))
	assert.Equal(t, v.Total, 3)
}
