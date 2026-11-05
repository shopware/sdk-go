package signature

import (
	"errors"
	"time"
)

var (
	// ErrMissingPendingShopSecret indicates that a registration confirmation cannot
	// be verified because no pending shop secret is stored for the shop.
	ErrMissingPendingShopSecret = errors.New("missing pending shop secret")

	// ErrMissingShopSignature indicates that a confirmed shop was re-registered
	// without the shop signature proving knowledge of its current secret.
	ErrMissingShopSignature = errors.New("missing shop signature for re-registration")

	// ErrMissingPreviousSignature indicates that a re-registration confirmation
	// arrived without the previous-secret signature.
	ErrMissingPreviousSignature = errors.New("missing previous shop signature for re-registration confirmation")
)

// ShopState carries the subset of shop data needed for signature verification,
// decoupling the signature package from persistence types.
type ShopState struct {
	ShopSecret                     string
	PendingShopSecret              string
	PreviousShopSecret             string
	SecretsRotatedAt               *time.Time
	RegistrationConfirmed          bool
	HasVerifiedWithDoubleSignature bool
}

type DualVerifier struct {
	primary           Verifier
	inflightAllowance time.Duration
}

func NewDualVerifier(primary Verifier, inflightAllowanceSeconds int) *DualVerifier {
	if inflightAllowanceSeconds <= 0 {
		inflightAllowanceSeconds = InflightAllowanceSeconds
	}
	return &DualVerifier{
		primary:           primary,
		inflightAllowance: time.Duration(inflightAllowanceSeconds) * time.Second,
	}
}

// VerifyRegistrationDual verifies the app signature on every registration request.
// A confirmed shop must additionally prove knowledge of its current shop secret, so a
// re-registration cannot be started with the app secret alone.
// Pass nil for shop on new registrations (no existing shop to verify against).
func (d *DualVerifier) VerifyRegistrationDual(
	appSignature, shopSignature, rawQuery, appSecret string,
	shop *ShopState,
	allowUnsignedReregistration bool,
) error {
	if err := d.primary.VerifyRegistration(appSignature, rawQuery, appSecret); err != nil {
		return err
	}

	switch resolveShopProofMode(shop, allowUnsignedReregistration) {
	case proofNotApplicable:
		return nil
	case proofOptional:
		if shopSignature == "" {
			return nil
		}
	case proofRequired:
		if shopSignature == "" {
			return ErrMissingShopSignature
		}
	}

	// Never falls back to PreviousShopSecret: Shopware retries a re-registration with
	// each secret it believes the app may hold, so rejecting one is recovery, not deadlock.
	return d.primary.VerifyRegistration(shopSignature, rawQuery, shop.ShopSecret)
}

// VerifyConfirmationDual verifies the confirmation signature with the pending secret.
// A confirmed shop must additionally prove knowledge of the still-active shop secret
// before that pending secret may be promoted.
func (d *DualVerifier) VerifyConfirmationDual(
	sig, previousSignature string,
	rawBody []byte,
	shop *ShopState,
	allowUnsignedReregistration bool,
) error {
	if shop == nil || shop.PendingShopSecret == "" {
		return ErrMissingPendingShopSecret
	}

	if err := d.primary.VerifyWebhook(sig, rawBody, shop.PendingShopSecret); err != nil {
		return err
	}

	switch resolveShopProofMode(shop, allowUnsignedReregistration) {
	case proofNotApplicable:
		return nil
	case proofOptional:
		if previousSignature == "" {
			return nil
		}
	case proofRequired:
		if previousSignature == "" {
			return ErrMissingPreviousSignature
		}
	}

	return d.primary.VerifyWebhook(previousSignature, rawBody, shop.ShopSecret)
}

// VerifyWebhookWithFallback verifies a webhook signature with the current shop secret.
// On failure, it falls back to the previous secret if within the rotation grace window.
func (d *DualVerifier) VerifyWebhookWithFallback(sig string, rawBody []byte, shop *ShopState) error {
	err := d.primary.VerifyWebhook(sig, rawBody, shop.ShopSecret)
	if err == nil {
		return nil
	}

	if shop.PreviousShopSecret != "" && shop.SecretsRotatedAt != nil {
		elapsed := time.Since(*shop.SecretsRotatedAt)
		if elapsed < d.inflightAllowance {
			return d.primary.VerifyWebhook(sig, rawBody, shop.PreviousShopSecret)
		}
	}

	return err
}

// shopProofMode selects how strictly knowledge of the shop's current secret is checked.
type shopProofMode int

const (
	// proofNotApplicable: no prior shop secret exists to verify a signature against.
	proofNotApplicable shopProofMode = iota

	// proofOptional: the legacy escape hatch. A supplied signature is still verified,
	// so an upgraded Shopware ratchets the shop back to proofRequired on its own.
	proofOptional

	// proofRequired: both an absent and an invalid signature are rejections.
	proofRequired
)

// resolveShopProofMode reports how strictly the caller must prove knowledge of the
// shop's current secret.
func resolveShopProofMode(shop *ShopState, allowUnsignedReregistration bool) shopProofMode {
	if shop == nil || !shop.RegistrationConfirmed {
		return proofNotApplicable
	}
	if !allowUnsignedReregistration {
		return proofRequired
	}

	// A shop that once proved knowledge of its secret is never eligible for the
	// legacy escape hatch again.
	if shop.HasVerifiedWithDoubleSignature {
		return proofRequired
	}

	return proofOptional
}
