package adminapi

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopware/app-sdk-go/clients/adminapi/internal/assert"
	"github.com/shopware/app-sdk-go/clients/adminapi/tokenstorage"
)

func TestGetSendsBearerAndAPIPrefix(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{})

	resp, err := c.Get(t.Context(), "product/abc")
	assert.NoError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	assert.Equal(t, string(resp.Body), `{"ok":true}`)

	calls := shop.apiCalls()
	assert.Len(t, calls, 1)
	assert.Equal(t, calls[0].URL.Path, "/api/product/abc")
	assert.Equal(t, calls[0].Header.Get("Authorization"), "Bearer access-1")
	assert.Equal(t, calls[0].Header.Get("Accept"), "application/json")
	assert.Equal(t, calls[0].Header.Get("User-Agent"), DefaultUserAgent)
	assert.Equal(t, calls[0].Header.Get("Content-Type"), "", "no body, no content type")
}

func TestVerbsUseTheirMethod(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{})
	ctx := t.Context()

	_, err := c.Post(ctx, "/a", map[string]int{"x": 1})
	assert.NoError(t, err)
	_, err = c.Put(ctx, "/a", map[string]int{"x": 1})
	assert.NoError(t, err)
	_, err = c.Patch(ctx, "/a", map[string]int{"x": 1})
	assert.NoError(t, err)
	_, err = c.Delete(ctx, "/a")
	assert.NoError(t, err)

	calls := shop.apiCalls()
	assert.Len(t, calls, 4)
	for i, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		assert.Equal(t, calls[i].Method, method)
	}
}

func TestPostSendsJSONBody(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{})

	_, err := c.Post(t.Context(), "/search/product", map[string]any{"limit": 1})
	assert.NoError(t, err)

	assert.Equal(t, shop.apiCalls()[0].Header.Get("Content-Type"), "application/json")
	assert.JSONEq(t, shop.apiBodies[0], `{"limit":1}`)
}

func TestRequestRejectsBodyThatCannotBeEncoded(t *testing.T) {
	c := newTestClient(t, newFakeShop(t), Config{})

	_, err := c.Post(t.Context(), "/a", make(chan int))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "encode request body")
}

func TestHeaderOrder(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{
		UserAgent: "my-app/1.0",
		Headers:   map[string]string{"X-Proxy-Auth": "p", "User-Agent": "from-config"},
	})

	_, err := c.Post(t.Context(), "/a", map[string]int{"x": 1},
		WithHeader("Accept", "text/csv"),
		WithHeader("Content-Type", "application/vnd.api+json"),
		WithHeader("Authorization", "Bearer stolen"),
		WithHeader("sw-language-id", "lang"),
	)
	assert.NoError(t, err)

	h := shop.apiCalls()[0].Header
	assert.Equal(t, h.Get("User-Agent"), "from-config", "Config.Headers override UserAgent")
	assert.Equal(t, h.Get("X-Proxy-Auth"), "p")
	assert.Equal(t, h.Get("Accept"), "text/csv", "request options override defaults")
	assert.Equal(t, h.Get("Content-Type"), "application/vnd.api+json")
	assert.Equal(t, h.Get("Sw-Language-Id"), "lang")
	assert.Equal(t, h.Get("Authorization"), "Bearer access-1", "Authorization cannot be overridden")

	token := shop.tokenCalls()
	assert.Len(t, token, 1)
}

func TestConfigHeadersAreSentWithTokenRequests(t *testing.T) {
	var got string
	shop := newFakeShop(t)
	shop.setToken(func(w http.ResponseWriter, n int, fields map[string]string) {
		issueToken(w, n, fields)
	})
	shop.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/oauth/token" {
			got = r.Header.Get("X-Proxy-Auth")
		}
		shop.serve(w, r)
	})
	c := newTestClient(t, shop, Config{Headers: map[string]string{"X-Proxy-Auth": "p"}})

	assert.NoError(t, c.Authenticate(t.Context()))
	assert.Equal(t, got, "p")
}

func TestWithQueryMergesWithPathQuery(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{})

	_, err := c.Get(t.Context(), "/a?x=1", WithQuery("y", "2"), WithQuery("y", "3"))
	assert.NoError(t, err)

	q := shop.apiCalls()[0].URL.Query()
	assert.Equal(t, q.Get("x"), "1")
	assert.Equal(t, q["y"], []string{"2", "3"})
}

func TestRequestRawSendsBodyAsIs(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{})

	_, err := c.RequestRaw(t.Context(), http.MethodPost, "/_action/media/1/upload",
		func() (io.Reader, error) { return strings.NewReader("binary"), nil },
		WithHeader("Content-Type", "image/png"))
	assert.NoError(t, err)

	assert.Equal(t, shop.apiBodies[0], "binary")
	assert.Equal(t, shop.apiCalls()[0].Header.Get("Content-Type"), "image/png")
}

func TestRequestRawBodyFactoryError(t *testing.T) {
	c := newTestClient(t, newFakeShop(t), Config{})

	_, err := c.RequestRaw(t.Context(), http.MethodPost, "/a",
		func() (io.Reader, error) { return nil, errBoom })
	assert.True(t, errors.Is(err, errBoom))
}

func TestRetriesOnceOn401AndRebuildsBody(t *testing.T) {
	var calls atomic.Int32
	shop := newFakeShop(t)
	shop.setAPI(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	})
	c := newTestClient(t, shop, Config{})

	var built atomic.Int32
	_, err := c.RequestRaw(t.Context(), http.MethodPost, "/a", func() (io.Reader, error) {
		built.Add(1)
		return strings.NewReader("payload"), nil
	})
	assert.NoError(t, err)

	assert.Equal(t, built.Load(), int32(2), "the body is rebuilt for the retry")
	assert.Equal(t, shop.apiBodies, []string{"payload", "payload"})
	assert.Len(t, shop.tokenCalls(), 2, "a new token is fetched for the retry")
	assert.Equal(t, shop.apiCalls()[1].Header.Get("Authorization"), "Bearer access-2")
}

func TestRetriesOn401OnlyOnce(t *testing.T) {
	shop := newFakeShop(t)
	shop.setAPI(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"errors": []map[string]string{{"detail": "no"}}})
	})
	c := newTestClient(t, shop, Config{})

	_, err := c.Get(t.Context(), "/a")
	assert.True(t, IsUnauthorized(err))
	assert.Len(t, shop.apiCalls(), 2)
}

func TestRedirectIsNotFollowed(t *testing.T) {
	var hit atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit.Store(true) }))
	t.Cleanup(other.Close)

	shop := newFakeShop(t)
	shop.setAPI(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusFound)
	})
	c := newTestClient(t, shop, Config{})

	_, err := c.Get(t.Context(), "/a")
	assert.True(t, errors.Is(err, ErrUnexpectedRedirect))
	assert.Contains(t, err.Error(), other.URL+"/steal")
	assert.False(t, hit.Load(), "the redirect target must not be called")
}

func TestResponseTooLarge(t *testing.T) {
	shop := newFakeShop(t)
	shop.setAPI(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 101))
	})
	c := newTestClient(t, shop, Config{MaxResponseBytes: 100})

	_, err := c.Get(t.Context(), "/a")
	assert.True(t, errors.Is(err, ErrResponseTooLarge))
}

func TestResponseAtLimitIsAccepted(t *testing.T) {
	shop := newFakeShop(t)
	shop.setAPI(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 100))
	})
	c := newTestClient(t, shop, Config{MaxResponseBytes: 100})

	resp, err := c.Get(t.Context(), "/a")
	assert.NoError(t, err)
	assert.Len(t, resp.Body, 100)
}

func TestRequestStopsWhenContextIsCancelled(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{})
	assert.NoError(t, c.Authenticate(t.Context()))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := c.Get(ctx, "/a")
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestTransportErrorIsWrapped(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{})
	assert.NoError(t, c.Authenticate(t.Context()))
	shop.srv.Close()

	_, err := c.Get(t.Context(), "/a")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "adminapi: GET /a")
}

func TestAPIErrors(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantMsg    string
		wantErrors int
		check      func(error) bool
	}{
		{
			name:   "shopware envelope with two errors",
			status: http.StatusBadRequest,
			body: `{"errors":[{"status":"400","code":"FRAMEWORK__WRITE_CONSTRAINT_VIOLATION",` +
				`"title":"Constraint violation","detail":"This value should not be blank.",` +
				`"source":{"pointer":"/0/name"},"meta":{"parameters":{}}},{"detail":"second"}]}`,
			wantMsg:    "adminapi: status 400: This value should not be blank. (and 1 more)",
			wantErrors: 2,
		},
		{
			name:       "not found",
			status:     http.StatusNotFound,
			body:       `{"errors":[{"status":"404","title":"Not Found"}]}`,
			wantMsg:    "adminapi: status 404: Not Found",
			wantErrors: 1,
			check:      IsNotFound,
		},
		{
			name:       "conflict with code only",
			status:     http.StatusConflict,
			body:       `{"errors":[{"code":"CONFLICT"}]}`,
			wantMsg:    "adminapi: status 409: CONFLICT",
			wantErrors: 1,
			check:      IsConflict,
		},
		{
			name:       "oauth error",
			status:     http.StatusUnauthorized,
			body:       `{"error":"invalid_client","error_description":"Client authentication failed"}`,
			wantMsg:    "adminapi: status 401: Client authentication failed",
			wantErrors: 1,
		},
		{
			name:       "oauth error with message",
			status:     http.StatusBadRequest,
			body:       `{"error":"invalid_grant","message":"The refresh token is invalid."}`,
			wantMsg:    "adminapi: status 400: The refresh token is invalid.",
			wantErrors: 1,
		},
		{
			name:    "plain text body",
			status:  http.StatusBadGateway,
			body:    "upstream down",
			wantMsg: "adminapi: status 502: upstream down",
		},
		{
			name:    "long body is truncated",
			status:  http.StatusInternalServerError,
			body:    strings.Repeat("a", 300),
			wantMsg: "adminapi: status 500: " + strings.Repeat("a", 200) + "...",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiErr := newAPIError(tt.status, []byte(tt.body))
			assert.Equal(t, apiErr.Error(), tt.wantMsg)
			assert.Len(t, apiErr.Errors, tt.wantErrors)
			if tt.check != nil {
				assert.True(t, tt.check(apiErr))
			}
		})
	}
}

func TestAPIErrorKeepsDetails(t *testing.T) {
	apiErr := newAPIError(http.StatusBadRequest, []byte(
		`{"errors":[{"status":"400","code":"C","title":"T","detail":"D","source":{"pointer":"/0/name"},"meta":{"a":1}}]}`))

	assert.Equal(t, apiErr.Errors[0].Source.Pointer, "/0/name")
	assert.Equal(t, apiErr.Errors[0].Code, "C")
	assert.JSONEq(t, string(apiErr.Errors[0].Meta), `{"a":1}`)
}

func TestAPIErrorFromRequest(t *testing.T) {
	shop := newFakeShop(t)
	shop.setAPI(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{"errors": []map[string]string{{"detail": "gone"}}})
	})
	c := newTestClient(t, shop, Config{})

	_, err := c.Get(t.Context(), "/product/x")
	var apiErr *APIError
	assert.ErrorAs(t, err, &apiErr)
	assert.Equal(t, apiErr.StatusCode, http.StatusNotFound)
	assert.True(t, IsNotFound(err))
	assert.False(t, IsConflict(err))
	assert.False(t, IsNotFound(errBoom))
}

// readOnlyStorage holds a token but fails to change it.
type readOnlyStorage struct {
	token tokenstorage.Token
}

func (s *readOnlyStorage) Get(context.Context, string) (tokenstorage.Token, error) {
	return s.token, nil
}

func (s *readOnlyStorage) Set(context.Context, string, tokenstorage.Token) error { return errBoom }

func (s *readOnlyStorage) Delete(context.Context, string) error { return errBoom }

func TestFailedInvalidationStopsTheRetry(t *testing.T) {
	shop := newFakeShop(t)
	shop.setAPI(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"errors": []map[string]string{{"status": "401", "detail": "token revoked"}},
		})
	})
	storage := &readOnlyStorage{token: tokenstorage.Token{AccessToken: "cached", Expiry: time.Now().Add(time.Hour)}}
	c := newTestClient(t, shop, Config{TokenStorage: storage})

	_, err := c.Get(t.Context(), "/a")

	assert.Error(t, err)
	assert.Equal(t, err.Error(),
		"adminapi: status 401: token revoked (the token could not be invalidated: adminapi: delete token: boom)")
	assert.True(t, IsUnauthorized(err), "the 401 is still reported")
	assert.True(t, errors.Is(err, errBoom), "the storage error is reported")
	assert.Len(t, shop.apiCalls(), 1, "the rejected token is not sent again")
	assert.Len(t, shop.tokenCalls(), 0)
}

func TestFailedTokenReadStopsTheRetry(t *testing.T) {
	shop := newFakeShop(t)
	shop.setAPI(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	storage := &flakyStorage{TokenStorage: tokenstorage.NewInMemory()}
	c := newTestClient(t, shop, Config{TokenStorage: storage})
	assert.NoError(t, c.Authenticate(t.Context()))
	storage.failGet.Store(true) // the store goes down after the first request started

	_, err := c.Get(t.Context(), "/a")

	assert.Error(t, err)
	assert.True(t, IsUnauthorized(err))
	assert.True(t, errors.Is(err, errBoom))
	assert.Len(t, shop.apiCalls(), 1)
}

// flakyStorage fails reads once failGet is set, but only after the token was
// read once for the request.
type flakyStorage struct {
	tokenstorage.TokenStorage

	failGet atomic.Bool
	reads   atomic.Int32
}

func (s *flakyStorage) Get(ctx context.Context, key string) (tokenstorage.Token, error) {
	if s.failGet.Load() && s.reads.Add(1) > 1 {
		return tokenstorage.Token{}, errBoom
	}
	return s.TokenStorage.Get(ctx, key)
}

func TestHeaderOptionsAreCaseInsensitive(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{})

	// Map order is random; repeat so a wrong winner would show up.
	const runs = 50
	for range runs {
		_, err := c.Post(t.Context(), "/upload", map[string]string{"a": "b"},
			WithHeader("content-type", "text/plain"),
			WithHeader("x-trace", "first"), WithHeader("X-Trace", "last"))
		assert.NoError(t, err)
	}

	calls := shop.apiCalls()
	assert.Len(t, calls, runs)
	for _, call := range calls {
		assert.Equal(t, call.Header.Values("Content-Type"), []string{"text/plain"}, "the caller's option wins")
		assert.Equal(t, call.Header.Values("X-Trace"), []string{"last"}, "the last option wins")
	}
}

func TestConfigHeadersAreCopiedAndCanonical(t *testing.T) {
	shop := newFakeShop(t)
	headers := map[string]string{"x-tenant": "a", "x-dup": "lower", "X-Dup": "upper"}
	c := newTestClient(t, shop, Config{Headers: headers})
	headers["x-tenant"] = "changed later"

	const runs = 20
	for range runs {
		_, err := c.Get(t.Context(), "/a")
		assert.NoError(t, err)
	}

	for _, call := range shop.apiCalls() {
		assert.Equal(t, call.Header.Get("X-Tenant"), "a", "the client keeps its own copy")
		assert.Equal(t, call.Header.Values("X-Dup"), []string{"lower"}, "names differing in case resolve the same way every time")
	}
}

func TestMaxResponseBytesLimits(t *testing.T) {
	const body = `{"ok":true}` // 11 bytes
	tests := []struct {
		name    string
		limit   int64
		wantErr bool
	}{
		{"no practical limit", math.MaxInt64, false},
		{"exactly the body size", int64(len(body)), false},
		{"one byte too small", int64(len(body)) - 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shop := newFakeShop(t)
			shop.setAPI(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
			c := newTestClient(t, shop, Config{MaxResponseBytes: tt.limit})
			// The limit applies to token responses too; a stored token keeps
			// this test about the API response.
			assert.NoError(t, c.SetToken(t.Context(), tokenstorage.Token{AccessToken: "t", Expiry: time.Now().Add(time.Hour)}))

			resp, err := c.Get(t.Context(), "/a")

			if tt.wantErr {
				assert.True(t, errors.Is(err, ErrResponseTooLarge))
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, string(resp.Body), body)
		})
	}
}

func TestRetryAfterLarge401DoesNotWaitForItsConnection(t *testing.T) {
	shop := newFakeShop(t)
	var calls atomic.Int32
	shop.setAPI(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			// Larger than maxDrainBytes, so the client does not read it to
			// the end and the connection stays busy until the body is closed.
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, strings.Repeat("x", 2*maxDrainBytes))
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	c := newTestClient(t, shop, Config{
		HTTPClient: &http.Client{Transport: &http.Transport{MaxConnsPerHost: 1}},
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	resp, err := c.Get(ctx, "/a")

	assert.NoError(t, err, "the retry needs the only connection; the 401 body must be closed first")
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	assert.Len(t, shop.tokenCalls(), 2, "a new token was fetched for the retry")
}
