// Package signature provides HMAC-SHA256 signature verification for Shopware requests.
//
// This package implements the cryptographic signature verification used by Shopware
// to authenticate requests between the shop and your app.
//
// # Signature Verification
//
// Create a verifier and verify signatures:
//
//	import "github.com/shopware/sdk-go/signature"
//
//	verifier := signature.NewVerifier()
//
//	// Verify registration signature (query string)
//	err := verifier.VerifyRegistration(signature, queryString, secret)
//
//	// Verify webhook signature (request body)
//	err := verifier.VerifyWebhook(signature, requestBody, secret)
//
// # Signature Types
//
// ## Registration Signatures
//
// Used for GET requests during app registration and iframe authentication:
//
//	// Query string WITHOUT the signature parameter
//	query := "shop-id=test&shop-url=https://example.com&timestamp=1234567890"
//	signature := request.Header.Get("shopware-app-signature")
//	err := verifier.VerifyRegistration(signature, query, appSecret)
//
// ## Webhook Signatures
//
// Used for POST requests (webhooks, action buttons):
//
//	body, _ := io.ReadAll(request.Body)
//	signature := request.Header.Get("shopware-shop-signature")
//	err := verifier.VerifyWebhook(signature, body, shopSecret)
//
// # Timestamp Verification
//
// Optionally verify request timestamps to prevent replay attacks:
//
//	timestamp := request.URL.Query().Get("timestamp")
//	err := verifier.VerifyTimestamp(timestamp)
//	// Returns error if timestamp is older than 5 minutes
//
// # Computing Signatures
//
// Generate signatures for testing or validation:
//
//	data := []byte("request-data")
//	secret := "your-secret"
//	sig := signature.ComputeSignature(data, secret)
//
// # Security Considerations
//
// ## Secrets
//
//   - AppSecret: From manifest.xml, used for registration
//   - ShopSecret: Generated during registration, used for webhooks
//   - Never log or expose secrets
//   - Store securely (environment variables, secret management)
//
// ## Constant-Time Comparison
//
// Signature verification uses crypto/subtle.ConstantTimeCompare to prevent
// timing attacks.
//
// ## Timestamp Window
//
// Default window is 5 minutes to balance security and clock skew tolerance.
//
// # Header Names
//
// Standard Shopware signature headers:
//
//	const (
//	    AppSignatureHeader  = "shopware-app-signature"  // Registration
//	    ShopSignatureHeader = "shopware-shop-signature" // Webhooks
//	)
//
// # Algorithm
//
// Signatures use HMAC-SHA256:
//
//	HMAC-SHA256(data, secret) → hex-encoded hash
//
// # Error Handling
//
// Verification returns specific errors:
//
//	err := verifier.VerifyWebhook(sig, body, secret)
//	if errors.Is(err, signature.ErrInvalidSignature) {
//	    // Signature mismatch
//	}
//	if errors.Is(err, signature.ErrTimestampExpired) {
//	    // Timestamp too old
//	}
//
// # Thread Safety
//
// Verifier is safe for concurrent use and stateless.
package signature
