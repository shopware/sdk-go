package signature

// Constants for signature verification
const (
	// AppSignatureHeader is the header name for app signature verification
	AppSignatureHeader = "shopware-app-signature"
	// ShopSignatureHeader is the header name for shop signature verification
	ShopSignatureHeader = "Shopware-Shop-Signature"
	// ReplayWindowSeconds is the time window in seconds for replay attack protection
	ReplayWindowSeconds = 300

	// ClockSkewSeconds is the tolerance for timestamps dated in the future,
	// covering clock drift between the shop and the app server.
	ClockSkewSeconds = 60

	// ShopSignaturePreviousHeader is the header for the previous shop secret signature during re-registration confirmation
	ShopSignaturePreviousHeader = "shopware-shop-signature-previous"

	// SecretLength is the length of generated secrets in bytes
	SecretLength = 32

	// InflightAllowanceSeconds is the grace window for accepting webhooks signed with the previous secret after rotation
	InflightAllowanceSeconds = 60
)
