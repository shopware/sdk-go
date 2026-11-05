# Admin API client

`github.com/shopware/app-sdk-go/clients/adminapi` is a Go client for the
[Shopware Admin API](https://shopware.stoplight.io/docs/admin-api).

It is a separate Go module, so it can be used without the rest of the app SDK
and its dependencies. It depends only on the standard library and
`golang.org/x/sync`, and never imports the app SDK.

## Installation

```bash
go get github.com/shopware/app-sdk-go/clients/adminapi
```

## Quick start

```go
client, err := adminapi.NewClient(adminapi.Config{
	BaseURL: "https://shop.example.com",
	Credentials: adminapi.IntegrationCredentials{
		ClientID:     "SWIA...",
		ClientSecret: "...",
	},
})
if err != nil {
	return err
}

resp, err := client.Post(ctx, "/search/product", map[string]any{"limit": 10})
if err != nil {
	return err
}

var result struct {
	Total int `json:"total"`
}
err = resp.JSON(&result)
```

The client logs in on the first request, keeps the token, refreshes it before
it expires, and retries a request once when the shop answers `401`. Many
concurrent requests share one token request.

## Logging in

| Credentials | Use it for |
|---|---|
| `IntegrationCredentials{ClientID, ClientSecret}` | Apps and integrations |
| `PasswordCredentials{Username, Password}` | Admin users. After the first login the refresh token is used; the password is sent again only if the refresh token is rejected. |
| `RefreshTokenCredentials{ClientID, RefreshToken}` | A refresh token you got elsewhere, for example from a browser login |

You can implement the `Credentials` interface for other grants, or to load
secrets lazily.

## Storing tokens

Tokens are kept in memory by default. To share them between processes, set
`Config.TokenStorage`:

| Storage | Use it for |
|---|---|
| `tokenstorage.NewInMemory()` | One process (default) |
| `tokenstorage.NewNoOp()` | No storage: a new token for every request. Tests only; with a password login the password is sent every time, and `RefreshTokenCredentials` are rejected |
| `tokenstorage.NewKV(store)` | Redis, Valkey, Memcached or any key-value store |
| `tokenstorage.NewScoped(parent, scope)` | Several apps or environments sharing one backend |

For `NewKV`, implement three methods with the driver you already use. This
module adds no driver dependency. With `go-redis`:

```go
type redisStore struct{ rdb *redis.Client }

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

client, err := adminapi.NewClient(adminapi.Config{
	BaseURL:      "https://shop.example.com",
	Credentials:  creds,
	TokenStorage: tokenstorage.NewKV(redisStore{rdb}, tokenstorage.WithKeyPrefix("myapp:token:")),
})
```

Tokens are credentials. Protect the store, and consider encrypting them with
`tokenstorage.WithCipher` (see the AES-GCM example in the package docs).

With admin login or refresh tokens and several processes, also set
`Config.Locker` so only one process refreshes a token at a time.

[Storing Admin API tokens](../../docs/token-storage.md) has the full guide: Valkey,
key names, TTL, encryption, a Redis `Locker` and security tips.

## Paths and errors

- Paths are relative to `/api`. Build them with `adminapi.Path("product", id)`
  when a segment comes from user input; each segment is escaped.
- A status of 400 or higher returns `*adminapi.APIError` with all errors
  Shopware reported. Use `adminapi.IsNotFound`, `IsUnauthorized` and
  `IsConflict` for common checks.
- A failed login (rejected credentials, unreachable token endpoint, invalid token
  response) returns `*adminapi.TokenError`; check it with `errors.As`.
- Redirects are not followed and return `adminapi.ErrUnexpectedRedirect`. This
  usually means a wrong `BaseURL`, for example `http` instead of `https`.

## Retries and timeouts

The client retries only after a `401`. For timeouts, retries of `429` and `5xx`,
proxies or instrumentation, pass your own `Config.HTTPClient` and wrap its
`Transport`.

## Roadmap

The app SDK uses this client since core `v1.3.0` (`App.NewClient`). Planned next: DAL
helpers (criteria, repositories, sync), the extension manager, media upload and a
browser login for CLI tools. See [ADR 0003](../../docs/adr/0003-admin-api-client-module.md).

The code is based on the archived
[`shopwareLabs/go-shopware-http-client`](https://github.com/shopwareLabs/go-shopware-http-client),
with the agreement of its owner.

## License

MIT, see [LICENSE](LICENSE). Parts of the code come from
`go-shopware-http-client` by Friends of Shopware.
