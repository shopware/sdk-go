package tokenstorage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	// DefaultKeyPrefix is the key prefix NewKV uses unless WithKeyPrefix is
	// given.
	DefaultKeyPrefix = "shopware:adminapi:token:"

	// DefaultRefreshTokenTTL matches the refresh token lifetime of Shopware
	// (one week). A stored token that carries a refresh token is kept this
	// long.
	DefaultRefreshTokenTTL = 7 * 24 * time.Hour

	// expiryGrace keeps a token without a refresh token a little longer than
	// its expiry, so clocks that differ slightly do not drop it early.
	expiryGrace = time.Minute

	// minTTL is the shortest TTL passed to the KeyValueStore. Some stores
	// treat a zero or negative TTL as "never expire".
	minTTL = time.Second

	kvFormatVersion = 1
)

// KeyValueStore is the small interface NewKV needs from an external store.
// It maps directly to Redis or Valkey GET, SET with PX, and DEL.
//
// Implementations must be safe for concurrent use.
type KeyValueStore interface {
	// Get returns the value stored under key, or ErrNotFound.
	Get(ctx context.Context, key string) ([]byte, error)

	// Set stores value under key. The store should drop the key after ttl.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete removes key. Deleting a missing key is not an error.
	Delete(ctx context.Context, key string) error
}

// Cipher encrypts tokens before NewKV writes them to the store, and decrypts
// them after reading. This package ships no cipher; use for example AES-GCM
// from crypto/cipher, or a key management service.
type Cipher interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

// KVOption configures NewKV.
type KVOption func(*KV)

// WithKeyPrefix sets the prefix put in front of every key. The default is
// DefaultKeyPrefix.
func WithKeyPrefix(prefix string) KVOption {
	return func(s *KV) { s.prefix = prefix }
}

// WithRefreshTokenTTL sets how long a token that carries a refresh token is
// kept. The default is DefaultRefreshTokenTTL.
func WithRefreshTokenTTL(ttl time.Duration) KVOption {
	return func(s *KV) { s.refreshTTL = ttl }
}

// WithCipher encrypts tokens at rest.
func WithCipher(c Cipher) KVOption {
	return func(s *KV) { s.cipher = c }
}

// KV is a TokenStorage on top of a KeyValueStore.
type KV struct {
	store      KeyValueStore
	prefix     string
	refreshTTL time.Duration
	cipher     Cipher
	now        func() time.Time
}

// NewKV returns a TokenStorage that keeps tokens in store.
//
// Each token is written as a small versioned JSON document (encrypted when
// WithCipher is set) with a TTL: until the access token expires when there is
// no refresh token, otherwise the refresh token TTL. A value that cannot be
// read (corrupt, unknown format, failed decryption) is treated as missing, so
// the client simply fetches a new token and its Set replaces the value. The
// value is not deleted: another process may have stored a fresh token under
// the key in the meantime, and deleting it would lose its refresh token.
func NewKV(store KeyValueStore, opts ...KVOption) *KV {
	s := &KV{
		store:      store,
		prefix:     DefaultKeyPrefix,
		refreshTTL: DefaultRefreshTokenTTL,
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type kvDocument struct {
	Version      int       `json:"v"`
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry"`
}

// Get returns the token stored under key, or ErrNotFound.
func (s *KV) Get(ctx context.Context, key string) (Token, error) {
	storeKey := s.prefix + key
	raw, err := s.store.Get(ctx, storeKey)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Token{}, ErrNotFound
		}
		return Token{}, fmt.Errorf("tokenstorage: get %q: %w", storeKey, err)
	}

	token, ok := s.decode(raw)
	if !ok {
		// A miss, not a delete: the next Set replaces the value. A delete
		// could remove a token another process stored after this read.
		return Token{}, ErrNotFound
	}
	return token, nil
}

// Set stores token under key.
func (s *KV) Set(ctx context.Context, key string, token Token) error {
	raw, err := s.encode(token)
	if err != nil {
		return err
	}
	storeKey := s.prefix + key
	if err = s.store.Set(ctx, storeKey, raw, s.ttl(token)); err != nil {
		return fmt.Errorf("tokenstorage: set %q: %w", storeKey, err)
	}
	return nil
}

// Delete removes the token stored under key.
func (s *KV) Delete(ctx context.Context, key string) error {
	storeKey := s.prefix + key
	if err := s.store.Delete(ctx, storeKey); err != nil {
		return fmt.Errorf("tokenstorage: delete %q: %w", storeKey, err)
	}
	return nil
}

func (s *KV) ttl(token Token) time.Duration {
	ttl := s.refreshTTL
	if token.RefreshToken == "" {
		ttl = token.Expiry.Sub(s.now()) + expiryGrace
	}
	return max(ttl, minTTL)
}

func (s *KV) encode(token Token) ([]byte, error) {
	raw, err := json.Marshal(kvDocument{
		Version:      kvFormatVersion,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
	})
	if err != nil {
		return nil, fmt.Errorf("tokenstorage: encode token: %w", err)
	}
	if s.cipher == nil {
		return raw, nil
	}
	encrypted, err := s.cipher.Encrypt(raw)
	if err != nil {
		return nil, fmt.Errorf("tokenstorage: encrypt token: %w", err)
	}
	return encrypted, nil
}

func (s *KV) decode(raw []byte) (Token, bool) {
	if s.cipher != nil {
		plain, err := s.cipher.Decrypt(raw)
		if err != nil {
			return Token{}, false
		}
		raw = plain
	}

	var doc kvDocument
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Version != kvFormatVersion {
		return Token{}, false
	}
	return Token{
		AccessToken:  doc.AccessToken,
		RefreshToken: doc.RefreshToken,
		Expiry:       doc.Expiry,
	}, true
}
