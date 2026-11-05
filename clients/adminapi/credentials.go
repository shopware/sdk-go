package adminapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

// Credentials decide how the client logs in to the shop.
//
// The client calls TokenRequest whenever it needs a new access token and
// POSTs the returned fields as JSON to /api/oauth/token. refreshToken is the
// refresh token of the stored token, or "" when there is none. If the shop
// rejects a refresh token, the client calls TokenRequest once more with "".
//
// Implement Credentials to use a grant this package does not provide, or to
// load secrets lazily (for example from a vault).
type Credentials interface {
	// TokenRequest returns the body of the token request.
	TokenRequest(ctx context.Context, refreshToken string) (map[string]string, error)

	// Identity returns a stable ID of the principal, such as the client ID
	// or the user name. The client uses it to build the default storage key,
	// so it must differ for every principal that can log in to the shop,
	// even when they use the same OAuth client. It must not contain secrets
	// in plain text; use a hash instead.
	Identity() string
}

// fingerprintLen is how many hex characters of a SHA-256 hash identify a
// refresh token in RefreshTokenCredentials.Identity.
const fingerprintLen = 16

// administrationClientID is the OAuth client of the Shopware administration,
// used for admin user logins.
const administrationClientID = "administration"

// Field names of the token request.
const (
	fieldGrantType    = "grant_type"
	fieldClientID     = "client_id"
	fieldClientSecret = "client_secret"
	fieldRefreshToken = "refresh_token"
	fieldUsername     = "username"
	fieldPassword     = "password"
	fieldScopes       = "scopes"
)

// IntegrationCredentials log in with the client ID and secret of an app or
// integration (client_credentials grant). The shop issues no refresh token
// for this grant.
type IntegrationCredentials struct {
	ClientID     string
	ClientSecret string
}

// TokenRequest returns a client_credentials request.
func (c IntegrationCredentials) TokenRequest(context.Context, string) (map[string]string, error) {
	return map[string]string{
		fieldGrantType:    "client_credentials",
		fieldClientID:     c.ClientID,
		fieldClientSecret: c.ClientSecret,
	}, nil
}

// Identity returns "integration:" + ClientID.
func (c IntegrationCredentials) Identity() string {
	return "integration:" + c.ClientID
}

// PasswordCredentials log in as an admin user with a user name and password.
//
// The first login uses the password grant. After that the client uses the
// refresh token it received, so the password is sent again only when the
// refresh token is rejected (expired or revoked).
type PasswordCredentials struct {
	Username string
	Password string
}

// TokenRequest returns a refresh_token request when refreshToken is set, and
// a password request otherwise.
func (c PasswordCredentials) TokenRequest(_ context.Context, refreshToken string) (map[string]string, error) {
	if refreshToken != "" {
		return refreshTokenRequest(administrationClientID, refreshToken), nil
	}
	return map[string]string{
		fieldGrantType: "password",
		fieldClientID:  administrationClientID,
		fieldScopes:    "write",
		fieldUsername:  c.Username,
		fieldPassword:  c.Password,
	}, nil
}

// Identity returns "password:" + Username.
func (c PasswordCredentials) Identity() string {
	return "password:" + c.Username
}

// RefreshTokenCredentials log in with a refresh token obtained elsewhere, for
// example by a browser login. RefreshToken is used until the client stores a
// newer one; after that the stored token wins.
type RefreshTokenCredentials struct {
	// ClientID is the OAuth client the refresh token was issued to.
	ClientID string

	// RefreshToken is the initial refresh token.
	RefreshToken string
}

// TokenRequest returns a refresh_token request with the stored refresh token,
// or with the initial one when none is stored.
func (c RefreshTokenCredentials) TokenRequest(_ context.Context, refreshToken string) (map[string]string, error) {
	if refreshToken == "" {
		refreshToken = c.RefreshToken
	}
	return refreshTokenRequest(c.ClientID, refreshToken), nil
}

// Identity returns "refresh:" + ClientID + ":" + a fingerprint of the initial
// refresh token. One OAuth client (for example "administration") issues
// tokens to many admin users, so the client ID alone would let clients of
// different users share a stored token. The fingerprint is a truncated
// SHA-256 hash and does not reveal the token. It stays the same while the
// client rotates the stored refresh token, because it is built from the
// initial one; a new initial token (a new login) gets its own storage key.
func (c RefreshTokenCredentials) Identity() string {
	sum := sha256.Sum256([]byte(c.RefreshToken))
	return "refresh:" + c.ClientID + ":" + hex.EncodeToString(sum[:])[:fingerprintLen]
}

func refreshTokenRequest(clientID, refreshToken string) map[string]string {
	return map[string]string{
		fieldGrantType:    grantTypeRefreshToken,
		fieldClientID:     clientID,
		fieldRefreshToken: refreshToken,
	}
}
