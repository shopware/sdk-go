package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/shopware/sdk-go/clients/adminapi"
	adminstorage "github.com/shopware/sdk-go/clients/adminapi/tokenstorage"
	"github.com/shopware/sdk-go/store"
)

// clientIDHashLen is how many hex characters of the client ID hash are part
// of a shop's token key.
const clientIDHashLen = 16

// NewClient creates the Admin API client for a shop. All clients of a shop
// share its OAuth token through Config.TokenStorage.
func (a *App) NewClient(ctx context.Context, shopID string) (*adminapi.Client, error) {
	shop, err := a.store.GetShopByID(ctx, shopID)
	if err != nil {
		return nil, err
	}
	return a.NewClientForShop(shop)
}

// NewClientForShop creates the Admin API client for a shop instance, for
// example the shop of a verified webhook. It returns an error when the shop
// has no valid URL or credentials.
func (a *App) NewClientForShop(shop *store.Shop) (*adminapi.Client, error) {
	return adminapi.NewClient(a.adminConfig(shop))
}

// ClearShopToken removes the stored Admin API token of a shop. Call it when
// the app is removed from the shop. The gin integration does this for you.
func (a *App) ClearShopToken(ctx context.Context, shop *store.Shop) error {
	if shop == nil {
		return errors.New("app: shop is required")
	}
	return deleteToken(ctx, a.tokenStorage, a.tokenLocker, shopTokenKey(shop))
}

// deleteToken deletes the token stored under key while holding the token
// lock of that key, the same lock the Admin API clients take to fetch a
// token. A fetch that is running when the token is deleted would otherwise
// store its token again afterwards.
func deleteToken(
	ctx context.Context, storage adminstorage.TokenStorage, locker adminstorage.Locker, key string,
) error {
	unlock, err := locker.Lock(ctx, key)
	if err != nil {
		return err
	}
	defer unlock()
	return storage.Delete(ctx, key)
}

// adminConfig builds the Admin API client configuration for shop.
func (a *App) adminConfig(shop *store.Shop) adminapi.Config {
	if shop == nil {
		// adminapi.NewClient rejects a config without credentials.
		return adminapi.Config{}
	}

	return adminapi.Config{
		BaseURL: shop.ShopURL,
		Credentials: adminapi.IntegrationCredentials{
			ClientID:     shop.ClientID,
			ClientSecret: shop.ClientSecret,
		},
		HTTPClient:      a.config.HTTPClient,
		UserAgent:       a.config.UserAgent,
		TokenStorage:    a.tokenStorage,
		TokenStorageKey: shopTokenKey(shop),
		Locker:          a.tokenLocker,
	}
}

// shopTokenKey is the storage key of a shop's token. It contains a hash of
// the client ID, so when a shop registers again and gets new credentials, the
// token of the old credentials is not used any more. tokenCleanupHandler
// deletes that token once the new registration is confirmed.
func shopTokenKey(shop *store.Shop) string {
	sum := sha256.Sum256([]byte(shop.ClientID))
	return shop.ShopID + ":" + hex.EncodeToString(sum[:])[:clientIDHashLen]
}
