# Storing Admin API Tokens

The Admin API client logs in to a shop and gets an OAuth token. It keeps that token in a
`tokenstorage.TokenStorage` and reuses it until shortly before it expires. This page
explains where to keep tokens and how to connect Redis or Valkey.

The SDK depends on no storage driver. You use the driver you already have and connect it
with three small methods.

## Memory Or An External Store

| Setup | Storage |
| --- | --- |
| One instance of your app | Memory, the default. Nothing to configure. |
| Several instances, or frequent restarts | An external store such as Redis or Valkey, so all instances share one token per shop. |
| Tests | Memory, or `tokenstorage.NewNoOp()` to fetch a new token for every request. Not for `RefreshTokenCredentials`: `adminapi.NewClient` rejects that combination, also when `NoOp` is wrapped in `NewScoped`. |

Memory is safe with several instances using `IntegrationCredentials` or
`PasswordCredentials`: each instance can log in independently. Instances using the same
`RefreshTokenCredentials` need shared persistent storage and a `Locker`, because the
first exchange revokes the initial refresh token. External storage also keeps rotated
tokens across restarts.

Set the storage once:

```go
// With the app system: all shops share this storage.
sdk, err := app.New(app.Config{
    // ...
    TokenStorage: storage,
})

// Without the app system: pass it to every client.
client, err := adminapi.NewClient(adminapi.Config{
    // ...
    TokenStorage: storage,
})
```

## Redis

With [go-redis](https://github.com/redis/go-redis), implement
`tokenstorage.KeyValueStore` and wrap it with `tokenstorage.NewKV`:

```go
import (
    "github.com/redis/go-redis/v9"
    "github.com/shopware/app-sdk-go/clients/adminapi/tokenstorage"
)

type redisStore struct{ rdb redis.UniversalClient }

func (s redisStore) Get(ctx context.Context, key string) ([]byte, error) {
    v, err := s.rdb.Get(ctx, key).Bytes()
    if errors.Is(err, redis.Nil) {
        return nil, tokenstorage.ErrNotFound
    }
    return v, err
}

func (s redisStore) Set(ctx context.Context, key string, v []byte, ttl time.Duration) error {
    return s.rdb.Set(ctx, key, v, ttl).Err()
}

func (s redisStore) Delete(ctx context.Context, key string) error {
    return s.rdb.Del(ctx, key).Err()
}

storage := tokenstorage.NewKV(redisStore{rdb}, tokenstorage.WithKeyPrefix("myapp:token:"))
```

`Get` must return `tokenstorage.ErrNotFound` for a missing key. Any other error is
passed on to the caller.

## Valkey

Valkey speaks the Redis protocol, so the go-redis code above works with Valkey without
changes. With the [valkey-go](https://github.com/valkey-io/valkey-go) driver:

```go
import (
    "github.com/valkey-io/valkey-go"
    "github.com/shopware/app-sdk-go/clients/adminapi/tokenstorage"
)

type valkeyStore struct{ client valkey.Client }

func (s valkeyStore) Get(ctx context.Context, key string) ([]byte, error) {
    v, err := s.client.Do(ctx, s.client.B().Get().Key(key).Build()).AsBytes()
    if valkey.IsValkeyNil(err) {
        return nil, tokenstorage.ErrNotFound
    }
    return v, err
}

func (s valkeyStore) Set(ctx context.Context, key string, v []byte, ttl time.Duration) error {
    cmd := s.client.B().Set().Key(key).Value(valkey.BinaryString(v)).Px(ttl).Build()
    return s.client.Do(ctx, cmd).Error()
}

func (s valkeyStore) Delete(ctx context.Context, key string) error {
    return s.client.Do(ctx, s.client.B().Del().Key(key).Build()).Error()
}

storage := tokenstorage.NewKV(valkeyStore{client}, tokenstorage.WithKeyPrefix("myapp:token:"))
```

Any other key-value store with expiring keys, such as Memcached, works the same way.

A full working example with Redis, Valkey and encryption is
[examples/gin-service](../examples/gin-service/README.md) (`TOKEN_STORAGE=redis`).

## Key Names

Every key `NewKV` writes is `prefix + token key`.

- **Prefix.** The default is `shopware:adminapi:token:`. Give each app and each
  environment its own prefix with `tokenstorage.WithKeyPrefix("myapp:prod:token:")`,
  so two apps on one Redis never read each other's tokens.
- **Token key with the app system.** The SDK uses `<shop ID>:<hash of the client ID>`.
  When a shop registers again with new credentials, the SDK deletes the token of the old
  credentials as soon as the new registration is confirmed. This also covers a changed
  client secret with the same client ID.
- **Token key without the app system.** `adminapi.NewClient` builds the key from a hash of
  the shop URL and the user or integration, unless you set `Config.TokenStorageKey`.
  Different shops and users never share a key, and the key does not reveal user names.
- **One storage for several parts of your code.** Wrap it with
  `tokenstorage.NewScoped(storage, "importer")` to put all keys of one part under a
  separate scope.

## How Long Tokens Are Kept

`NewKV` sets a TTL on every key, so old tokens remove themselves:

- A token **with** a refresh token (admin login) is kept for 7 days, the lifetime of a
  Shopware refresh token. Change it with `tokenstorage.WithRefreshTokenTTL`.
- A token **without** a refresh token (integrations and apps) is kept until its access
  token expires, plus one minute.

With `IntegrationCredentials` or `PasswordCredentials`, an evicted key causes a new
login on the next request. With `RefreshTokenCredentials`, eviction can lose the only
usable rotated refresh token; the initial token may already be revoked, requiring a
new external login. Keep these tokens in durable storage protected from eviction
and data loss.

When an app is removed from a shop, delete the shop's token at once with
`sdk.ClearShopToken(ctx, shop)`. The Gin integration does this for you. Tokens of old
credentials are deleted automatically when a shop registers again.

## Encrypting Tokens

`NewKV` can encrypt tokens before they are written, so a copy of the store does not leak
working tokens. Pass any `tokenstorage.Cipher`. AES-GCM from the standard library is
enough:

```go
type aesGCM struct{ aead cipher.AEAD }

func newAESGCM(key []byte) (*aesGCM, error) { // key: 32 bytes for AES-256
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

storage := tokenstorage.NewKV(redisStore{rdb},
    tokenstorage.WithKeyPrefix("myapp:token:"),
    tokenstorage.WithCipher(c),
)
```

- Generate a base64-encoded key with `openssl rand -base64 32`, load it from your secret
   manager or an environment variable, and decode it with `base64.StdEncoding.DecodeString`
   before passing the resulting 32 bytes to `newAESGCM`. Never store the key in code.
- **Rotate keys deliberately.** A single-key cipher treats values encrypted with another key
   as missing; the next token the client fetches overwrites them. `NewKV` does not delete such
   values, because another instance may have just stored a fresh token under the same key.
   Update all instances together, or during a rolling rotation use a `Cipher` that decrypts
   with both old and new keys while encrypting only with the new key.

## One Refresh At A Time

A client makes sure only one of its requests fetches a token at a time. The `App` goes
further: all clients it creates for a shop share one lock, so clients that start at the
same time in one process fetch one token per shop. Across processes you can add a
`tokenstorage.Locker`.

**For apps and integrations (`IntegrationCredentials`) a Locker is optional.** Without
it, two instances that fetch a token at the same moment both get one; both tokens work,
and the last one stored wins. Set `app.Config.Locker` if you want exactly one token per
shop across all instances, for example to keep the number of token requests low:

```go
sdk, err := app.New(app.Config{
    // ...
    TokenStorage: storage,
    Locker:       redisLocker{rdb: rdb, ttl: time.Minute}, // see below
})
```

**A Locker helps more** with admin login (`PasswordCredentials`) or `RefreshTokenCredentials`
when several processes share a storage. Shopware gives out a new refresh token on every
refresh and revokes the old one. Without a lock two processes may refresh with the same
refresh token, and one of them is rejected. The client recovers from that: it reads the
newer token from the storage or logs in again. With `RefreshTokenCredentials` there is
no password to log in again with, so use a Locker there.

A Locker with go-redis (`SET NX PX`):

```go
// unlockScript deletes the lock only if this process still holds it.
var unlockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0`)

type redisLocker struct {
    rdb redis.UniversalClient
    ttl time.Duration // longer than one token request, for example 1 minute
}

func (l redisLocker) Lock(ctx context.Context, key string) (func(), error) {
    key = "myapp:token-lock:" + key
    owner := rand.Text() // random, so we never release someone else's lock
    for {
        ok, err := l.rdb.SetNX(ctx, key, owner, l.ttl).Result()
        if err != nil {
            return nil, err
        }
        if ok {
            return func() {
                _ = unlockScript.Run(context.WithoutCancel(ctx), l.rdb, []string{key}, owner).Err()
            }, nil
        }
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-time.After(100 * time.Millisecond): // another process is refreshing
        }
    }
}

// Without the app system:
client, err := adminapi.NewClient(adminapi.Config{
    // ...
    TokenStorage: storage,
    Locker:       redisLocker{rdb: rdb, ttl: time.Minute},
})
```

After it gets the lock, the client reads the storage again, so it uses the token the
other process just stored instead of refreshing a second time.

The same lock covers every change of the stored token, not only refreshes: dropping the
access token after a `401`, `InvalidateToken`, `SetToken` and `ClearToken`. None of them
can write a stale token over one another request has just stored; with a Locker this also
holds across processes. For example,
a `SetToken` with a token from a browser login waits for a running refresh, so the refresh
cannot overwrite it afterwards.

If a refresh succeeds but the storage cannot save the new token (for example, Redis is
briefly down), the client keeps that token in memory and stores it on the next attempt
instead of exchanging the old refresh token again, which Shopware has already revoked. The
storage error is still returned. The token kept in memory is lost if the process restarts
before the storage recovers.

## Security Tips

A token works like a password for the shop's Admin API. Protect the store like a
password database:

- Keep Redis or Valkey in a private network; never expose it to the internet.
- Turn on authentication (a password or ACL user) and TLS between your app and the store.
- Use your own prefix or database for tokens, and do not share the store with untrusted
  services.
- Encrypt tokens with `WithCipher`, and keep the encryption key in a different place
  than the store.
- Never log token values. The SDK does not.
- Delete a shop's token when the app is removed (`sdk.ClearShopToken`; the Gin
  integration does it for you).
