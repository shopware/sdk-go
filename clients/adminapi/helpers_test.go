package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/shopware/sdk-go/clients/adminapi/tokenstorage"
)

// fakeShop is a minimal Shopware Admin API: /api/oauth/token issues tokens,
// every other path is handled by api.
type fakeShop struct {
	t   *testing.T
	srv *httptest.Server

	mu            sync.Mutex
	tokenRequests []map[string]string
	apiRequests   []*http.Request
	apiBodies     []string

	// token answers token requests. The default issues "access-N" and, for
	// the password and refresh_token grants, "refresh-N".
	token func(w http.ResponseWriter, n int, fields map[string]string)

	// api answers every other request. The default returns 200 {"ok":true}.
	api http.HandlerFunc
}

func newFakeShop(t *testing.T) *fakeShop {
	t.Helper()
	s := &fakeShop{t: t}
	s.token = issueToken
	s.api = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *fakeShop) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	if r.URL.Path == "/api/oauth/token" {
		var fields map[string]string
		if err := json.Unmarshal(body, &fields); err != nil {
			s.t.Errorf("token request body is not JSON: %v", err)
		}
		s.mu.Lock()
		s.tokenRequests = append(s.tokenRequests, fields)
		n := len(s.tokenRequests)
		handler := s.token
		s.mu.Unlock()
		handler(w, n, fields)
		return
	}

	s.mu.Lock()
	s.apiRequests = append(s.apiRequests, r.Clone(context.Background()))
	s.apiBodies = append(s.apiBodies, string(body))
	handler := s.api
	s.mu.Unlock()
	handler(w, r)
}

func issueToken(w http.ResponseWriter, n int, fields map[string]string) {
	resp := map[string]any{
		"token_type":   "Bearer",
		"access_token": fmt.Sprintf("access-%d", n),
		"expires_in":   600,
	}
	if fields["grant_type"] != "client_credentials" {
		resp["refresh_token"] = fmt.Sprintf("refresh-%d", n)
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *fakeShop) tokenCalls() []map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]string(nil), s.tokenRequests...)
}

func (s *fakeShop) apiCalls() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.apiRequests...)
}

func (s *fakeShop) setToken(h func(w http.ResponseWriter, n int, fields map[string]string)) {
	s.mu.Lock()
	s.token = h
	s.mu.Unlock()
}

func (s *fakeShop) setAPI(h http.HandlerFunc) {
	s.mu.Lock()
	s.api = h
	s.mu.Unlock()
}

var integration = IntegrationCredentials{ClientID: "id", ClientSecret: "secret"}

// newTestClient builds a client for shop. Config fields other than BaseURL
// may be preset in config.
func newTestClient(t *testing.T, shop *fakeShop, config Config) *Client {
	t.Helper()
	config.BaseURL = shop.srv.URL
	if config.Credentials == nil {
		config.Credentials = integration
	}
	c, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// clock is a fake time source for token expiry tests.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock {
	return &clock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// failingStorage returns errors from the operations that have one set.
type failingStorage struct {
	tokenstorage.TokenStorage

	getErr, setErr error
}

func (s failingStorage) Get(ctx context.Context, key string) (tokenstorage.Token, error) {
	if s.getErr != nil {
		return tokenstorage.Token{}, s.getErr
	}
	return s.TokenStorage.Get(ctx, key)
}

func (s failingStorage) Set(ctx context.Context, key string, token tokenstorage.Token) error {
	if s.setErr != nil {
		return s.setErr
	}
	return s.TokenStorage.Set(ctx, key, token)
}

var errBoom = errors.New("boom")
