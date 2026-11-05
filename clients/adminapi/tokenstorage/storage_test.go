package tokenstorage

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/shopware/sdk-go/clients/adminapi/internal/assert"
)

var (
	errBoom = errors.New("boom")
	sample  = Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
)

func TestInMemoryRoundTrip(t *testing.T) {
	s := NewInMemory()
	ctx := t.Context()

	_, err := s.Get(ctx, "k")
	assert.True(t, errors.Is(err, ErrNotFound))

	assert.NoError(t, s.Set(ctx, "k", sample))
	got, err := s.Get(ctx, "k")
	assert.NoError(t, err)
	assert.Equal(t, got, sample)

	_, err = s.Get(ctx, "other")
	assert.True(t, errors.Is(err, ErrNotFound), "keys are isolated")

	assert.NoError(t, s.Delete(ctx, "k"))
	_, err = s.Get(ctx, "k")
	assert.True(t, errors.Is(err, ErrNotFound))
	assert.NoError(t, s.Delete(ctx, "k"), "deleting a missing key is fine")
}

func TestInMemoryKeepsExpiredTokens(t *testing.T) {
	s := NewInMemory()
	expired := Token{RefreshToken: "still-useful", Expiry: time.Now().Add(-time.Hour)}
	assert.NoError(t, s.Set(t.Context(), "k", expired))

	got, err := s.Get(t.Context(), "k")
	assert.NoError(t, err)
	assert.Equal(t, got, expired)
}

func TestInMemoryConcurrentUse(t *testing.T) {
	s := NewInMemory()
	ctx := t.Context()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			key := fmt.Sprintf("k%d", i%4)
			_ = s.Set(ctx, key, sample)
			_, _ = s.Get(ctx, key)
			_ = s.Delete(ctx, key)
		})
	}
	wg.Wait()
}

func TestNoOpStoresNothing(t *testing.T) {
	s := NewNoOp()
	assert.NoError(t, s.Set(t.Context(), "k", sample))
	_, err := s.Get(t.Context(), "k")
	assert.True(t, errors.Is(err, ErrNotFound))
	assert.NoError(t, s.Delete(t.Context(), "k"))
}

func TestScopedPrefixesKeys(t *testing.T) {
	parent := NewInMemory()
	a := NewScoped(parent, "app-a")
	b := NewScoped(parent, "app-b")
	ctx := t.Context()

	assert.NoError(t, a.Set(ctx, "k", sample))
	_, err := b.Get(ctx, "k")
	assert.True(t, errors.Is(err, ErrNotFound), "scopes are isolated")

	got, err := parent.Get(ctx, "5:app-a:k")
	assert.NoError(t, err)
	assert.Equal(t, got, sample)

	got, err = a.Get(ctx, "k")
	assert.NoError(t, err)
	assert.Equal(t, got, sample)

	assert.NoError(t, a.Delete(ctx, "k"))
	_, err = parent.Get(ctx, "5:app-a:k")
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestScopedKeysDoNotCollide(t *testing.T) {
	parent := NewInMemory()
	ctx := t.Context()

	// With a plain separator both pairs would map to the same key.
	pairs := []struct{ scope, key string }{
		{"a", "\x00b"},
		{"a\x00", "b"},
		{"a", ":b"},
		{"a:", "b"},
		{"1:a", "b"},
		{"1", "a:b"},
		{"", "a"},
		{"a", ""},
	}
	for i, p := range pairs {
		token := Token{AccessToken: strconv.Itoa(i)}
		assert.NoError(t, NewScoped(parent, p.scope).Set(ctx, p.key, token))
	}
	for i, p := range pairs {
		got, err := NewScoped(parent, p.scope).Get(ctx, p.key)
		assert.NoError(t, err)
		assert.Equal(t, got.AccessToken, strconv.Itoa(i), "scope %q, key %q", p.scope, p.key)
	}
}

func TestDiscards(t *testing.T) {
	noop := NewNoOp()
	var nilScoped *Scoped
	tests := []struct {
		name    string
		storage TokenStorage
		want    bool
	}{
		{"NoOp", noop, true},
		{"pointer to NoOp", &noop, true},
		{"NoOp in Scoped", NewScoped(noop, "app"), true},
		{"NoOp in nested Scoped", NewScoped(NewScoped(noop, "app"), "env"), true},
		{"in-memory", NewInMemory(), false},
		{"in-memory in Scoped", NewScoped(NewInMemory(), "app"), false},
		{"nil", nil, false},
		{"nil Scoped", nilScoped, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, Discards(tt.storage), tt.want)
		})
	}
}
