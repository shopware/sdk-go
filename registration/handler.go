package registration

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shopware/sdk-go/sdkerrors"
	"github.com/shopware/sdk-go/signature"
	"github.com/shopware/sdk-go/store"
)

// HandlerInterface defines methods for handling registration and confirmation
type HandlerInterface interface {
	HandleRegistration(ctx context.Context, req *Request) (*Response, error)
	HandleConfirmation(ctx context.Context, req *ConfirmationRequest) (*ConfirmationResponse, error)
}

// Config contains configuration for the registration handler
type Config struct {
	AppName   string
	AppSecret string

	// AllowUnsignedReregistration re-enables a known-vulnerable path; see the field
	// of the same name on app.Config. Leave false unless you must serve shops on
	// Shopware older than 6.6.10.15 / 6.7.8.1.
	AllowUnsignedReregistration bool
}

// Handler handles registration and confirmation requests
type Handler struct {
	config       Config
	store        store.ShopStore
	verifier     signature.Verifier
	dualVerifier *signature.DualVerifier
}

// NewHandler creates a new registration handler
func NewHandler(
	config Config,
	shopStore store.ShopStore,
	verifier signature.Verifier,
	dualVerifier *signature.DualVerifier,
) HandlerInterface {
	return &Handler{
		config:       config,
		store:        shopStore,
		verifier:     verifier,
		dualVerifier: dualVerifier,
	}
}

// sanitizeShopURL checks a shop URL and removes double slashes and trailing
// slashes from it.
//
// The checks repeat what adminapi.NewClient requires of Config.BaseURL
// (clients/adminapi/config.go): http or https, a host, no query and no
// fragment. A shop whose URL fails them could register but never get an
// Admin API client, so it is rejected at installation instead.
func sanitizeShopURL(rawURL string) (string, error) {
	u, parseErr := url.Parse(rawURL)
	if parseErr != nil {
		return "", parseErr
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("shop URL must use http or https: %q", rawURL)
	}
	if u.Host == "" {
		return "", fmt.Errorf("shop URL has no host: %q", rawURL)
	}
	// Checked on the raw string: an empty fragment ("https://shop/#") leaves
	// no trace in url.URL.
	if strings.ContainsAny(rawURL, "?#") {
		return "", fmt.Errorf("shop URL must not contain a query or fragment: %q", rawURL)
	}

	// Remove double slashes in path
	path := strings.ReplaceAll(u.Path, "//", "/")
	// Remove trailing slash
	u.Path = strings.TrimSuffix(path, "/")

	return u.String(), nil
}

// wrapSignatureError maps a verification failure onto an SDK error. A missing proof
// header gets its own message so operators can tell an outdated Shopware apart from an
// attempted takeover; both stay 401 / invalid_signature.
func wrapSignatureError(err error) error {
	message := "Invalid signature"

	switch {
	case errors.Is(err, signature.ErrMissingShopSignature):
		message = "Re-registration of a confirmed shop requires the " + signature.ShopSignatureHeader + " header"
	case errors.Is(err, signature.ErrMissingPreviousSignature):
		message = "Re-registration confirmation requires the " + signature.ShopSignaturePreviousHeader + " header"
	}

	return sdkerrors.Wrap(err, sdkerrors.TypeInvalidSignature, message, StatusUnauthorized)
}

// HandleRegistration processes a registration request from Shopware
func (h *Handler) HandleRegistration(ctx context.Context, req *Request) (*Response, error) {
	if validateErr := req.Validate(); validateErr != nil {
		return nil, validateErr
	}

	queryForSignature := signature.PrepareQueryForSignature(req.RawQuery)

	rawShopURL := req.ShopURL
	sanitizedURL, sanitizeErr := sanitizeShopURL(req.ShopURL)
	if sanitizeErr != nil {
		return nil, sdkerrors.Wrap(
			sanitizeErr,
			sdkerrors.TypeInvalidRequest,
			"Invalid shop URL format",
			StatusBadRequest,
		)
	}
	req.ShopURL = sanitizedURL

	timestampStr := strconv.FormatInt(req.Timestamp, 10)
	if timestampErr := h.verifier.VerifyTimestamp(timestampStr); timestampErr != nil {
		return nil, sdkerrors.Wrap(
			timestampErr,
			sdkerrors.TypeTimestampExpired,
			timestampErr.Error(),
			StatusUnauthorized,
		)
	}

	shopSecret := h.verifier.GenerateShopSecret()
	proof := h.verifier.GenerateProof(req.ShopID, rawShopURL, h.config.AppName, h.config.AppSecret)

	existingShop, lookupErr := h.store.GetShopByID(ctx, req.ShopID)

	switch {
	case errors.Is(lookupErr, store.ErrShopNotFound):
		if sigErr := h.dualVerifier.VerifyRegistrationDual(
			req.AppSignature, req.ShopSignature, queryForSignature,
			h.config.AppSecret, nil, h.config.AllowUnsignedReregistration,
		); sigErr != nil {
			return nil, wrapSignatureError(sigErr)
		}

		// HasVerifiedWithDoubleSignature stays false: there is no prior secret here, so
		// any shop signature on a first registration was never verified against anything.
		shop := &store.Shop{
			ShopID:                req.ShopID,
			ShopURL:               req.ShopURL,
			ShopSecret:            shopSecret,
			PendingShopSecret:     shopSecret,
			PendingShopURL:        req.ShopURL,
			Active:                false,
			RegistrationConfirmed: false,
			CreatedAt:             time.Now(),
			UpdatedAt:             time.Now(),
		}
		if storeErr := h.store.UpdateShop(ctx, shop); storeErr != nil {
			return nil, sdkerrors.NewStorageError("Failed to store shop registration", storeErr)
		}

	case lookupErr != nil:
		return nil, sdkerrors.NewStorageError("Failed to look up shop during registration", lookupErr)

	default:
		if sigErr := h.dualVerifier.VerifyRegistrationDual(
			req.AppSignature, req.ShopSignature, queryForSignature,
			h.config.AppSecret,
			&signature.ShopState{
				ShopSecret:                     existingShop.ShopSecret,
				RegistrationConfirmed:          existingShop.RegistrationConfirmed,
				HasVerifiedWithDoubleSignature: existingShop.HasVerifiedWithDoubleSignature,
			},
			h.config.AllowUnsignedReregistration,
		); sigErr != nil {
			return nil, wrapSignatureError(sigErr)
		}

		existingShop.PendingShopSecret = shopSecret
		existingShop.PendingShopURL = req.ShopURL
		existingShop.UpdatedAt = time.Now()
		// Ratchet only on proof that was actually checked: a confirmed shop is the only
		// case where the signature was verified against a stored secret.
		if existingShop.RegistrationConfirmed && req.ShopSignature != "" {
			existingShop.HasVerifiedWithDoubleSignature = true
		}
		if storeErr := h.store.UpdateShop(ctx, existingShop); storeErr != nil {
			return nil, sdkerrors.NewStorageError("Failed to store shop registration", storeErr)
		}
	}

	return &Response{
		Proof:           proof,
		Secret:          shopSecret,
		ConfirmationURL: "",
	}, nil
}

// HandleConfirmation processes a registration confirmation request from Shopware
func (h *Handler) HandleConfirmation(ctx context.Context, req *ConfirmationRequest) (*ConfirmationResponse, error) {
	if validateErr := req.Validate(); validateErr != nil {
		return nil, validateErr
	}

	if timestampErr := h.verifier.VerifyTimestamp(req.Timestamp); timestampErr != nil {
		return nil, sdkerrors.Wrap(
			timestampErr,
			sdkerrors.TypeTimestampExpired,
			timestampErr.Error(),
			StatusUnauthorized,
		)
	}

	shop, getErr := h.store.GetShopByID(ctx, req.ShopID)
	if getErr != nil {
		return nil, sdkerrors.Wrap(
			getErr,
			sdkerrors.TypeShopNotFound,
			"Shop not found or not registered",
			StatusNotFound,
		)
	}

	if verifyErr := h.dualVerifier.VerifyConfirmationDual(
		req.Signature, req.PreviousSignature, req.RawBody,
		&signature.ShopState{
			ShopSecret:                     shop.ShopSecret,
			PendingShopSecret:              shop.PendingShopSecret,
			RegistrationConfirmed:          shop.RegistrationConfirmed,
			HasVerifiedWithDoubleSignature: shop.HasVerifiedWithDoubleSignature,
		},
		h.config.AllowUnsignedReregistration,
	); verifyErr != nil {
		if errors.Is(verifyErr, signature.ErrMissingPendingShopSecret) {
			return nil, sdkerrors.NewRegistrationStateError("Registration confirmation is not pending", verifyErr)
		}
		return nil, wrapSignatureError(verifyErr)
	}

	// See the ratchet comment in HandleRegistration.
	if shop.RegistrationConfirmed && req.PreviousSignature != "" {
		shop.HasVerifiedWithDoubleSignature = true
	}

	oldSecret := shop.ShopSecret

	if shop.PendingShopSecret != "" {
		shop.ShopSecret = shop.PendingShopSecret
		shop.PendingShopSecret = ""
		if shop.PendingShopURL != "" {
			shop.ShopURL = shop.PendingShopURL
			shop.PendingShopURL = ""
		}
		if oldSecret != "" && oldSecret != shop.ShopSecret {
			shop.PreviousShopSecret = oldSecret
			now := time.Now()
			shop.SecretsRotatedAt = &now
		}
	}

	shop.ClientID = req.APIKey
	shop.ClientSecret = req.SecretKey
	shop.RegistrationConfirmed = true
	shop.UpdatedAt = time.Now()

	if updateErr := h.store.UpdateShop(ctx, shop); updateErr != nil {
		return nil, sdkerrors.NewStorageError("Failed to confirm shop registration", updateErr)
	}

	return &ConfirmationResponse{
		Success: true,
		Message: "Registration confirmed successfully",
	}, nil
}
