// Package auth provides token-based authentication for frontend requests.
// This is a reference implementation.
// Feel free to customize or use JWT, sessions, or other approaches instead.
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	// MinSecretLength is the minimum required length for the encryption secret (AES-256)
	MinSecretLength = 32
)

var (
	// ErrInvalidToken is returned when token validation fails
	ErrInvalidToken = errors.New("invalid token")
	// ErrExpiredToken is returned when token has expired
	ErrExpiredToken = errors.New("token has expired")
	// ErrSessionExpired is returned when a token is too old to be renewed,
	// i.e. the absolute session lifetime has elapsed. The caller must
	// re-authenticate with a freshly signed Shopware query.
	ErrSessionExpired = errors.New("session has expired")
)

// TokenManager handles encrypted token generation and validation.
type TokenManager struct {
	secretKey []byte
}

// ShopContext contains the authenticated shop information from the token.
type ShopContext struct {
	ShopID  string `json:"shopId"`
	ShopURL string `json:"shopUrl"`

	// ExpiresAt is when this token stops granting access.
	ExpiresAt time.Time `json:"expiresAt"`

	// SessionStartedAt anchors the absolute session lifetime. Renew carries it
	// over unchanged, so refreshing a token cannot extend the session beyond
	// RenewOptions.MaxSessionAge — without it, a sliding session would recreate
	// the very problem short-lived tokens are meant to solve.
	SessionStartedAt time.Time `json:"sessionStartedAt"`
}

// RenewOptions configures how a token may be refreshed.
type RenewOptions struct {
	// Duration is the lifetime of the newly issued token.
	Duration time.Duration

	// Grace is how long after expiry a token can still be renewed. It lets a
	// client recover reactively from a 401 instead of having to refresh ahead
	// of time; the absolute session cap still applies.
	Grace time.Duration

	// MaxSessionAge caps how long a session may be extended by refreshing,
	// measured from SessionStartedAt.
	MaxSessionAge time.Duration
}

// NewTokenManager creates a token manager with the given secret.
// The secret should be at least 32 bytes for AES-256 encryption.
func NewTokenManager(secret string) (*TokenManager, error) {
	if len(secret) < MinSecretLength {
		return nil, fmt.Errorf("secret must be at least %d bytes, got %d", MinSecretLength, len(secret))
	}

	// Use first 32 bytes for AES-256
	return &TokenManager{
		secretKey: []byte(secret)[:MinSecretLength],
	}, nil
}

// Generate creates an encrypted token containing shop context and starts a new
// session. The token is encrypted using AES-GCM and base64 encoded.
func (tm *TokenManager) Generate(shopID, shopURL string, duration time.Duration) (string, error) {
	now := time.Now()

	return tm.seal(ShopContext{
		ShopID:           shopID,
		ShopURL:          shopURL,
		ExpiresAt:        now.Add(duration),
		SessionStartedAt: now,
	})
}

// Renew issues a fresh token from an existing one, keeping the original session
// start. It accepts a token that expired less than opts.Grace ago, but refuses
// once opts.MaxSessionAge has elapsed since the session began — at that point
// the client must re-authenticate through a newly signed Shopware query.
//
// The returned context describes the renewed token. Callers should re-check that
// the shop still exists and is active before handing the new token out.
func (tm *TokenManager) Renew(token string, opts RenewOptions) (string, *ShopContext, error) {
	ctx, err := tm.open(token)
	if err != nil {
		return "", nil, err
	}

	now := time.Now()

	// Tokens minted before sessions were tracked cannot be renewed safely,
	// because their session start is unknown.
	if ctx.SessionStartedAt.IsZero() {
		return "", nil, ErrSessionExpired
	}

	if now.After(ctx.ExpiresAt.Add(opts.Grace)) {
		return "", nil, ErrExpiredToken
	}

	sessionEnd := ctx.SessionStartedAt.Add(opts.MaxSessionAge)
	if now.After(sessionEnd) {
		return "", nil, ErrSessionExpired
	}

	// Never let a token outlive the session it belongs to.
	expiresAt := now.Add(opts.Duration)
	if expiresAt.After(sessionEnd) {
		expiresAt = sessionEnd
	}

	renewed := ShopContext{
		ShopID:           ctx.ShopID,
		ShopURL:          ctx.ShopURL,
		ExpiresAt:        expiresAt,
		SessionStartedAt: ctx.SessionStartedAt,
	}

	sealed, err := tm.seal(renewed)
	if err != nil {
		return "", nil, err
	}

	return sealed, &renewed, nil
}

// seal encrypts a token payload.
func (tm *TokenManager) seal(ctx ShopContext) (string, error) {
	// Marshal to JSON
	payload, err := json.Marshal(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to marshal token payload: %w", err)
	}

	// Create AES cipher
	block, err := aes.NewCipher(tm.secretKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, randErr := io.ReadFull(rand.Reader, nonce); randErr != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", randErr)
	}

	// Encrypt payload
	ciphertext := gcm.Seal(nonce, nonce, payload, nil)

	// Base64 encode
	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

// Validate decrypts a token and rejects it once expired, returning the shop context.
func (tm *TokenManager) Validate(token string) (*ShopContext, error) {
	ctx, err := tm.open(token)
	if err != nil {
		return nil, err
	}

	// Check expiration
	if time.Now().After(ctx.ExpiresAt) {
		return nil, ErrExpiredToken
	}

	return ctx, nil
}

// open decrypts a token without applying the expiry check, so that Renew can
// still read a recently expired token.
func (tm *TokenManager) open(token string) (*ShopContext, error) {
	// Decode base64
	ciphertext, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("%w: decode failed: %w", ErrInvalidToken, err)
	}

	// Create AES cipher
	block, err := aes.NewCipher(tm.secretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Check minimum length
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, ErrInvalidToken
	}

	// Extract nonce and ciphertext
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: decrypt failed: %w", ErrInvalidToken, err)
	}

	// Unmarshal JSON
	var ctx ShopContext
	if unmarshalErr := json.Unmarshal(plaintext, &ctx); unmarshalErr != nil {
		return nil, fmt.Errorf("%w: unmarshal failed: %w", ErrInvalidToken, unmarshalErr)
	}

	return &ctx, nil
}
