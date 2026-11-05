package tokenstorage_test

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/shopware/app-sdk-go/clients/adminapi/tokenstorage"
)

// mapStore stands in for Redis or Valkey. A real implementation calls the
// driver: GET, SET with PX, and DEL, and maps "key not found" to
// tokenstorage.ErrNotFound. With go-redis for example:
//
//	func (s redisStore) Get(ctx context.Context, key string) ([]byte, error) {
//		v, err := s.rdb.Get(ctx, key).Bytes()
//		if errors.Is(err, redis.Nil) {
//			return nil, tokenstorage.ErrNotFound
//		}
//		return v, err
//	}
//
//	func (s redisStore) Set(ctx context.Context, key string, v []byte, ttl time.Duration) error {
//		return s.rdb.Set(ctx, key, v, ttl).Err()
//	}
//
//	func (s redisStore) Delete(ctx context.Context, key string) error {
//		return s.rdb.Del(ctx, key).Err()
//	}
type mapStore struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (m *mapStore) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[key]
	if !ok {
		return nil, tokenstorage.ErrNotFound
	}
	return v, nil
}

func (m *mapStore) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[key] = value
	return nil
}

func (m *mapStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, key)
	return nil
}

// Keep tokens in an external key-value store. Pass the result as
// adminapi.Config.TokenStorage.
func ExampleNewKV() {
	store := tokenstorage.NewKV(&mapStore{values: map[string][]byte{}},
		tokenstorage.WithKeyPrefix("myapp:token:"))

	ctx := context.Background()
	_ = store.Set(ctx, "shop-1", tokenstorage.Token{AccessToken: "abc", Expiry: time.Now().Add(time.Hour)})

	token, err := store.Get(ctx, "shop-1")
	fmt.Println(token.AccessToken, err)

	_, err = store.Get(ctx, "shop-2")
	fmt.Println(errors.Is(err, tokenstorage.ErrNotFound))
	// Output:
	// abc <nil>
	// true
}

// aesGCM encrypts tokens with AES-GCM from the standard library. Load the key
// from a secret store; never hard-code it.
type aesGCM struct {
	aead cipher.AEAD
}

func newAESGCM(key []byte) (*aesGCM, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &aesGCM{aead: aead}, nil
}

func (c *aesGCM) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (c *aesGCM) Decrypt(ciphertext []byte) ([]byte, error) {
	size := c.aead.NonceSize()
	if len(ciphertext) < size {
		return nil, errors.New("ciphertext too short")
	}
	return c.aead.Open(nil, ciphertext[:size], ciphertext[size:], nil)
}

// Encrypt tokens before they are written to the store.
func ExampleWithCipher() {
	key := make([]byte, 32) // AES-256; load it from a secret store
	c, err := newAESGCM(key)
	if err != nil {
		fmt.Println(err)
		return
	}

	backend := &mapStore{values: map[string][]byte{}}
	store := tokenstorage.NewKV(backend, tokenstorage.WithCipher(c))

	ctx := context.Background()
	_ = store.Set(ctx, "shop-1", tokenstorage.Token{RefreshToken: "secret-refresh-token"})

	token, _ := store.Get(ctx, "shop-1")
	fmt.Println(token.RefreshToken)
	// Output: secret-refresh-token
}
