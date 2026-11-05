package tokenstorage

import (
	"context"
	"strconv"
)

// Scoped puts every key of a parent storage under a fixed scope. Use it to
// share one backend between several applications or environments.
type Scoped struct {
	parent TokenStorage
	prefix string
}

// NewScoped returns a storage that stores tokens in parent under
// "<length of scope>:<scope>:<key>", for example "5:app-a:shop-1". The
// length makes the encoding unambiguous: two different scope and key pairs
// never produce the same effective key, whatever characters they contain.
func NewScoped(parent TokenStorage, scope string) *Scoped {
	return &Scoped{parent: parent, prefix: strconv.Itoa(len(scope)) + ":" + scope + ":"}
}

// Get returns the token stored under the scoped key.
func (s *Scoped) Get(ctx context.Context, key string) (Token, error) {
	return s.parent.Get(ctx, s.key(key))
}

// Set stores token under the scoped key.
func (s *Scoped) Set(ctx context.Context, key string, token Token) error {
	return s.parent.Set(ctx, s.key(key), token)
}

// Delete removes the token stored under the scoped key.
func (s *Scoped) Delete(ctx context.Context, key string) error {
	return s.parent.Delete(ctx, s.key(key))
}

func (s *Scoped) key(key string) string {
	return s.prefix + key
}
