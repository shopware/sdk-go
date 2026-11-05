package signature

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Verifier defines the interface for signature verification and generation
type Verifier interface {
	VerifyRegistration(signature, rawQuery, appSecret string) error
	VerifyWebhook(signature string, rawBody []byte, shopSecret string) error
	VerifyTimestamp(timestampStr string) error
	GenerateProof(shopID, shopURL, appName, appSecret string) string
	GenerateShopSecret() string
}

// HMACVerifier implements HMAC-SHA256 signature verification
type HMACVerifier struct{}

// NewVerifier creates a new signature verifier
func NewVerifier() Verifier {
	return &HMACVerifier{}
}

// VerifyRegistration verifies a registration request signature using the app secret
func (v *HMACVerifier) VerifyRegistration(signature, rawQuery, appSecret string) error {
	if signature == "" {
		return fmt.Errorf("missing %s header", AppSignatureHeader)
	}

	expectedSig := v.computeHMAC([]byte(rawQuery), appSecret)
	if !v.constantTimeCompare(signature, expectedSig) {
		return errors.New("invalid signature")
	}

	return nil
}

// VerifyWebhook verifies a webhook request signature using the shop secret
func (v *HMACVerifier) VerifyWebhook(signature string, rawBody []byte, shopSecret string) error {
	if signature == "" {
		return fmt.Errorf("missing %s header", ShopSignatureHeader)
	}

	expectedSig := v.computeHMAC(rawBody, shopSecret)
	if !v.constantTimeCompare(signature, expectedSig) {
		return errors.New("invalid signature")
	}

	return nil
}

// VerifyTimestamp checks if a timestamp is within the replay window.
func (v *HMACVerifier) VerifyTimestamp(timestampStr string) error {
	if timestampStr == "" {
		return ErrMissingTimestamp
	}

	window := ReplayWindowSeconds * time.Second
	if err := CheckFreshness(timestampStr, window, window, time.Now()); err != nil {
		return err
	}

	return nil
}

// GenerateProof generates a registration proof for the confirmation response
func (v *HMACVerifier) GenerateProof(shopID, shopURL, appName, appSecret string) string {
	data := shopID + shopURL + appName
	return v.computeHMAC([]byte(data), appSecret)
}

// GenerateShopSecret generates a cryptographically secure random shop secret
func (v *HMACVerifier) GenerateShopSecret() string {
	bytes := make([]byte, SecretLength)
	// rand.Read always returns nil error for crypto/rand
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func (v *HMACVerifier) computeHMAC(data []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func (v *HMACVerifier) constantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// ComputeSignature computes an HMAC-SHA256 signature for the given data and secret
// This is useful for signing responses or computing signatures for testing
func ComputeSignature(data []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
