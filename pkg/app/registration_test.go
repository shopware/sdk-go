package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	adminstorage "github.com/shopware/app-sdk-go/clients/adminapi/tokenstorage"
	"github.com/shopware/app-sdk-go/pkg/app"
	"github.com/shopware/app-sdk-go/pkg/registration"
	"github.com/shopware/app-sdk-go/pkg/signature"
	"github.com/shopware/app-sdk-go/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	regAppSecret = "test-secret-1234567890"
	regShopID    = "shop-1"
)

// registrationFlow drives a shop through registration and confirmation with
// the App's registration handler, like Shopware does.
type registrationFlow struct {
	t       *testing.T
	sdk     *app.App
	store   store.ShopStore
	shopURL string
	secret  string // the shop's confirmed secret
}

func newRegistrationFlow(t *testing.T, shopURL string) *registrationFlow {
	t.Helper()
	shopStore := store.NewInMemoryStore()
	sdk, err := app.New(app.Config{
		AppName:         "TestApp",
		AppSecret:       regAppSecret,
		ConfirmationURL: "https://test.com/confirm",
		ShopStore:       shopStore,
	})
	require.NoError(t, err)
	return &registrationFlow{t: t, sdk: sdk, store: shopStore, shopURL: shopURL}
}

// register sends a registration and returns the new pending shop secret.
func (f *registrationFlow) register() string {
	f.t.Helper()
	resp, err := f.sendRegistration()
	require.NoError(f.t, err)
	return resp.Secret
}

// sendRegistration sends a signed registration for the flow's shop URL.
func (f *registrationFlow) sendRegistration() (*registration.Response, error) {
	f.t.Helper()
	ts := time.Now().Unix()
	query := fmt.Sprintf("shop-id=%s&shop-url=%s&timestamp=%d", regShopID, f.shopURL, ts)
	req := &registration.Request{
		ShopID:       regShopID,
		ShopURL:      f.shopURL,
		Timestamp:    ts,
		RawQuery:     query,
		AppSignature: signature.ComputeSignature([]byte(query), regAppSecret),
	}
	if f.secret != "" {
		req.ShopSignature = signature.ComputeSignature([]byte(query), f.secret)
	}
	return f.sdk.RegistrationHandler().HandleRegistration(context.Background(), req)
}

// confirm sends the confirmation with the given credentials, signed with
// signWith.
func (f *registrationFlow) confirm(pending, signWith, clientID, clientSecret string) error {
	f.t.Helper()
	req := registration.ConfirmationRequest{
		ShopID:    regShopID,
		ShopURL:   f.shopURL,
		APIKey:    clientID,
		SecretKey: clientSecret,
		Timestamp: strconv.FormatInt(time.Now().Unix(), 10),
	}
	body, err := json.Marshal(req)
	require.NoError(f.t, err)
	req.RawBody = body
	req.Signature = signature.ComputeSignature(body, signWith)
	if f.secret != "" {
		req.PreviousSignature = signature.ComputeSignature(body, f.secret)
	}
	_, err = f.sdk.RegistrationHandler().HandleConfirmation(context.Background(), &req)
	if err == nil {
		f.secret = pending
	}
	return err
}

// install registers and confirms the shop and stores a token for it.
func (f *registrationFlow) install(clientID, clientSecret string) *store.Shop {
	f.t.Helper()
	pending := f.register()
	require.NoError(f.t, f.confirm(pending, pending, clientID, clientSecret))

	shop, err := f.store.GetShopByID(context.Background(), regShopID)
	require.NoError(f.t, err)
	client, err := f.sdk.NewClientForShop(shop)
	require.NoError(f.t, err)
	require.NoError(f.t, client.SetToken(context.Background(), adminstorage.Token{
		AccessToken: "old-token",
		Expiry:      time.Now().Add(time.Hour),
	}))
	return shop
}

// storedToken returns the token a client for shop uses. It is "old-token"
// while the stored token exists, otherwise a new one fetched from the shop.
func (f *registrationFlow) storedToken(shop *store.Shop) string {
	f.t.Helper()
	client, err := f.sdk.NewClientForShop(shop)
	require.NoError(f.t, err)
	token, err := client.AccessToken(context.Background())
	require.NoError(f.t, err)
	return token
}

func TestApp_ReRegistrationDeletesOldToken(t *testing.T) {
	tests := map[string]struct {
		clientID, clientSecret string
		wantDeleted            bool
	}{
		"new client ID and secret": {"client-2", "secret-2", true},
		"only the secret changes":  {"client-1", "secret-2", true},
		"same credentials":         {"client-1", "secret-1", false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			shop := newTokenShop(t)
			f := newRegistrationFlow(t, shop.URL)
			old := f.install("client-1", "secret-1")

			pending := f.register()
			require.NoError(t, f.confirm(pending, pending, tt.clientID, tt.clientSecret))

			if tt.wantDeleted {
				assert.NotEqual(t, "old-token", f.storedToken(old), "the token of the old credentials is deleted")
				assert.Equal(t, int32(1), shop.tokenCalls.Load())
			} else {
				assert.Equal(t, "old-token", f.storedToken(old), "unchanged credentials keep their token")
				assert.Zero(t, shop.tokenCalls.Load())
			}
		})
	}
}

func TestApp_FailedConfirmationKeepsToken(t *testing.T) {
	shop := newTokenShop(t)
	f := newRegistrationFlow(t, shop.URL)
	old := f.install("client-1", "secret-1")

	pending := f.register()
	err := f.confirm(pending, "wrong-secret", "client-2", "secret-2")
	require.Error(t, err)

	assert.Equal(t, "old-token", f.storedToken(old), "a rejected confirmation does not touch the token")
}

func TestApp_RegistrationRejectsShopURLUnusableForTheAdminAPI(t *testing.T) {
	// The Admin API client cannot be created for such a URL, so the shop
	// must not be able to register with it.
	for _, shopURL := range []string{"https://shop.example.com/?tenant=x", "https://shop.example.com#", "shop.example.com"} {
		t.Run(shopURL, func(t *testing.T) {
			f := newRegistrationFlow(t, shopURL)

			_, err := f.sendRegistration()

			require.Error(t, err)
			assert.Contains(t, err.Error(), "Invalid shop URL format")
			_, getErr := f.store.GetShopByID(context.Background(), regShopID)
			require.ErrorIs(t, getErr, store.ErrShopNotFound)
		})
	}
}
