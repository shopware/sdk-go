package tokenstorage

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by TokenStorage.Get and KeyValueStore.Get when no
// token is stored under the key.
var ErrNotFound = errors.New("tokenstorage: token not found")

// Token is an OAuth token of the Shopware Admin API.
//
// A Token whose access token has expired can still be useful: its refresh
// token may be exchanged for a new access token. Storages therefore keep a
// Token until it is deleted or its storage TTL runs out, and leave the
// decision whether it is still valid to the client.
type Token struct {
	// AccessToken is the bearer token sent with API requests. It is empty
	// after the client invalidated it (for example after a 401 response).
	AccessToken string

	// RefreshToken is used to get a new access token without logging in
	// again. It is empty for the client_credentials grant.
	RefreshToken string

	// Expiry is the moment the access token expires.
	Expiry time.Time
}

// TokenStorage stores tokens under string keys. Implementations must be safe
// for concurrent use.
type TokenStorage interface {
	// Get returns the token stored under key, or ErrNotFound.
	Get(ctx context.Context, key string) (Token, error)

	// Set stores token under key, replacing any previous token.
	Set(ctx context.Context, key string, token Token) error

	// Delete removes the token stored under key. Deleting a missing key is
	// not an error.
	Delete(ctx context.Context, key string) error
}

// Locker serializes token refreshes and invalidations for a key across
// processes, for example with Redis SET NX PX. It is optional: without it,
// they are serialized within one client only.
type Locker interface {
	// Lock blocks until the lock for key is held or ctx is done. The returned
	// function releases the lock.
	Lock(ctx context.Context, key string) (unlock func(), err error)
}
