// Package adminapi is a client for the Shopware Admin API.
//
// The module is under active development. Until the first v1 release its API
// may change between minor versions.
//
// # Logging in
//
// Pick the credentials that match how you access the shop:
//
//   - IntegrationCredentials: client ID and secret of an app or integration.
//   - PasswordCredentials: user name and password of an admin user. After the
//     first login the client uses the refresh token and sends the password
//     again only if the refresh token is rejected.
//   - RefreshTokenCredentials: a refresh token obtained elsewhere.
//
// You can also implement Credentials yourself.
//
//	client, err := adminapi.NewClient(adminapi.Config{
//		BaseURL:     "https://shop.example.com",
//		Credentials: adminapi.IntegrationCredentials{ClientID: id, ClientSecret: secret},
//	})
//	resp, err := client.Post(ctx, "/search/product", map[string]any{"limit": 10})
//
// The client logs in on the first request, keeps the token, refreshes it
// shortly before it expires and retries a request once when the shop answers
// 401. Concurrent requests share one token fetch.
//
// # Storing tokens
//
// Tokens are kept in memory by default. Set Config.TokenStorage to share them
// between processes, for example in Redis or Valkey: implement the small
// tokenstorage.KeyValueStore interface with your driver and wrap it with
// tokenstorage.NewKV. This module depends on no storage driver.
//
// With admin login or refresh tokens and several processes, also set
// Config.Locker, so only one process refreshes a token at a time.
//
// # Paths
//
// Request paths are relative to /api. Build them with Path when they contain
// values from outside your code, so those values cannot change the path.
//
// # Errors
//
// A response with status 400 or higher is returned as *APIError with every
// error Shopware reported. IsNotFound, IsUnauthorized and IsConflict check
// common cases. Redirects are not followed and return ErrUnexpectedRedirect.
//
// # Retries, timeouts, logging
//
// The client retries only after a 401. For timeouts, retries of 429 and 5xx
// responses, proxies or instrumentation, pass your own Config.HTTPClient and
// wrap its Transport.
//
// The package does not import the app SDK (github.com/shopware/app-sdk-go):
// the dependency goes one way only, from the SDK to this module. See
// docs/adr/0003-admin-api-client-module.md in the app-sdk-go repository.
package adminapi
