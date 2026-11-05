package app

import (
	"context"

	adminstorage "github.com/shopware/app-sdk-go/clients/adminapi/tokenstorage"
	"github.com/shopware/app-sdk-go/pkg/registration"
	"github.com/shopware/app-sdk-go/pkg/store"
)

// tokenCleanupHandler deletes the stored Admin API token of a shop when a
// registration confirmation replaces the shop's credentials. Without it, the
// token of the old credentials would stay in the storage until its TTL ends,
// and would even be reused when only the client secret changes, because the
// storage key is derived from the client ID.
type tokenCleanupHandler struct {
	registration.HandlerInterface

	store   store.ShopStore
	storage adminstorage.TokenStorage
	locker  adminstorage.Locker
}

// HandleConfirmation confirms the registration and then deletes the token of
// the previous credentials, if they changed.
func (h tokenCleanupHandler) HandleConfirmation(
	ctx context.Context, req *registration.ConfirmationRequest,
) (*registration.ConfirmationResponse, error) {
	// Read the previous credentials before the confirmation replaces them.
	var previousKey, previousID, previousSecret string
	if req != nil {
		if previous, err := h.store.GetShopByID(ctx, req.ShopID); err == nil && previous.ClientID != "" {
			previousKey = shopTokenKey(previous)
			previousID, previousSecret = previous.ClientID, previous.ClientSecret
		}
	}

	resp, err := h.HandlerInterface.HandleConfirmation(ctx, req)
	if err != nil || previousKey == "" {
		return resp, err
	}

	if previousID != req.APIKey || previousSecret != req.SecretKey {
		// Best effort: the confirmation already succeeded and must not fail
		// now. A token left behind is not used with the new client ID and
		// expires with its TTL.
		_ = deleteToken(ctx, h.storage, h.locker, previousKey)
	}
	return resp, nil
}
