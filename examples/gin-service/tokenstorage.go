package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	adminstorage "github.com/shopware/app-sdk-go/clients/adminapi/tokenstorage"
)

const (
	tokenStorageMemory = "memory"
	tokenStorageRedis  = "redis"

	// redisPingTimeout bounds the connection check at startup.
	redisPingTimeout = 5 * time.Second

	// encryptionKeyLen is the key size of AES-256.
	encryptionKeyLen = 32
)

// redisStore implements adminstorage.KeyValueStore with go-redis. It works
// with Redis and with Valkey, which speaks the same protocol. These three
// methods are all the SDK needs; the SDK itself depends on no Redis driver.
type redisStore struct {
	rdb redis.UniversalClient
}

func (s redisStore) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := s.rdb.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, adminstorage.ErrNotFound
	}
	return value, err
}

func (s redisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return s.rdb.Set(ctx, key, value, ttl).Err()
}

func (s redisStore) Delete(ctx context.Context, key string) error {
	return s.rdb.Del(ctx, key).Err()
}

// newTokenStorage builds the Admin API token storage from the configuration.
// It returns nil for "memory", which makes the App keep tokens in memory, and
// a function that releases the storage's connections on shutdown.
func newTokenStorage(
	ctx context.Context, config Config, logger *slog.Logger,
) (adminstorage.TokenStorage, func() error, error) {
	noClose := func() error { return nil }

	switch config.TokenStorage {
	case "", tokenStorageMemory:
		logger.InfoContext(ctx, "Keeping Admin API tokens in memory")
		return nil, noClose, nil
	case tokenStorageRedis:
	default:
		return nil, noClose, fmt.Errorf("unknown token storage %q (use %q or %q)",
			config.TokenStorage, tokenStorageMemory, tokenStorageRedis)
	}

	var opts []adminstorage.KVOption
	opts = append(opts, adminstorage.WithKeyPrefix(config.TokenKeyPrefix))
	if config.TokenEncryptionKey != "" {
		c, err := newAESGCM(config.TokenEncryptionKey)
		if err != nil {
			return nil, noClose, err
		}
		opts = append(opts, adminstorage.WithCipher(c))
	}

	rdb := redis.NewClient(&redis.Options{Addr: config.RedisAddr, Password: config.RedisPassword})
	pingCtx, cancel := context.WithTimeout(ctx, redisPingTimeout)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		_ = rdb.Close()
		return nil, noClose, fmt.Errorf("connect to Redis at %s: %w", config.RedisAddr, err)
	}

	logger.InfoContext(ctx, "Keeping Admin API tokens in Redis",
		slog.String("addr", config.RedisAddr),
		slog.String("key_prefix", config.TokenKeyPrefix),
		slog.Bool("encrypted", config.TokenEncryptionKey != ""),
	)
	return adminstorage.NewKV(redisStore{rdb: rdb}, opts...), rdb.Close, nil
}

// aesGCM encrypts tokens before they are written to Redis, so a leaked dump
// of the store does not leak usable tokens.
type aesGCM struct {
	aead cipher.AEAD
}

// newAESGCM creates the cipher from a base64-encoded 32-byte key, for example
// one generated with `openssl rand -base64 32`.
func newAESGCM(encodedKey string) (*aesGCM, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, fmt.Errorf("TOKEN_ENCRYPTION_KEY is not valid base64: %w", err)
	}
	if len(key) != encryptionKeyLen {
		return nil, fmt.Errorf("TOKEN_ENCRYPTION_KEY must be %d bytes, got %d", encryptionKeyLen, len(key))
	}
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
