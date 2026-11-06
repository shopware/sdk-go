package signature

import "time"

// Constants for signature verification
const (
	// AppSignatureHeader is the header name for app signature verification
	AppSignatureHeader = "shopware-app-signature"
	// ShopSignatureHeader is the header name for shop signature verification
	ShopSignatureHeader = "Shopware-Shop-Signature"

	// ShopSignaturePreviousHeader is the header for the previous shop secret signature during re-registration confirmation
	ShopSignaturePreviousHeader = "shopware-shop-signature-previous"

	// SecretLength is the length of generated secrets in bytes
	SecretLength = 32
)

// Default durations used when a config or policy leaves the field unset.
const (
	// DefaultReplayWindow is how old a signed timestamp may be before the
	// request is rejected as a possible replay.
	DefaultReplayWindow = 5 * time.Minute

	// DefaultClockSkew is the tolerance for timestamps dated in the future,
	// covering clock drift between the shop and the app server.
	DefaultClockSkew = time.Minute

	// DefaultInflightAllowance is the grace window for accepting requests signed
	// with the previous shop secret after a secret rotation.
	DefaultInflightAllowance = time.Minute
)
