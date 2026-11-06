// Package tokenstorage stores OAuth tokens of the Shopware Admin API client.
//
// The client works with the TokenStorage interface. This package provides:
//
//   - NewInMemory: tokens live in the memory of one process (the default).
//   - NewNoOp: tokens are not stored; every request fetches a new token. Not
//     for refresh-token logins (see NoOp).
//   - NewKV: tokens live in any external key-value store, such as Redis,
//     Valkey or Memcached. You implement the small KeyValueStore interface
//     with the driver you already use; this package depends on no driver.
//   - NewScoped: puts the keys of one storage under a common prefix.
//
// Tokens are credentials. Protect the store they are kept in, and consider
// encrypting them with WithCipher when you use NewKV.
//
// The guide at
// https://github.com/shopware/sdk-go/blob/main/docs/token-storage.md
// shows Redis and Valkey stores, key names, encryption and a Redis Locker.
package tokenstorage
