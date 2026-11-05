package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopware/sdk-go/app"
	"github.com/shopware/sdk-go/clients/adminapi"
	adminstorage "github.com/shopware/sdk-go/clients/adminapi/tokenstorage"
	"github.com/shopware/sdk-go/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tokenShop is a fake shop that counts token requests and records the
// User-Agent of API requests.
type tokenShop struct {
	*httptest.Server

	tokenCalls atomic.Int32
	userAgent  atomic.Value
}

func newTokenShop(t *testing.T) *tokenShop {
	t.Helper()
	return newSlowTokenShop(t, 0)
}

// newSlowTokenShop answers token requests after delay, so concurrent clients
// overlap while a token is being fetched.
func newSlowTokenShop(t *testing.T, delay time.Duration) *tokenShop {
	t.Helper()
	s := &tokenShop{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/oauth/token" {
			n := s.tokenCalls.Add(1)
			time.Sleep(delay)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": fmt.Sprintf("token-%d", n),
				"expires_in":   600,
			})
			return
		}
		s.userAgent.Store(r.Header.Get("User-Agent"))
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(s.Close)
	return s
}

func newAppWithShop(t *testing.T, shopURL string, config app.Config) (*app.App, *store.Shop) {
	t.Helper()
	config.AppName = "TestApp"
	config.AppSecret = "test-secret-1234567890"
	config.ConfirmationURL = "https://test.com/confirm"
	config.ShopStore = store.NewInMemoryStore()

	sdk, err := app.New(config)
	require.NoError(t, err)

	shop := &store.Shop{
		ShopID:       "shop-1",
		ShopURL:      shopURL,
		ShopSecret:   "shop-secret",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		Active:       true,
	}
	require.NoError(t, config.ShopStore.UpdateShop(context.Background(), shop))
	return sdk, shop
}

func TestApp_ClientsShareTokenByDefault(t *testing.T) {
	shop := newTokenShop(t)
	sdk, _ := newAppWithShop(t, shop.URL, app.Config{})

	for range 3 {
		c, err := sdk.NewClient(context.Background(), "shop-1")
		require.NoError(t, err)
		_, err = c.Get(context.Background(), "/product")
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), shop.tokenCalls.Load(), "all clients of the shop share one token")
}

func TestApp_ConcurrentClientsFetchOneToken(t *testing.T) {
	shop := newSlowTokenShop(t, 50*time.Millisecond)
	sdk, shopRecord := newAppWithShop(t, shop.URL, app.Config{})

	const clients = 10
	var wg sync.WaitGroup
	errs := make(chan error, clients)
	for range clients {
		wg.Go(func() {
			c, err := sdk.NewClientForShop(shopRecord)
			if err == nil {
				_, err = c.Get(context.Background(), "/product")
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), shop.tokenCalls.Load(),
		"clients created separately still fetch one token per shop")
}

// countingLocker is a Locker that counts locks and serializes them like a
// distributed lock would.
type countingLocker struct {
	mu    sync.Mutex
	locks atomic.Int32
}

func (l *countingLocker) Lock(_ context.Context, _ string) (func(), error) {
	l.locks.Add(1)
	l.mu.Lock()
	return l.mu.Unlock, nil
}

func TestApp_UsesConfiguredLocker(t *testing.T) {
	shop := newSlowTokenShop(t, 20*time.Millisecond)
	locker := &countingLocker{}
	storage := adminstorage.NewInMemory()

	// Two Apps on one storage and one Locker act like two instances.
	first, shopRecord := newAppWithShop(t, shop.URL, app.Config{TokenStorage: storage, Locker: locker})
	second, _ := newAppWithShop(t, shop.URL, app.Config{TokenStorage: storage, Locker: locker})

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, sdk := range []*app.App{first, second} {
		wg.Go(func() {
			c, err := sdk.NewClientForShop(shopRecord)
			if err == nil {
				_, err = c.Get(context.Background(), "/product")
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), shop.tokenCalls.Load(), "instances sharing a Locker fetch one token")
	assert.Positive(t, locker.locks.Load(), "the configured Locker is used")
}

func TestApp_UsesConfiguredTokenStorage(t *testing.T) {
	shop := newTokenShop(t)
	storage := adminstorage.NewInMemory()
	sdk, shopRecord := newAppWithShop(t, shop.URL, app.Config{TokenStorage: storage})

	_, err := mustClient(t, sdk, shopRecord).Get(context.Background(), "/product")
	require.NoError(t, err)

	// Another App on the same storage (another instance) reuses the token.
	other, _ := newAppWithShop(t, shop.URL, app.Config{TokenStorage: storage})
	c2, err := other.NewClient(context.Background(), "shop-1")
	require.NoError(t, err)
	_, err = c2.Get(context.Background(), "/product")
	require.NoError(t, err)

	assert.Equal(t, int32(1), shop.tokenCalls.Load())
}

func TestApp_NewCredentialsUseNewToken(t *testing.T) {
	shop := newTokenShop(t)
	sdk, shopRecord := newAppWithShop(t, shop.URL, app.Config{})

	_, err := mustClient(t, sdk, shopRecord).Get(context.Background(), "/product")
	require.NoError(t, err)

	// The shop registers again and gets new credentials.
	changed := *shopRecord
	changed.ClientID = "new-client-id"
	changed.ClientSecret = "new-client-secret"
	_, err = mustClient(t, sdk, &changed).Get(context.Background(), "/product")
	require.NoError(t, err)

	assert.Equal(t, int32(2), shop.tokenCalls.Load(), "the old token is not used for new credentials")
}

func TestApp_ClearShopToken(t *testing.T) {
	shop := newTokenShop(t)
	sdk, shopRecord := newAppWithShop(t, shop.URL, app.Config{})
	ctx := context.Background()

	admin, err := sdk.NewClientForShop(shopRecord)
	require.NoError(t, err)
	require.NoError(t, admin.SetToken(ctx, adminstorage.Token{AccessToken: "seeded", Expiry: time.Now().Add(time.Hour)}))

	require.NoError(t, sdk.ClearShopToken(ctx, shopRecord))

	token, err := admin.AccessToken(ctx)
	require.NoError(t, err)
	assert.NotEqual(t, "seeded", token, "a new token is fetched")
	assert.Equal(t, int32(1), shop.tokenCalls.Load())

	require.Error(t, sdk.ClearShopToken(ctx, nil))
}

func TestApp_ClearShopTokenWaitsForRunningFetch(t *testing.T) {
	shop := newSlowTokenShop(t, 100*time.Millisecond)
	sdk, shopRecord := newAppWithShop(t, shop.URL, app.Config{})
	ctx := context.Background()

	admin, err := sdk.NewClientForShop(shopRecord)
	require.NoError(t, err)
	fetched := make(chan error, 1)
	go func() {
		_, fetchErr := admin.AccessToken(ctx)
		fetched <- fetchErr
	}()
	require.Eventually(t, func() bool { return shop.tokenCalls.Load() == 1 },
		time.Second, time.Millisecond, "the token request is running")

	require.NoError(t, sdk.ClearShopToken(ctx, shopRecord))
	require.NoError(t, <-fetched)

	next, err := sdk.NewClientForShop(shopRecord)
	require.NoError(t, err)
	token, err := next.AccessToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, "token-2", token, "the token of the running fetch is not stored again after the cleanup")
}

func TestApp_NewClientUsesConfig(t *testing.T) {
	shop := newTokenShop(t)
	sdk, _ := newAppWithShop(t, shop.URL, app.Config{UserAgent: "my-app/1.0"})

	admin, err := sdk.NewClient(context.Background(), "shop-1")
	require.NoError(t, err)
	_, err = admin.Get(context.Background(), "/product")
	require.NoError(t, err)
	assert.Equal(t, "my-app/1.0", shop.userAgent.Load())

	_, err = sdk.NewClient(context.Background(), "missing")
	require.Error(t, err)
}

func TestApp_HTTPClientIsUsed(t *testing.T) {
	shop := newTokenShop(t)
	var used atomic.Int32
	httpClient := &http.Client{Transport: countingTransport{n: &used}}
	sdk, _ := newAppWithShop(t, shop.URL, app.Config{HTTPClient: httpClient})

	c, err := sdk.NewClient(context.Background(), "shop-1")
	require.NoError(t, err)
	_, err = c.Get(context.Background(), "/product")
	require.NoError(t, err)

	assert.Equal(t, int32(2), used.Load(), "token request and API request")
}

type countingTransport struct {
	n *atomic.Int32
}

func (t countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.n.Add(1)
	return http.DefaultTransport.RoundTrip(r)
}

func TestApp_NewClientForShopRejectsInvalidShop(t *testing.T) {
	sdk, _ := newAppWithShop(t, "https://shop.example.com", app.Config{})

	_, err := sdk.NewClientForShop(&store.Shop{ShopID: "x", ShopURL: "not a url"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BaseURL")

	_, err = sdk.NewClientForShop(nil)
	require.Error(t, err)
}

// mustClient creates the Admin API client of shop.
func mustClient(t *testing.T, sdk *app.App, shop *store.Shop) *adminapi.Client {
	t.Helper()
	c, err := sdk.NewClientForShop(shop)
	require.NoError(t, err)
	return c
}
