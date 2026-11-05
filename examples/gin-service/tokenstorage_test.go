package main

import (
	"context"
	"encoding/base64"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adminstorage "github.com/shopware/app-sdk-go/clients/adminapi/tokenstorage"
)

var discardLogger = slog.New(slog.DiscardHandler)

func redisConfig(addr string) Config {
	return Config{
		TokenStorage:   tokenStorageRedis,
		RedisAddr:      addr,
		TokenKeyPrefix: "test:token:",
	}
}

func TestTokenStorage_Memory(t *testing.T) {
	storage, closeFn, err := newTokenStorage(context.Background(), Config{TokenStorage: "memory"}, discardLogger)

	require.NoError(t, err)
	assert.Nil(t, storage, "nil makes the App keep tokens in memory")
	require.NoError(t, closeFn())
}

func TestTokenStorage_Redis(t *testing.T) {
	mr := miniredis.RunT(t)
	storage, closeFn, err := newTokenStorage(context.Background(), redisConfig(mr.Addr()), discardLogger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeFn() })
	ctx := context.Background()

	_, err = storage.Get(ctx, "shop-1")
	require.ErrorIs(t, err, adminstorage.ErrNotFound, "a missing key is reported as ErrNotFound")

	token := adminstorage.Token{AccessToken: "access", Expiry: time.Now().Add(10 * time.Minute)}
	require.NoError(t, storage.Set(ctx, "shop-1", token))

	got, err := storage.Get(ctx, "shop-1")
	require.NoError(t, err)
	assert.Equal(t, "access", got.AccessToken)

	assert.True(t, mr.Exists("test:token:shop-1"), "the key prefix is used")
	assert.Greater(t, mr.TTL("test:token:shop-1"), 10*time.Minute, "the key expires with the token")

	require.NoError(t, storage.Delete(ctx, "shop-1"))
	assert.False(t, mr.Exists("test:token:shop-1"))
}

func TestTokenStorage_RedisEncrypted(t *testing.T) {
	mr := miniredis.RunT(t)
	config := redisConfig(mr.Addr())
	config.TokenEncryptionKey = base64.StdEncoding.EncodeToString(make([]byte, encryptionKeyLen))
	storage, closeFn, err := newTokenStorage(context.Background(), config, discardLogger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeFn() })
	ctx := context.Background()

	require.NoError(t, storage.Set(ctx, "shop-1", adminstorage.Token{RefreshToken: "secret-refresh-token"}))

	raw, err := mr.Get("test:token:shop-1")
	require.NoError(t, err)
	assert.NotContains(t, raw, "secret-refresh-token", "the stored value is encrypted")

	got, err := storage.Get(ctx, "shop-1")
	require.NoError(t, err)
	assert.Equal(t, "secret-refresh-token", got.RefreshToken)
}

func TestTokenStorage_ConfigErrors(t *testing.T) {
	mr := miniredis.RunT(t)

	tests := map[string]struct {
		config Config
		want   string
	}{
		"unknown storage": {
			config: Config{TokenStorage: "memcached"},
			want:   "unknown token storage",
		},
		"key is not base64": {
			config: Config{TokenStorage: tokenStorageRedis, RedisAddr: mr.Addr(), TokenEncryptionKey: "not base64!"},
			want:   "not valid base64",
		},
		"key has the wrong size": {
			config: Config{TokenStorage: tokenStorageRedis, RedisAddr: mr.Addr(), TokenEncryptionKey: base64.StdEncoding.EncodeToString([]byte("short"))},
			want:   "must be 32 bytes",
		},
		"redis unreachable": {
			config: Config{TokenStorage: tokenStorageRedis, RedisAddr: "127.0.0.1:1"},
			want:   "connect to Redis",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := newTokenStorage(context.Background(), tt.config, discardLogger)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}
