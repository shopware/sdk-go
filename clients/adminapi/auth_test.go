package adminapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopware/sdk-go/clients/adminapi/internal/assert"
	"github.com/shopware/sdk-go/clients/adminapi/tokenstorage"
)

func TestTokenIsFetchedOnceAndReused(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{})

	for range 3 {
		_, err := c.Get(t.Context(), "/a")
		assert.NoError(t, err)
	}

	calls := shop.tokenCalls()
	assert.Len(t, calls, 1)
	assert.Equal(t, calls[0], map[string]string{
		"grant_type": "client_credentials", "client_id": "id", "client_secret": "secret",
	})
}

func TestTokenIsRefreshedShortlyBeforeExpiry(t *testing.T) {
	clk := newClock()
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{})
	c.now = clk.Now

	assert.NoError(t, c.Authenticate(t.Context()))
	clk.Advance(600*time.Second - tokenExpiryMargin - time.Second)
	assert.NoError(t, c.Authenticate(t.Context()))
	assert.Len(t, shop.tokenCalls(), 1, "still valid outside the margin")

	clk.Advance(2 * time.Second)
	assert.NoError(t, c.Authenticate(t.Context()))
	assert.Len(t, shop.tokenCalls(), 2, "refreshed inside the margin")
}

func TestMissingExpiresInUsesDefaultLifetime(t *testing.T) {
	clk := newClock()
	shop := newFakeShop(t)
	shop.setToken(func(w http.ResponseWriter, _ int, _ map[string]string) {
		writeJSON(w, http.StatusOK, map[string]any{"access_token": "a"})
	})
	storage := tokenstorage.NewInMemory()
	c := newTestClient(t, shop, Config{TokenStorage: storage, TokenStorageKey: "k"})
	c.now = clk.Now

	assert.NoError(t, c.Authenticate(t.Context()))

	token, err := storage.Get(t.Context(), "k")
	assert.NoError(t, err)
	assert.Equal(t, token.Expiry, clk.Now().Add(defaultTokenLifetime))
}

func TestFreshTokenIsUsedEvenIfShorterThanMargin(t *testing.T) {
	shop := newFakeShop(t)
	shop.setToken(func(w http.ResponseWriter, n int, _ map[string]string) {
		writeJSON(w, http.StatusOK, map[string]any{"access_token": "short", "expires_in": 10})
		_ = n
	})
	c := newTestClient(t, shop, Config{})

	token, err := c.AccessToken(t.Context())
	assert.NoError(t, err)
	assert.Equal(t, token, "short")
}

func TestNoOpStorageFetchesTokenForEveryRequest(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{TokenStorage: tokenstorage.NewNoOp()})

	for range 2 {
		_, err := c.Get(t.Context(), "/a")
		assert.NoError(t, err)
	}
	assert.Len(t, shop.tokenCalls(), 2)
}

func TestConcurrentRequestsFetchTokenOnce(t *testing.T) {
	const workers = 50
	release := make(chan struct{})
	shop := newFakeShop(t)
	shop.setToken(func(w http.ResponseWriter, n int, fields map[string]string) {
		<-release
		issueToken(w, n, fields)
	})
	c := newTestClient(t, shop, Config{})

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Go(func() {
			_, err := c.Get(t.Context(), "/a")
			errs <- err
		})
	}
	// Give the workers time to queue up behind the first token fetch.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errs)

	for err := range errs {
		assert.NoError(t, err)
	}
	assert.Len(t, shop.tokenCalls(), 1)
	assert.Len(t, shop.apiCalls(), workers)
}

func TestCancelledCallerDoesNotFailOthers(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	shop := newFakeShop(t)
	shop.setToken(func(w http.ResponseWriter, n int, fields map[string]string) {
		once.Do(func() { close(started) })
		<-release
		issueToken(w, n, fields)
	})
	c := newTestClient(t, shop, Config{})

	ctx, cancel := context.WithCancel(t.Context())
	first := make(chan error, 1)
	go func() {
		_, err := c.AccessToken(ctx)
		first <- err
	}()
	<-started

	second := make(chan error, 1)
	go func() {
		_, err := c.AccessToken(t.Context())
		second <- err
	}()

	cancel()
	assert.True(t, errors.Is(<-first, context.Canceled), "the cancelled caller stops waiting")

	close(release)
	assert.NoError(t, <-second, "the other caller still gets the token")
	assert.Len(t, shop.tokenCalls(), 1)
}

func TestStorageErrorsArePassedOn(t *testing.T) {
	t.Run("get", func(t *testing.T) {
		shop := newFakeShop(t)
		c := newTestClient(t, shop, Config{
			TokenStorage: failingStorage{TokenStorage: tokenstorage.NewInMemory(), getErr: errBoom},
		})

		_, err := c.Get(t.Context(), "/a")
		assert.True(t, errors.Is(err, errBoom))
		assert.Contains(t, err.Error(), "read token")
		assert.Len(t, shop.apiCalls(), 0)
	})

	t.Run("set", func(t *testing.T) {
		shop := newFakeShop(t)
		c := newTestClient(t, shop, Config{
			TokenStorage: failingStorage{TokenStorage: tokenstorage.NewInMemory(), setErr: errBoom},
		})

		_, err := c.Get(t.Context(), "/a")
		assert.True(t, errors.Is(err, errBoom))
		assert.Contains(t, err.Error(), "store token")
		assert.Len(t, shop.apiCalls(), 0, "no request with a token that could not be stored")
	})
}

// fakeLocker records locks; onLock runs while the lock is held.
type fakeLocker struct {
	mu     sync.Mutex
	locked []string
	err    error
	onLock func()
}

func (l *fakeLocker) Lock(_ context.Context, key string) (func(), error) {
	if l.err != nil {
		return nil, l.err
	}
	l.mu.Lock()
	l.locked = append(l.locked, key)
	l.mu.Unlock()
	if l.onLock != nil {
		l.onLock()
	}
	return func() {}, nil
}

func TestLockerIsUsedAndStorageIsReadAgain(t *testing.T) {
	storage := tokenstorage.NewInMemory()
	locker := &fakeLocker{}
	// Another process stores a fresh token while this one waits for the lock.
	locker.onLock = func() {
		_ = storage.Set(context.Background(), "k", tokenstorage.Token{
			AccessToken: "from-other-process", Expiry: time.Now().Add(time.Hour),
		})
	}
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{TokenStorage: storage, TokenStorageKey: "k", Locker: locker})

	token, err := c.AccessToken(t.Context())
	assert.NoError(t, err)
	assert.Equal(t, token, "from-other-process")
	assert.Equal(t, locker.locked, []string{"k"})
	assert.Len(t, shop.tokenCalls(), 0)
}

type nilUnlockLocker struct{}

// Lock returns no unlock function, which a careless implementation might do.
func (nilUnlockLocker) Lock(context.Context, string) (func(), error) {
	return nil, nil //nolint:nilnil // simulates a Locker without an unlock function
}

func TestLockerWithoutUnlockFunc(t *testing.T) {
	c := newTestClient(t, newFakeShop(t), Config{Locker: nilUnlockLocker{}})

	assert.NoError(t, c.Authenticate(t.Context()))
}

func TestLockerErrorIsPassedOn(t *testing.T) {
	c := newTestClient(t, newFakeShop(t), Config{Locker: &fakeLocker{err: errBoom}})

	err := c.Authenticate(t.Context())
	assert.True(t, errors.Is(err, errBoom))
	assert.Contains(t, err.Error(), "lock token")
}

func Test401DropsAccessTokenButKeepsRefreshToken(t *testing.T) {
	var calls atomic.Int32
	shop := newFakeShop(t)
	shop.setAPI(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	})
	storage := tokenstorage.NewInMemory()
	c := newTestClient(t, shop, Config{
		Credentials:     PasswordCredentials{Username: "admin", Password: "shopware"},
		TokenStorage:    storage,
		TokenStorageKey: "k",
	})

	_, err := c.Get(t.Context(), "/a")
	assert.NoError(t, err)

	tokens := shop.tokenCalls()
	assert.Len(t, tokens, 2)
	assert.Equal(t, tokens[0]["grant_type"], "password")
	assert.Equal(t, tokens[1]["grant_type"], "refresh_token", "the refresh token is used after the 401")
	assert.Equal(t, tokens[1]["refresh_token"], "refresh-1")

	stored, err := storage.Get(t.Context(), "k")
	assert.NoError(t, err)
	assert.Equal(t, stored, tokenstorage.Token{AccessToken: "access-2", RefreshToken: "refresh-2", Expiry: stored.Expiry})
}

func TestInvalidateUsedTokenKeepsReplacedToken(t *testing.T) {
	storage := tokenstorage.NewInMemory()
	c := newTestClient(t, newFakeShop(t), Config{TokenStorage: storage, TokenStorageKey: "k"})
	newer := tokenstorage.Token{AccessToken: "newer", Expiry: time.Now().Add(time.Hour)}
	assert.NoError(t, storage.Set(t.Context(), "k", newer))

	assert.NoError(t, c.invalidateUsedToken(t.Context(), "older"))

	stored, err := storage.Get(t.Context(), "k")
	assert.NoError(t, err)
	assert.Equal(t, stored, newer)
}

func TestSetInvalidateAndClearToken(t *testing.T) {
	storage := tokenstorage.NewInMemory()
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{TokenStorage: storage, TokenStorageKey: "k"})
	ctx := t.Context()

	seeded := tokenstorage.Token{AccessToken: "seeded", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)}
	assert.NoError(t, c.SetToken(ctx, seeded))
	token, err := c.AccessToken(ctx)
	assert.NoError(t, err)
	assert.Equal(t, token, "seeded")
	assert.Len(t, shop.tokenCalls(), 0)

	assert.NoError(t, c.InvalidateToken(ctx))
	stored, err := storage.Get(ctx, "k")
	assert.NoError(t, err)
	assert.Equal(t, stored, tokenstorage.Token{RefreshToken: "r"})

	assert.NoError(t, c.ClearToken(ctx))
	_, err = storage.Get(ctx, "k")
	assert.True(t, errors.Is(err, tokenstorage.ErrNotFound))

	// Invalidating without a refresh token removes the token.
	assert.NoError(t, c.SetToken(ctx, tokenstorage.Token{AccessToken: "a", Expiry: time.Now().Add(time.Hour)}))
	assert.NoError(t, c.InvalidateToken(ctx))
	_, err = storage.Get(ctx, "k")
	assert.True(t, errors.Is(err, tokenstorage.ErrNotFound))
}

func TestTokenManagementStorageErrors(t *testing.T) {
	broken := &brokenStorage{}
	c := newTestClient(t, newFakeShop(t), Config{TokenStorage: broken})
	ctx := t.Context()

	assert.True(t, errors.Is(c.SetToken(ctx, tokenstorage.Token{}), errBoom))
	assert.True(t, errors.Is(c.InvalidateToken(ctx), errBoom))
	assert.True(t, errors.Is(c.ClearToken(ctx), errBoom))
}

type brokenStorage struct{}

func (*brokenStorage) Get(context.Context, string) (tokenstorage.Token, error) {
	return tokenstorage.Token{}, errBoom
}
func (*brokenStorage) Set(context.Context, string, tokenstorage.Token) error { return errBoom }
func (*brokenStorage) Delete(context.Context, string) error                  { return errBoom }

func TestTokenEndpointErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler func(w http.ResponseWriter, n int, fields map[string]string)
		want    string
		check   func(error) bool
	}{
		{
			name: "rejected credentials",
			handler: func(w http.ResponseWriter, _ int, _ map[string]string) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
			},
			want:  "adminapi: get token: adminapi: status 401: invalid_client",
			check: IsUnauthorized,
		},
		{
			name: "not JSON",
			handler: func(w http.ResponseWriter, _ int, _ map[string]string) {
				_, _ = io.WriteString(w, "<html>maintenance</html>")
			},
			want: "decode token response",
		},
		{
			name: "no access token",
			handler: func(w http.ResponseWriter, _ int, _ map[string]string) {
				writeJSON(w, http.StatusOK, map[string]any{"expires_in": 600})
			},
			want: "has no access_token",
		},
		{
			name: "redirect",
			handler: func(w http.ResponseWriter, _ int, _ map[string]string) {
				w.Header().Set("Location", "https://elsewhere.example.com/")
				w.WriteHeader(http.StatusMovedPermanently)
			},
			want:  "unexpected redirect",
			check: func(err error) bool { return errors.Is(err, ErrUnexpectedRedirect) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shop := newFakeShop(t)
			shop.setToken(tt.handler)
			c := newTestClient(t, shop, Config{})

			_, err := c.Get(t.Context(), "/a")
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			if tt.check != nil {
				assert.True(t, tt.check(err))
			}
			assert.Len(t, shop.apiCalls(), 0)
		})
	}
}

func TestTokenResponseErrorsDoNotLeakTokens(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string // %d is the body length
	}{
		{
			name: "refresh token without access token",
			body: `{"refresh_token":"secret-refresh","expires_in":600}`,
			want: `adminapi: token response (status 200, content type "application/json", %d bytes) has no access_token: invalid token response`,
		},
		{
			name: "cut off JSON with tokens",
			body: `{"access_token":"secret-access","refresh_token":"secret-refresh"`,
			want: `adminapi: decode token response (status 200, content type "application/json", %d bytes): invalid token response: unexpected end of JSON input`,
		},
		{
			name: "token of the wrong type",
			body: `{"access_token":42,"refresh_token":"secret-refresh"}`,
			want: `adminapi: decode token response (status 200, content type "application/json", %d bytes): invalid token response: ` +
				`json: cannot unmarshal number into Go struct field tokenResponse.access_token of type string`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shop := newFakeShop(t)
			shop.setToken(func(w http.ResponseWriter, _ int, _ map[string]string) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tt.body)
			})
			c := newTestClient(t, shop, Config{})

			err := c.Authenticate(t.Context())

			assert.Error(t, err)
			assert.Equal(t, err.Error(), fmt.Sprintf(tt.want, len(tt.body)))
			assert.NotContains(t, err.Error(), "secret")
			assert.True(t, errors.Is(err, ErrInvalidTokenResponse))
		})
	}
}

func TestTokenFailuresAreTokenErrors(t *testing.T) {
	tests := []struct {
		name        string
		handler     func(w http.ResponseWriter, n int, fields map[string]string)
		wantInvalid bool
		wantStatus  int
	}{
		{
			name: "rejected",
			handler: func(w http.ResponseWriter, _ int, _ map[string]string) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "not JSON",
			handler: func(w http.ResponseWriter, _ int, _ map[string]string) {
				_, _ = io.WriteString(w, "<html>")
			},
			wantInvalid: true,
		},
		{
			name: "no access token",
			handler: func(w http.ResponseWriter, _ int, _ map[string]string) {
				writeJSON(w, http.StatusOK, map[string]any{})
			},
			wantInvalid: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shop := newFakeShop(t)
			shop.setToken(tt.handler)
			c := newTestClient(t, shop, Config{})

			err := c.Authenticate(t.Context())

			var tokenErr *TokenError
			assert.ErrorAs(t, err, &tokenErr)
			assert.Equal(t, tokenErr.Error(), err.Error(), "TokenError keeps the message")
			assert.Equal(t, errors.Is(err, ErrInvalidTokenResponse), tt.wantInvalid)
			var apiErr *APIError
			if tt.wantStatus != 0 {
				assert.ErrorAs(t, err, &apiErr)
				assert.Equal(t, apiErr.StatusCode, tt.wantStatus)
			}
		})
	}

	t.Run("unreachable", func(t *testing.T) {
		shop := newFakeShop(t)
		c := newTestClient(t, shop, Config{})
		shop.srv.Close()

		var tokenErr *TokenError
		assert.ErrorAs(t, c.Authenticate(t.Context()), &tokenErr)
	})

	t.Run("storage errors are not token errors", func(t *testing.T) {
		c := newTestClient(t, newFakeShop(t), Config{
			TokenStorage: failingStorage{TokenStorage: tokenstorage.NewInMemory(), getErr: errBoom},
		})

		var tokenErr *TokenError
		assert.False(t, errors.As(c.Authenticate(t.Context()), &tokenErr))
	})
}

func TestTokenEndpointUnreachable(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{})
	shop.srv.Close()

	err := c.Authenticate(t.Context())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "adminapi: token request")
}

func TestCancelledContextStartsNoTokenFetch(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := c.AccessToken(ctx)

	assert.True(t, errors.Is(err, context.Canceled))
	assert.Len(t, shop.tokenCalls(), 0)
}

// gatedStorage pauses the first write that drops an access token until
// release is closed, so a test can run a refresh in the middle of an
// invalidation.
type gatedStorage struct {
	tokenstorage.TokenStorage

	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *gatedStorage) Set(ctx context.Context, key string, token tokenstorage.Token) error {
	if token.AccessToken == "" {
		s.once.Do(func() {
			close(s.entered)
			<-s.release
		})
	}
	return s.TokenStorage.Set(ctx, key, token)
}

func TestInvalidationDoesNotOverwriteRefreshedToken(t *testing.T) {
	shop := newFakeShop(t)
	storage := &gatedStorage{
		TokenStorage: tokenstorage.NewInMemory(),
		entered:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	c := newTestClient(t, shop, Config{
		Credentials:     RefreshTokenCredentials{ClientID: "administration", RefreshToken: "refresh-0"},
		TokenStorage:    storage,
		TokenStorageKey: "k",
	})
	ctx := t.Context()

	// A request used access-0 and got 401. The token has also expired, so
	// the next request refreshes it with refresh-0.
	assert.NoError(t, storage.TokenStorage.Set(ctx, "k", tokenstorage.Token{
		AccessToken: "access-0", RefreshToken: "refresh-0", Expiry: time.Now().Add(-time.Minute),
	}))

	invalidated := make(chan error, 1)
	go func() {
		invalidated <- c.invalidateUsedToken(ctx, "access-0")
	}()
	<-storage.entered // the invalidation has read the token and is writing

	refreshed := make(chan error, 1)
	go func() {
		_, err := c.AccessToken(ctx)
		refreshed <- err
	}()

	select {
	case err := <-refreshed:
		t.Fatalf("the refresh ran while the invalidation held the token (err: %v)", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(storage.release)
	assert.NoError(t, <-invalidated)
	assert.NoError(t, <-refreshed)

	// Shopware revoked refresh-0 when it issued refresh-1. If the
	// invalidation wrote its stale copy after the refresh, refresh-0 would be
	// stored and refresh-1 lost.
	stored, err := storage.Get(ctx, "k")
	assert.NoError(t, err)
	assert.Equal(t, stored.RefreshToken, "refresh-1")
	assert.Equal(t, stored.AccessToken, "access-1")
}

func TestInvalidateTokenRespectsContext(t *testing.T) {
	c := newTestClient(t, newFakeShop(t), Config{})
	c.tokenMu <- struct{}{} // another caller holds the token lock
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	assert.True(t, errors.Is(c.InvalidateToken(ctx), context.Canceled))
}

// blockTokenEndpoint makes the shop hold every token request until release is
// called, and closes entered when the first one arrives. release also runs at
// cleanup, so a failing test does not leave the server waiting.
func blockTokenEndpoint(t *testing.T, shop *fakeShop) (chan struct{}, func()) {
	t.Helper()
	entered, gate := make(chan struct{}), make(chan struct{})
	var enteredOnce, releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate) }) }
	t.Cleanup(release)
	shop.setToken(func(w http.ResponseWriter, n int, fields map[string]string) {
		enteredOnce.Do(func() { close(entered) })
		<-gate
		issueToken(w, n, fields)
	})
	return entered, release
}

func TestSetTokenWaitsForRefreshInProgress(t *testing.T) {
	shop := newFakeShop(t)
	storage := tokenstorage.NewInMemory()
	c := newTestClient(t, shop, Config{
		Credentials:     RefreshTokenCredentials{ClientID: "administration", RefreshToken: "refresh-0"},
		TokenStorage:    storage,
		TokenStorageKey: "k",
	})
	entered, release := blockTokenEndpoint(t, shop)
	ctx := t.Context()

	refreshed := make(chan error, 1)
	go func() {
		_, err := c.AccessToken(ctx)
		refreshed <- err
	}()
	<-entered // the refresh read the storage and waits for the shop

	external := tokenstorage.Token{AccessToken: "browser", RefreshToken: "browser-refresh", Expiry: time.Now().Add(time.Hour)}
	set := make(chan error, 1)
	go func() { set <- c.SetToken(ctx, external) }()

	select {
	case err := <-set:
		t.Fatalf("SetToken returned during the refresh (err: %v)", err)
	case <-time.After(100 * time.Millisecond):
	}
	release()
	assert.NoError(t, <-refreshed)
	assert.NoError(t, <-set)

	// The token set by the caller wins; the refresh did not overwrite it.
	stored, err := storage.Get(ctx, "k")
	assert.NoError(t, err)
	assert.Equal(t, stored, external)
}

func TestClearTokenWaitsForRefreshInProgress(t *testing.T) {
	shop := newFakeShop(t)
	storage := tokenstorage.NewInMemory()
	c := newTestClient(t, shop, Config{
		Credentials:     RefreshTokenCredentials{ClientID: "administration", RefreshToken: "refresh-0"},
		TokenStorage:    storage,
		TokenStorageKey: "k",
	})
	entered, release := blockTokenEndpoint(t, shop)
	ctx := t.Context()

	refreshed := make(chan error, 1)
	go func() {
		_, err := c.AccessToken(ctx)
		refreshed <- err
	}()
	<-entered

	cleared := make(chan error, 1)
	go func() { cleared <- c.ClearToken(ctx) }()

	select {
	case err := <-cleared:
		t.Fatalf("ClearToken returned during the refresh (err: %v)", err)
	case <-time.After(100 * time.Millisecond):
	}
	release()
	assert.NoError(t, <-refreshed)
	assert.NoError(t, <-cleared)

	// A logout stays a logout: the refresh did not store its token again.
	_, err := storage.Get(ctx, "k")
	assert.True(t, errors.Is(err, tokenstorage.ErrNotFound))
}

func TestSetAndClearTokenRespectContext(t *testing.T) {
	c := newTestClient(t, newFakeShop(t), Config{})
	c.tokenMu <- struct{}{} // another caller holds the token lock
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	assert.True(t, errors.Is(c.SetToken(ctx, tokenstorage.Token{AccessToken: "x"}), context.Canceled))
	assert.True(t, errors.Is(c.ClearToken(ctx), context.Canceled))
}

// rotatingShop issues tokens like Shopware: every refresh_token exchange
// revokes the refresh token it used. "refresh-0" is valid at the start.
func rotatingShop(t *testing.T) *fakeShop {
	t.Helper()
	var mu sync.Mutex
	valid := map[string]bool{"refresh-0": true}
	shop := newFakeShop(t)
	shop.setToken(func(w http.ResponseWriter, n int, fields map[string]string) {
		mu.Lock()
		defer mu.Unlock()
		if !valid[fields["refresh_token"]] {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"errors": []map[string]string{{"code": "8", "status": "400", "title": "The refresh token is invalid."}},
			})
			return
		}
		delete(valid, fields["refresh_token"])
		next := fmt.Sprintf("refresh-%d", n)
		valid[next] = true
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": fmt.Sprintf("access-%d", n), "refresh_token": next, "expires_in": 600,
		})
	})
	return shop
}

// unreliableStorage fails writes and deletes while failSet is set.
type unreliableStorage struct {
	tokenstorage.TokenStorage

	failSet atomic.Bool
}

func (s *unreliableStorage) Set(ctx context.Context, key string, token tokenstorage.Token) error {
	if s.failSet.Load() {
		return errBoom
	}
	return s.TokenStorage.Set(ctx, key, token)
}

func (s *unreliableStorage) Delete(ctx context.Context, key string) error {
	if s.failSet.Load() {
		return errBoom
	}
	return s.TokenStorage.Delete(ctx, key)
}

func newRefreshTokenClient(t *testing.T, shop *fakeShop, storage tokenstorage.TokenStorage) *Client {
	t.Helper()
	return newTestClient(t, shop, Config{
		Credentials:     RefreshTokenCredentials{ClientID: "administration", RefreshToken: "refresh-0"},
		TokenStorage:    storage,
		TokenStorageKey: "k",
	})
}

func TestRotatedTokenSurvivesATransientStorageFailure(t *testing.T) {
	shop := rotatingShop(t)
	storage := &unreliableStorage{TokenStorage: tokenstorage.NewInMemory()}
	c := newRefreshTokenClient(t, shop, storage)
	ctx := t.Context()

	storage.failSet.Store(true)
	_, err := c.AccessToken(ctx)
	assert.True(t, errors.Is(err, errBoom), "the storage failure is reported")

	storage.failSet.Store(false)
	token, err := c.AccessToken(ctx)
	assert.NoError(t, err)
	assert.Equal(t, token, "access-1", "the token issued before the failure is used")
	assert.Len(t, shop.tokenCalls(), 1, "no second exchange with the revoked refresh token")

	stored, err := storage.Get(ctx, "k")
	assert.NoError(t, err)
	assert.Equal(t, stored.RefreshToken, "refresh-1")
}

func TestPendingTokenStaysWhileStorageIsDown(t *testing.T) {
	shop := rotatingShop(t)
	storage := &unreliableStorage{TokenStorage: tokenstorage.NewInMemory()}
	c := newRefreshTokenClient(t, shop, storage)
	ctx := t.Context()

	storage.failSet.Store(true)
	for range 3 {
		_, err := c.AccessToken(ctx)
		assert.True(t, errors.Is(err, errBoom))
	}
	assert.Len(t, shop.tokenCalls(), 1, "retries store the pending token instead of exchanging again")

	storage.failSet.Store(false)
	token, err := c.AccessToken(ctx)
	assert.NoError(t, err)
	assert.Equal(t, token, "access-1")
}

func TestPendingTokenDoesNotOverwriteANewerToken(t *testing.T) {
	shop := rotatingShop(t)
	storage := &unreliableStorage{TokenStorage: tokenstorage.NewInMemory()}
	c := newRefreshTokenClient(t, shop, storage)
	ctx := t.Context()

	storage.failSet.Store(true)
	_, err := c.AccessToken(ctx)
	assert.True(t, errors.Is(err, errBoom))
	storage.failSet.Store(false)

	// Meanwhile another process stored its own token.
	newer := tokenstorage.Token{AccessToken: "other", RefreshToken: "other-refresh", Expiry: time.Now().Add(time.Hour)}
	assert.NoError(t, storage.TokenStorage.Set(ctx, "k", newer))

	token, err := c.AccessToken(ctx)
	assert.NoError(t, err)
	assert.Equal(t, token, "other")
	stored, err := storage.Get(ctx, "k")
	assert.NoError(t, err)
	assert.Equal(t, stored, newer, "the newer token is kept")
}

func TestSetAndClearTokenDropThePendingToken(t *testing.T) {
	for name, act := range map[string]func(context.Context, *Client) error{
		"SetToken": func(ctx context.Context, c *Client) error {
			return c.SetToken(ctx, tokenstorage.Token{AccessToken: "browser", RefreshToken: "browser-refresh", Expiry: time.Now().Add(time.Hour)})
		},
		"ClearToken": func(ctx context.Context, c *Client) error { return c.ClearToken(ctx) },
	} {
		t.Run(name, func(t *testing.T) {
			shop := rotatingShop(t)
			storage := &unreliableStorage{TokenStorage: tokenstorage.NewInMemory()}
			c := newRefreshTokenClient(t, shop, storage)
			ctx := t.Context()

			storage.failSet.Store(true)
			_, err := c.AccessToken(ctx)
			assert.True(t, errors.Is(err, errBoom))
			storage.failSet.Store(false)

			assert.NoError(t, act(ctx, c))

			assert.True(t, c.pending == nil, "the caller's action wins over the pending token")
			stored, err := storage.Get(ctx, "k")
			if name == "ClearToken" {
				assert.True(t, errors.Is(err, tokenstorage.ErrNotFound), "a logout stays a logout")
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, stored.AccessToken, "browser")
		})
	}
}

func TestFailedSetOrClearTokenKeepsThePendingToken(t *testing.T) {
	for name, act := range map[string]func(context.Context, *Client) error{
		"SetToken": func(ctx context.Context, c *Client) error {
			return c.SetToken(ctx, tokenstorage.Token{AccessToken: "browser", RefreshToken: "browser-refresh", Expiry: time.Now().Add(time.Hour)})
		},
		"ClearToken": func(ctx context.Context, c *Client) error { return c.ClearToken(ctx) },
	} {
		t.Run(name, func(t *testing.T) {
			shop := rotatingShop(t)
			storage := &unreliableStorage{TokenStorage: tokenstorage.NewInMemory()}
			c := newRefreshTokenClient(t, shop, storage)
			ctx := t.Context()

			storage.failSet.Store(true)
			_, err := c.AccessToken(ctx)
			assert.True(t, errors.Is(err, errBoom))

			// The storage is still down, so this fails too.
			assert.True(t, errors.Is(act(ctx, c), errBoom))
			assert.True(t, c.pending != nil, "the pending token holds the only valid refresh token")

			storage.failSet.Store(false)
			token, err := c.AccessToken(ctx)
			assert.NoError(t, err)
			assert.Equal(t, token, "access-1", "recovery still works")
			assert.Len(t, shop.tokenCalls(), 1)
		})
	}
}

func TestPendingTokenFromNewerTokenFallbackSurvivesAWriteFailure(t *testing.T) {
	storage := &unreliableStorage{TokenStorage: tokenstorage.NewInMemory()}
	shop := newFakeShop(t)
	var mu sync.Mutex
	revoked := map[string]bool{}
	shop.setToken(func(w http.ResponseWriter, n int, fields map[string]string) {
		mu.Lock()
		defer mu.Unlock()
		used := fields["refresh_token"]
		if used == "stale" {
			// Another process already rotated "stale" into "newer" and
			// stored it; its access token has expired.
			_ = storage.TokenStorage.Set(context.Background(), "k", tokenstorage.Token{RefreshToken: "newer"})
		}
		if used != "newer" || revoked[used] {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"errors": []map[string]string{{"code": "8", "status": "400", "title": "The refresh token is invalid."}},
			})
			return
		}
		revoked[used] = true // Shopware revokes the token it exchanges
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": fmt.Sprintf("access-%d", n), "refresh_token": fmt.Sprintf("refresh-%d", n), "expires_in": 600,
		})
	})
	c := newTestClient(t, shop, Config{
		Credentials:     RefreshTokenCredentials{ClientID: "administration", RefreshToken: "stale"},
		TokenStorage:    storage,
		TokenStorageKey: "k",
	})
	ctx := t.Context()
	// This client stored "stale" earlier.
	assert.NoError(t, storage.TokenStorage.Set(ctx, "k", tokenstorage.Token{RefreshToken: "stale"}))

	// "stale" is rejected, the fallback exchanges "newer", and storing the
	// result fails.
	storage.failSet.Store(true)
	_, err := c.AccessToken(ctx)
	assert.True(t, errors.Is(err, errBoom))

	storage.failSet.Store(false)
	token, err := c.AccessToken(ctx)
	assert.NoError(t, err)
	assert.Equal(t, token, "access-2", "the token issued for \"newer\" is kept, not discarded as stale")
	assert.Len(t, shop.tokenCalls(), 2, "no exchange with the revoked \"newer\" token")

	stored, err := storage.Get(ctx, "k")
	assert.NoError(t, err)
	assert.Equal(t, stored.RefreshToken, "refresh-2")
}

func TestInvalidationAfterAFailedWriteKeepsThePendingToken(t *testing.T) {
	for name, invalidate := range map[string]func(context.Context, *Client) error{
		"InvalidateToken": func(ctx context.Context, c *Client) error { return c.InvalidateToken(ctx) },
		// A request that started with access-0 gets a 401 after the refresh.
		"late 401": func(ctx context.Context, c *Client) error { return c.invalidateUsedToken(ctx, "access-0") },
	} {
		t.Run(name, func(t *testing.T) {
			shop := rotatingShop(t)
			storage := &unreliableStorage{TokenStorage: tokenstorage.NewInMemory()}
			c := newRefreshTokenClient(t, shop, storage)
			ctx := t.Context()
			// An expired token, so the next request refreshes with refresh-0.
			assert.NoError(t, storage.TokenStorage.Set(ctx, "k", tokenstorage.Token{
				AccessToken: "access-0", RefreshToken: "refresh-0", Expiry: time.Now().Add(-time.Minute),
			}))

			// The refresh revokes refresh-0, but its result cannot be stored.
			storage.failSet.Store(true)
			_, err := c.AccessToken(ctx)
			assert.True(t, errors.Is(err, errBoom))
			storage.failSet.Store(false)

			assert.NoError(t, invalidate(ctx, c))

			// The pending token was stored first, so its refresh token
			// survives the invalidation and the client recovers.
			stored, err := storage.Get(ctx, "k")
			assert.NoError(t, err)
			assert.Equal(t, stored.RefreshToken, "refresh-1")
			_, err = c.AccessToken(ctx)
			assert.NoError(t, err)
		})
	}
}
