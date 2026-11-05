package adminapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shopware/app-sdk-go/clients/adminapi/internal/assert"
	"github.com/shopware/app-sdk-go/clients/adminapi/tokenstorage"
)

func TestTokenRequestBodies(t *testing.T) {
	password := PasswordCredentials{Username: "admin", Password: "shopware"}
	refresh := RefreshTokenCredentials{ClientID: "shopware-cli", RefreshToken: "initial"}

	tests := []struct {
		name         string
		creds        Credentials
		refreshToken string
		want         map[string]string
		identity     string
	}{
		{
			name: "integration ignores refresh token", creds: integration, refreshToken: "r",
			want:     map[string]string{"grant_type": "client_credentials", "client_id": "id", "client_secret": "secret"},
			identity: "integration:id",
		},
		{
			name: "password login", creds: password,
			want: map[string]string{
				"grant_type": "password", "client_id": "administration", "scopes": "write",
				"username": "admin", "password": "shopware",
			},
			identity: "password:admin",
		},
		{
			name: "password refresh", creds: password, refreshToken: "r",
			want:     map[string]string{"grant_type": "refresh_token", "client_id": "administration", "refresh_token": "r"},
			identity: "password:admin",
		},
		{
			name: "refresh token uses initial token", creds: refresh,
			want:     map[string]string{"grant_type": "refresh_token", "client_id": "shopware-cli", "refresh_token": "initial"},
			identity: "refresh:shopware-cli:ac1b5c0961a7269b",
		},
		{
			name: "refresh token prefers stored token", creds: refresh, refreshToken: "stored",
			want:     map[string]string{"grant_type": "refresh_token", "client_id": "shopware-cli", "refresh_token": "stored"},
			identity: "refresh:shopware-cli:ac1b5c0961a7269b",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.creds.TokenRequest(t.Context(), tt.refreshToken)
			assert.NoError(t, err)
			assert.Equal(t, got, tt.want)
			assert.Equal(t, tt.creds.Identity(), tt.identity)
		})
	}
}

func TestPasswordLoginThenRefreshWithoutPassword(t *testing.T) {
	clk := newClock()
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{Credentials: PasswordCredentials{Username: "admin", Password: "shopware"}})
	c.now = clk.Now

	assert.NoError(t, c.Authenticate(t.Context()))
	clk.Advance(time.Hour)
	assert.NoError(t, c.Authenticate(t.Context()))
	clk.Advance(time.Hour)
	assert.NoError(t, c.Authenticate(t.Context()))

	calls := shop.tokenCalls()
	assert.Len(t, calls, 3)
	assert.Equal(t, calls[0]["grant_type"], "password")
	assert.Equal(t, calls[1]["refresh_token"], "refresh-1")
	assert.Equal(t, calls[2]["refresh_token"], "refresh-2", "the rotated refresh token is used")
	for _, call := range calls[1:] {
		assert.Equal(t, call["grant_type"], "refresh_token")
		_, sent := call["password"]
		assert.False(t, sent, "the password is not sent again")
	}
}

func TestRejectedRefreshTokenFallsBackToPassword(t *testing.T) {
	shop := newFakeShop(t)
	shop.setToken(func(w http.ResponseWriter, n int, fields map[string]string) {
		if fields["grant_type"] == "refresh_token" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		}
		issueToken(w, n, fields)
	})
	storage := tokenstorage.NewInMemory()
	c := newTestClient(t, shop, Config{
		Credentials:     PasswordCredentials{Username: "admin", Password: "shopware"},
		TokenStorage:    storage,
		TokenStorageKey: "k",
	})
	assert.NoError(t, storage.Set(t.Context(), "k", tokenstorage.Token{RefreshToken: "expired"}))

	token, err := c.AccessToken(t.Context())
	assert.NoError(t, err)
	assert.Equal(t, token, "access-2")

	calls := shop.tokenCalls()
	assert.Len(t, calls, 2)
	assert.Equal(t, calls[0]["refresh_token"], "expired")
	assert.Equal(t, calls[1]["grant_type"], "password")

	stored, err := storage.Get(t.Context(), "k")
	assert.NoError(t, err)
	assert.Equal(t, stored.RefreshToken, "refresh-2")
}

func TestRejectedRefreshTokenUsesTokenStoredByAnotherProcess(t *testing.T) {
	storage := tokenstorage.NewInMemory()
	shop := newFakeShop(t)
	shop.setToken(func(w http.ResponseWriter, _ int, _ map[string]string) {
		// Another process refreshed first; Shopware rejects the old token.
		_ = storage.Set(context.Background(), "k", tokenstorage.Token{
			AccessToken: "other", RefreshToken: "other-refresh", Expiry: time.Now().Add(time.Hour),
		})
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
	})
	c := newTestClient(t, shop, Config{
		Credentials:     PasswordCredentials{Username: "admin", Password: "shopware"},
		TokenStorage:    storage,
		TokenStorageKey: "k",
	})
	assert.NoError(t, storage.Set(t.Context(), "k", tokenstorage.Token{RefreshToken: "old"}))

	token, err := c.AccessToken(t.Context())
	assert.NoError(t, err)
	assert.Equal(t, token, "other")
	assert.Len(t, shop.tokenCalls(), 1, "no password login")
}

func TestRejectedRefreshTokenTriesNewerStoredRefreshToken(t *testing.T) {
	storage := tokenstorage.NewInMemory()
	shop := newFakeShop(t)
	shop.setToken(func(w http.ResponseWriter, n int, fields map[string]string) {
		if fields["refresh_token"] == "old" {
			// Another process rotated the token, but its access token is not
			// valid any more.
			_ = storage.Set(context.Background(), "k", tokenstorage.Token{RefreshToken: "newer"})
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		}
		issueToken(w, n, fields)
	})
	c := newTestClient(t, shop, Config{
		Credentials:     PasswordCredentials{Username: "admin", Password: "shopware"},
		TokenStorage:    storage,
		TokenStorageKey: "k",
	})
	assert.NoError(t, storage.Set(t.Context(), "k", tokenstorage.Token{RefreshToken: "old"}))

	assert.NoError(t, c.Authenticate(t.Context()))

	calls := shop.tokenCalls()
	assert.Len(t, calls, 2)
	assert.Equal(t, calls[1]["refresh_token"], "newer")
}

func TestRejectedIntegrationCredentialsAreNotRetried(t *testing.T) {
	shop := newFakeShop(t)
	shop.setToken(func(w http.ResponseWriter, _ int, _ map[string]string) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
	})
	c := newTestClient(t, shop, Config{})

	assert.True(t, IsUnauthorized(c.Authenticate(t.Context())))
	assert.Len(t, shop.tokenCalls(), 1)
}

func TestRefreshFallbackOnlyForRejectedGrant(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		wantFallback bool
	}{
		{
			name:         "OAuth invalid_grant",
			status:       http.StatusBadRequest,
			body:         `{"error":"invalid_grant","error_description":"The refresh token is invalid."}`,
			wantFallback: true,
		},
		{
			name:         "Shopware invalid refresh token",
			status:       http.StatusBadRequest,
			body:         `{"errors":[{"code":"8","status":"400","title":"The refresh token is invalid.","detail":"Token has been revoked"}]}`,
			wantFallback: true,
		},
		{
			name:         "Shopware invalid grant",
			status:       http.StatusBadRequest,
			body:         `{"errors":[{"code":"10","status":"400","title":"The provided authorization grant is invalid."}]}`,
			wantFallback: true,
		},
		{
			name:   "OAuth invalid_client",
			status: http.StatusUnauthorized,
			body:   `{"error":"invalid_client"}`,
		},
		{
			name:   "Shopware invalid client",
			status: http.StatusUnauthorized,
			body:   `{"errors":[{"code":"4","status":"401","title":"Client authentication failed"}]}`,
		},
		{
			name:   "Shopware malformed request",
			status: http.StatusBadRequest,
			body:   `{"errors":[{"code":"3","status":"400","title":"The request is missing a required parameter"}]}`,
		},
		{
			name:   "unknown body",
			status: http.StatusBadRequest,
			body:   `Bad Request`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shop := newFakeShop(t)
			shop.setToken(func(w http.ResponseWriter, n int, fields map[string]string) {
				if fields["grant_type"] == "refresh_token" {
					w.WriteHeader(tt.status)
					_, _ = io.WriteString(w, tt.body)
					return
				}
				issueToken(w, n, fields)
			})
			storage := tokenstorage.NewInMemory()
			c := newTestClient(t, shop, Config{
				Credentials:     PasswordCredentials{Username: "admin", Password: "shopware"},
				TokenStorage:    storage,
				TokenStorageKey: "k",
			})
			assert.NoError(t, storage.Set(t.Context(), "k", tokenstorage.Token{RefreshToken: "old"}))

			err := c.Authenticate(t.Context())

			calls := shop.tokenCalls()
			if tt.wantFallback {
				assert.NoError(t, err)
				assert.Len(t, calls, 2)
				assert.Equal(t, calls[1]["grant_type"], "password")
				return
			}
			assert.Error(t, err)
			assert.Len(t, calls, 1, "the password is not sent")
			var tokenErr *TokenError
			assert.ErrorAs(t, err, &tokenErr)
		})
	}
}

func TestServerErrorOnRefreshIsNotRetriedWithPassword(t *testing.T) {
	shop := newFakeShop(t)
	shop.setToken(func(w http.ResponseWriter, _ int, _ map[string]string) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	storage := tokenstorage.NewInMemory()
	c := newTestClient(t, shop, Config{
		Credentials:     PasswordCredentials{Username: "admin", Password: "shopware"},
		TokenStorage:    storage,
		TokenStorageKey: "k",
	})
	assert.NoError(t, storage.Set(t.Context(), "k", tokenstorage.Token{RefreshToken: "r"}))

	assert.Error(t, c.Authenticate(t.Context()))
	assert.Len(t, shop.tokenCalls(), 1)
}

func TestRotatedRefreshTokenIsStoredBeforeUse(t *testing.T) {
	storage := tokenstorage.NewInMemory()
	var seen tokenstorage.Token
	shop := newFakeShop(t)
	shop.setAPI(func(w http.ResponseWriter, _ *http.Request) {
		seen, _ = storage.Get(context.Background(), "k")
		w.WriteHeader(http.StatusNoContent)
	})
	c := newTestClient(t, shop, Config{
		Credentials:     RefreshTokenCredentials{ClientID: "shopware-cli", RefreshToken: "initial"},
		TokenStorage:    storage,
		TokenStorageKey: "k",
	})

	_, err := c.Get(t.Context(), "/_info/me")
	assert.NoError(t, err)

	assert.Equal(t, shop.tokenCalls()[0]["refresh_token"], "initial")
	assert.Equal(t, seen.RefreshToken, "refresh-1", "stored before the API request was sent")
	assert.Equal(t, seen.AccessToken, "access-1")
}

func TestFailedStoreOfRotatedTokenFailsRequest(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{
		Credentials:  RefreshTokenCredentials{ClientID: "shopware-cli", RefreshToken: "initial"},
		TokenStorage: failingStorage{TokenStorage: tokenstorage.NewInMemory(), setErr: errBoom},
	})

	_, err := c.Get(t.Context(), "/a")
	assert.True(t, errors.Is(err, errBoom))
	assert.Len(t, shop.apiCalls(), 0)
}

func TestRefreshTokenIsKeptWhenResponseHasNone(t *testing.T) {
	shop := newFakeShop(t)
	shop.setToken(func(w http.ResponseWriter, _ int, _ map[string]string) {
		writeJSON(w, http.StatusOK, map[string]any{"access_token": "a", "expires_in": 600})
	})
	storage := tokenstorage.NewInMemory()
	c := newTestClient(t, shop, Config{
		Credentials:     RefreshTokenCredentials{ClientID: "shopware-cli", RefreshToken: "initial"},
		TokenStorage:    storage,
		TokenStorageKey: "k",
	})

	assert.NoError(t, c.Authenticate(t.Context()))

	stored, err := storage.Get(t.Context(), "k")
	assert.NoError(t, err)
	assert.Equal(t, stored.RefreshToken, "initial")
}

// vaultCredentials is a custom grant that loads its secret lazily.
type vaultCredentials struct {
	secret func(context.Context) (string, error)
}

func (v vaultCredentials) TokenRequest(ctx context.Context, _ string) (map[string]string, error) {
	secret, err := v.secret(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]string{"grant_type": "client_credentials", "client_id": "vault", "client_secret": secret}, nil
}

func (vaultCredentials) Identity() string { return "vault" }

func TestCustomCredentials(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{Credentials: vaultCredentials{
		secret: func(context.Context) (string, error) { return "from-vault", nil },
	}})

	assert.NoError(t, c.Authenticate(t.Context()))
	assert.Equal(t, shop.tokenCalls()[0]["client_secret"], "from-vault")
}

func TestCredentialsErrorIsPassedOn(t *testing.T) {
	shop := newFakeShop(t)
	c := newTestClient(t, shop, Config{Credentials: vaultCredentials{
		secret: func(context.Context) (string, error) { return "", errBoom },
	}})

	err := c.Authenticate(t.Context())
	assert.True(t, errors.Is(err, errBoom))
	assert.Contains(t, err.Error(), "build token request")
	assert.Len(t, shop.tokenCalls(), 0)
}

func TestRefreshTokenLoginsOfDifferentUsersDoNotShareTokens(t *testing.T) {
	shop := newFakeShop(t)
	shop.setToken(func(w http.ResponseWriter, _ int, fields map[string]string) {
		// The shop issues tokens per user; the refresh token tells them apart.
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token":  "access-of-" + fields["refresh_token"],
			"refresh_token": fields["refresh_token"] + "-rotated",
			"expires_in":    600,
		})
	})
	storage := tokenstorage.NewInMemory() // shared, default storage keys
	alice := newTestClient(t, shop, Config{
		Credentials:  RefreshTokenCredentials{ClientID: "administration", RefreshToken: "alice"},
		TokenStorage: storage,
	})
	bob := newTestClient(t, shop, Config{
		Credentials:  RefreshTokenCredentials{ClientID: "administration", RefreshToken: "bob"},
		TokenStorage: storage,
	})

	aliceToken, err := alice.AccessToken(t.Context())
	assert.NoError(t, err)
	bobToken, err := bob.AccessToken(t.Context())
	assert.NoError(t, err)

	assert.Equal(t, aliceToken, "access-of-alice")
	assert.Equal(t, bobToken, "access-of-bob", "bob does not get alice's token")
	assert.NotEqual(t, alice.tokenKey, bob.tokenKey)
}

func TestRefreshTokenIdentity(t *testing.T) {
	creds := RefreshTokenCredentials{ClientID: "administration", RefreshToken: "secret-refresh-token"}

	assert.False(t, strings.Contains(creds.Identity(), "secret"), "the identity does not reveal the token")
	assert.Equal(t, creds.Identity(), creds.Identity(), "the identity is stable")
	other := RefreshTokenCredentials{ClientID: "administration", RefreshToken: "other"}
	assert.NotEqual(t, creds.Identity(), other.Identity())
}
