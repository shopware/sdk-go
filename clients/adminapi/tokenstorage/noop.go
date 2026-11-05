package tokenstorage

import "context"

// NoOp is a TokenStorage that stores nothing, so the client fetches a new
// token for every request (concurrent requests share one fetch). Use it in
// tests or when tokens must never be kept.
//
// It does not work with every kind of credentials:
//   - IntegrationCredentials: works, with one token request per request.
//   - PasswordCredentials: works, but the password is sent with every
//     request.
//   - RefreshTokenCredentials: does not work. Shopware revokes the refresh
//     token on the first exchange, and the new one is discarded, so
//     adminapi.NewClient rejects this combination, also when NoOp is wrapped
//     in Scoped (see Discards).
type NoOp struct{}

// NewNoOp returns a storage that stores nothing.
func NewNoOp() NoOp { return NoOp{} }

// Get always returns ErrNotFound.
func (NoOp) Get(context.Context, string) (Token, error) { return Token{}, ErrNotFound }

// Set discards the token.
func (NoOp) Set(context.Context, string, Token) error { return nil }

// Delete does nothing.
func (NoOp) Delete(context.Context, string) error { return nil }

// Discards reports whether storage is known to keep no tokens: a NoOp, also
// when wrapped in Scoped. Clients use it to reject credentials that cannot
// work without storage, such as refresh-token logins. A custom storage that
// keeps nothing cannot be detected and reports false.
func Discards(storage TokenStorage) bool {
	switch s := storage.(type) {
	case NoOp, *NoOp:
		return true
	case *Scoped:
		return s != nil && Discards(s.parent)
	default:
		return false
	}
}
