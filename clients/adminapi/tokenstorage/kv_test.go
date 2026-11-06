package tokenstorage

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shopware/sdk-go/clients/adminapi/internal/assert"
)

// mapStore is an in-memory KeyValueStore that records TTLs.
type mapStore struct {
	mu      sync.Mutex
	values  map[string][]byte
	ttls    map[string]time.Duration
	deleted []string
	err     error
}

func newMapStore() *mapStore {
	return &mapStore{values: map[string][]byte{}, ttls: map[string]time.Duration{}}
}

func (m *mapStore) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	v, ok := m.values[key]
	if !ok {
		return nil, ErrNotFound
	}
	return v, nil
}

func (m *mapStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.values[key] = value
	m.ttls[key] = ttl
	return nil
}

func (m *mapStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	delete(m.values, key)
	m.deleted = append(m.deleted, key)
	return nil
}

func newTestKV(store KeyValueStore, now time.Time, opts ...KVOption) *KV {
	s := NewKV(store, opts...)
	s.now = func() time.Time { return now }
	return s
}

func TestKVRoundTrip(t *testing.T) {
	store := newMapStore()
	s := newTestKV(store, sample.Expiry.Add(-time.Hour))
	ctx := t.Context()

	_, err := s.Get(ctx, "k")
	assert.True(t, errors.Is(err, ErrNotFound))

	assert.NoError(t, s.Set(ctx, "k", sample))
	got, err := s.Get(ctx, "k")
	assert.NoError(t, err)
	assert.Equal(t, got.AccessToken, sample.AccessToken)
	assert.Equal(t, got.RefreshToken, sample.RefreshToken)
	assert.True(t, got.Expiry.Equal(sample.Expiry))

	raw := store.values[DefaultKeyPrefix+"k"]
	assert.JSONEq(t, string(raw),
		`{"v":1,"access_token":"a","refresh_token":"r","expiry":"2026-01-01T12:00:00Z"}`)

	assert.NoError(t, s.Delete(ctx, "k"))
	_, err = s.Get(ctx, "k")
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestKVKeyPrefix(t *testing.T) {
	store := newMapStore()
	s := newTestKV(store, time.Now(), WithKeyPrefix("myapp:"))

	assert.NoError(t, s.Set(t.Context(), "k", sample))
	_, ok := store.values["myapp:k"]
	assert.True(t, ok)
}

func TestKVTTL(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		token Token
		opts  []KVOption
		want  time.Duration
	}{
		{
			name:  "no refresh token: until expiry plus grace",
			token: Token{AccessToken: "a", Expiry: now.Add(10 * time.Minute)},
			want:  10*time.Minute + expiryGrace,
		},
		{
			name:  "refresh token: default refresh TTL",
			token: Token{AccessToken: "a", RefreshToken: "r", Expiry: now.Add(10 * time.Minute)},
			want:  DefaultRefreshTokenTTL,
		},
		{
			name:  "refresh token: custom refresh TTL",
			token: Token{RefreshToken: "r"},
			opts:  []KVOption{WithRefreshTokenTTL(time.Hour)},
			want:  time.Hour,
		},
		{
			name:  "already expired: minimum TTL",
			token: Token{AccessToken: "a", Expiry: now.Add(-time.Hour)},
			want:  minTTL,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMapStore()
			s := newTestKV(store, now, tt.opts...)
			assert.NoError(t, s.Set(t.Context(), "k", tt.token))
			assert.Equal(t, store.ttls[DefaultKeyPrefix+"k"], tt.want)
		})
	}
}

func TestKVUnreadableValueIsAMissAndNotDeleted(t *testing.T) {
	tests := map[string][]byte{
		"not JSON":        []byte("garbage"),
		"unknown version": []byte(`{"v":99,"access_token":"a"}`),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			store := newMapStore()
			store.values[DefaultKeyPrefix+"k"] = raw
			s := NewKV(store)

			ctx := t.Context()

			_, err := s.Get(ctx, "k")
			assert.True(t, errors.Is(err, ErrNotFound))
			// Deleting could remove a token another process stored after
			// the read; the next Set replaces the value instead.
			assert.Empty(t, store.deleted)

			assert.NoError(t, s.Set(ctx, "k", sample))
			got, err := s.Get(ctx, "k")
			assert.NoError(t, err)
			assert.Equal(t, got.AccessToken, sample.AccessToken)
		})
	}
}

func TestKVStoreErrorsArePassedOn(t *testing.T) {
	store := newMapStore()
	store.err = errBoom
	s := NewKV(store)
	ctx := t.Context()

	_, err := s.Get(ctx, "k")
	assert.True(t, errors.Is(err, errBoom))
	assert.False(t, errors.Is(err, ErrNotFound))
	assert.True(t, errors.Is(s.Set(ctx, "k", sample), errBoom))
	assert.True(t, errors.Is(s.Delete(ctx, "k"), errBoom))
}

// xorCipher is a toy cipher for tests; failDecrypt simulates a rotated key.
type xorCipher struct {
	failEncrypt, failDecrypt bool
}

func (c xorCipher) Encrypt(p []byte) ([]byte, error) {
	if c.failEncrypt {
		return nil, errBoom
	}
	return xor(p), nil
}

func (c xorCipher) Decrypt(p []byte) ([]byte, error) {
	if c.failDecrypt {
		return nil, errBoom
	}
	return xor(p), nil
}

func xor(p []byte) []byte {
	out := make([]byte, len(p))
	for i, b := range p {
		out[i] = b ^ 0x5a
	}
	return out
}

func TestKVCipher(t *testing.T) {
	store := newMapStore()
	s := NewKV(store, WithCipher(xorCipher{}))
	ctx := t.Context()

	assert.NoError(t, s.Set(ctx, "k", sample))
	raw := store.values[DefaultKeyPrefix+"k"]
	assert.False(t, bytes.Contains(raw, []byte(`"refresh_token"`)), "stored value is encrypted")

	got, err := s.Get(ctx, "k")
	assert.NoError(t, err)
	assert.Equal(t, got.RefreshToken, "r")
}

func TestKVCipherErrors(t *testing.T) {
	store := newMapStore()
	ctx := t.Context()

	err := NewKV(store, WithCipher(xorCipher{failEncrypt: true})).Set(ctx, "k", sample)
	assert.True(t, errors.Is(err, errBoom))

	assert.NoError(t, NewKV(store, WithCipher(xorCipher{})).Set(ctx, "k", sample))
	_, err = NewKV(store, WithCipher(xorCipher{failDecrypt: true})).Get(ctx, "k")
	assert.True(t, errors.Is(err, ErrNotFound), "a value that cannot be decrypted is a miss")
}
