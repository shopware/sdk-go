# Agent Guidelines for Shopware SDK for Go

This document provides comprehensive guidelines for maintaining and developing the Shopware SDK for Go. Follow these patterns and best practices to ensure consistency, quality, and maintainability.

## Table of Contents

1. [Project Overview](#project-overview)
2. [Architecture Principles](#architecture-principles)
3. [Code Organization](#code-organization)
4. [Naming Conventions](#naming-conventions)
5. [Error Handling](#error-handling)
6. [Logging](#logging)
7. [Testing Standards](#testing-standards)
8. [Documentation](#documentation)
9. [Type Safety](#type-safety)
10. [Security Best Practices](#security-best-practices)
11. [Performance Considerations](#performance-considerations)
12. [Git Workflow](#git-workflow)

---

## Project Overview

The Shopware SDK for Go is a library that simplifies building Shopware apps and talking to the Shopware Admin API in Go. It provides:

- **Registration flow**: Handle app registration with Shopware stores
- **Webhook handling**: Verify and process webhooks from Shopware
- **API Client**: Authenticated HTTP client for Shopware Admin API with optional token caching
- **Store abstraction**: Multiple persistence backends (in-memory, SQLite, DynamoDB)
- **Framework integrations**: Gin web framework support
- **Optional middleware**: Retry logic, logging, and caching as opt-in features

**Key Design Goals**:
1. **Minimal & Unopinionated**: The SDK core does NOT force logging, retry logic, or caching on users
2. **Flexible**: Developers can add features via middleware and interfaces
3. **Non-prescriptive**: Integrates into existing applications without imposing patterns

---

## Architecture Principles

### 1. Interface-First Design

- Define interfaces before implementations
- Keep interfaces minimal and focused
- Use dependency injection for flexibility

**Example**: `ShopStore` interface allows multiple storage backends:

```go
type ShopStore interface {
    GetShopByID(ctx context.Context, shopID string) (*Shop, error)
    UpdateShop(ctx context.Context, shop *Shop) error
    SetShopActive(ctx context.Context, shopID string, active bool) error
    DeleteShop(ctx context.Context, shopID string) error
    HealthCheck(ctx context.Context) error
}
```

### 2. Separation of Concerns

- **Package structure**: Each package has a single, clear responsibility
- **Store implementations**: Separated into individual files (`memory.go`, `sqlite.go`, `dynamodb.go`)
- **Configuration**: Separated into implementation-specific files (`SQLiteConfig`, `DynamoDBConfig`)

### 3. Context-First APIs

- Always accept `context.Context` as the first parameter
- Respect context cancellation in all operations
- Use context for request-scoped values (e.g., trace IDs)

### 4. No Panics in Library Code

- **Never** use `panic()` in library code
- Return errors explicitly
- Only panic in examples if it's acceptable for demo code

**Bad**:
```go
func NewApp(cfg Config) *App {
    if err := cfg.Validate(); err != nil {
        panic(err) // ❌ Never panic in library code
    }
}
```

**Good**:
```go
func NewApp(cfg Config) (*App, error) {
    if err := cfg.Validate(); err != nil {
        return nil, fmt.Errorf("invalid config: %w", err)
    }
    return &App{config: cfg}, nil
}
```

### 5. Optional Features via Middleware and Interfaces

The SDK provides optional features that users can opt into:

#### Retry Middleware

Store retry logic is NOT built-in. Users can wrap any store with retry middleware:

```go
import "github.com/shopware/sdk-go/store/middleware"

baseStore, _ := store.NewDynamoDBStore(ctx, store.DynamoDBConfig{...})
storeWithRetry := middleware.NewRetryMiddleware(baseStore, middleware.RetryConfig{
    MaxRetries: 3,
    BaseDelay:  100 * time.Millisecond,
})
```

#### Token Caching

The App shares one token per shop between all Admin API clients it creates. Tokens
live in memory by default; set `app.Config.TokenStorage` to share them between
instances (Redis, Valkey, ...) without adding a driver dependency to the SDK:

```go
import adminstorage "github.com/shopware/sdk-go/clients/adminapi/tokenstorage"

sdk, err := app.New(app.Config{
    // ...
    TokenStorage: adminstorage.NewKV(myKeyValueStore), // implements Get/Set/Delete
})
client, err := sdk.NewClient(ctx, shopID) // *adminapi.Client
```

The Admin API client lives in its own module, `clients/adminapi`, and never imports the
core. `sdk.NewClient` / `sdk.NewClientForShop` return `*adminapi.Client`.

#### Logging

The SDK core does NOT log. Integrations (like Gin) provide optional logging:

```go
// No logger (default)
ginintegration.Configure(router, ginintegration.Config{...})

// With logger
ginintegration.Configure(router, ginintegration.Config{
    Logger: myLogger, // Optional
})
```

**When adding new features, ask: "Should this be built-in or optional?"**
- If it's a cross-cutting concern (logging, retry, caching) → Make it optional
- If it's core functionality (signature verification) → Built-in

---

## Code Organization

### Package Structure

```
.                     # Core module: github.com/shopware/sdk-go
├── app/              # Core SDK orchestration
├── internal/         # Internal utilities (not exported)
│   └── testutil/     # Shared test utilities
├── registration/     # App registration flow
├── resolver/         # Shop context resolution
├── response/         # HTTP response builders
├── sdkerrors/        # Custom error types
├── signature/        # Signature verification utilities
├── store/            # Persistence abstraction
│   └── middleware/   # Optional store middleware (retry, logging, etc.)
├── webhook/          # Webhook handling
├── clients/adminapi/ # Admin API client (separate module)
└── integrations/gin/ # Gin integration (separate module)
```

### File Naming

- **Implementation files**: `memory.go`, `sqlite.go`, `dynamodb.go`
- **Test files**: `*_test.go`
- **Benchmark files**: `*_bench_test.go`
- **Documentation**: `doc.go` in each package
- **README**: Per-package `README.md` for complex packages

### Configuration Patterns

- **Separate configs by implementation**: `SQLiteConfig` in `sqlite.go`, `DynamoDBConfig` in `dynamodb.go`
- **Provide sensible defaults**: Use zero values where appropriate, apply defaults in constructors
- **Validate early**: Validate configuration in constructors, not at usage time

**Example**:
```go
// In sqlite.go
type SQLiteConfig struct {
    DatabasePath string
}

func NewSQLiteStore(config SQLiteConfig) (*SQLiteStore, error) {
    // Apply defaults
    if config.DatabasePath == "" {
        config.DatabasePath = "./data/shops.db"
    }
    
    // Validate and create
    // ...
}
```

---

## Naming Conventions

### Methods and Functions

- **Use descriptive names**: `GetShopByID` not `GetShop`
- **Use verbs**: `UpdateShop`, `DeleteShop`, `VerifySignature`
- **Be specific**: `SetShopActive` not `SetActive`
- **Upsert operations**: Use `Update` prefix (e.g., `UpdateShop` for insert-or-update)

### Types

- **Use pseudotypes for enumerations**: Define custom string types for better type safety

**Good**:
```go
type Shop struct {
    ShopID       string
    ShopURL      string
    Active       bool      // Simple boolean for active/inactive state
    // ...
}
```

**Bad**:
```go
type Shop struct {
    ShopID       string
    ShopURL      string
    Status       string    // Ambiguous string type
    // ...
}
```

### Variables

- Use short names for receivers: `s *SQLiteStore`, `m *InMemoryStore`
- Use descriptive names for function parameters: `shopID string`, `active bool`
- Avoid abbreviations unless universally understood: `ctx` (context), `err` (error)

---

## Error Handling

### Custom Error Types

Use custom error types from `sdkerrors` for domain-specific errors:

```go
import "github.com/shopware/sdk-go/sdkerrors"

if !shop.Active {
    return nil, sdkerrors.NewShopNotActive(shop.ShopID)
}
```

### Error Wrapping

Always wrap errors with context:

```go
if err := s.db.ExecContext(ctx, query, args...); err != nil {
    return fmt.Errorf("failed to store shop: %w", err)
}
```

### Error Checking

- Check errors immediately
- Don't ignore errors (use `_` only when intentional)
- Return errors early

---

## Logging

### Core Library: No Logging

**The SDK core library does NOT log**. This is intentional to keep the library minimal and unopinionated.

- **Store implementations** (memory, SQLite, DynamoDB): No logging
- **Registration handler**: No logging
- **Webhook middleware**: No logging
- **Resolver**: No logging
- **Client**: No logging

Users can add logging in their own application code.

### Integrations: Optional Logging

Framework integrations (e.g., Gin) MAY provide optional logging:

```go
// Gin integration with optional logger
ginintegration.Configure(router, ginintegration.Config{
    AppName:   "MyApp",
    AppSecret: "secret",
    ShopStore: store,
    Logger:    log, // Optional - defaults to no-op logger
})
```

If `Logger` is not provided, the integration uses a no-op logger.

### Logging in Integrations

The core SDK does not log (see [ADR 0002](docs/adr/0002-no-logging-in-core.md)). If
you're implementing an integration that needs logging, accept a stdlib `*slog.Logger`:

```go
import "log/slog"

type MyIntegration struct {
    logger *slog.Logger
}

func New(cfg Config) *MyIntegration {
    log := cfg.Logger
    if log == nil {
        log = slog.New(slog.DiscardHandler) // Default to no-op
    }
    return &MyIntegration{logger: log}
}
```

### Sensitive Data

**Never log sensitive information** (if you add logging in your application):
- API keys, secrets, tokens
- Shop secrets
- Full request/response bodies (may contain sensitive data)

---

## Testing Standards

### Assertion library: testify only

Use `github.com/stretchr/testify` (`assert` and `require`) for **all** assertions. Never mix raw `t.Errorf`/`t.Fatalf`/`t.Error` with testify in the same file.

- **`require.*`** — for preconditions and setup steps that must succeed for the rest of the test to be meaningful. A `require` failure stops the test immediately.
- **`assert.*`** — for the actual assertions under test. An `assert` failure records the error but lets the test continue so you see all failures at once.

```go
// Setup — use require (fatal on failure)
require.NoError(t, shopStore.UpdateShop(ctx, testShop))

// Assertions — use assert (continue on failure)
assert.Equal(t, expected.ShopID, result.ShopID)
assert.True(t, result.Active)
```

### Never discard errors in tests

Every function that returns an error must have that error checked. Never write `_, _ :=` or `_ =` for error returns in test code.

```go
// Bad
regResp, _ := handler.HandleRegistration(ctx, req)
shopStore.UpdateShop(ctx, shop) // return value discarded

// Good
regResp, err := handler.HandleRegistration(ctx, req)
require.NoError(t, err)

err := shopStore.UpdateShop(ctx, shop)
require.NoError(t, err)
```

### Table-driven tests with `t.Parallel()`

Group related scenarios into table-driven tests. Add `t.Parallel()` to both the parent test and each subtest when the subtests don't share mutable state.

```go
func TestResolveFromQuery_ErrorPaths(t *testing.T) {
    t.Parallel()

    tests := []struct {
        name        string
        buildReq    func() *http.Request
        errContains string
    }{
        {
            name: "missing shop-id",
            buildReq: func() *http.Request {
                return httptest.NewRequest(http.MethodGet, "/test?shopware-shop-signature=x", nil)
            },
            errContains: "shop-id",
        },
        // ...more cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            // ...test body
        })
    }
}
```

**When NOT to use `t.Parallel()`**: tests sharing mutable state (store suites, mock servers, shared counters).

### Shared test helpers (`internal/testutil`)

Reuse the helpers in `testutil/` instead of reimplementing patterns in each test file.

| Helper | Purpose |
|---|---|
| `testutil.NewTestShop(opts...)` | Create a `*store.Shop` with functional options (`WithShopID`, `WithShopURL`, `WithShopSecret`, etc.) |
| `testutil.NewTestWebhookEvent(opts...)` | Create a `*webhook.Event` with functional options |
| `testutil.AssertSDKError(t, err, expectedType)` | Assert error is `*sdkerrors.Error` with the given type; returns the error for further assertions on `.Message`, `.Code` |
| `testutil.NewSignedRequest(method, url, body, secret)` | Build an `*http.Request` with a valid HMAC signature header |
| `testutil.NewSignedQueryRequest(url, query, secret)` | Build a GET `*http.Request` with a signed query string |
| `testutil.MockShopStore` | Testify-mock implementation of `store.ShopStore` with compile-time interface check |
| `testutil.NewFailingStore(err)` | Returns a `MockShopStore` pre-configured to return `err` on every method |

When a pattern is repeated 3+ times across test files, extract it into `testutil/` rather than duplicating it.

### Mock implementations

Use `testutil.MockShopStore` (backed by `testify/mock`) for store mocks. Do not define per-file mock structs that duplicate the `ShopStore` interface.

```go
mockStore := new(testutil.MockShopStore)
mockStore.On("GetShopByID", mock.Anything, "shop-123").Return(existingShop, nil)
mockStore.On("UpdateShop", mock.Anything, mock.AnythingOfType("*store.Shop")).Return(nil)

// ...run test...

mockStore.AssertExpectations(t)
```

For tests that need every store method to fail:

```go
shopStore := testutil.NewFailingStore(errors.New("connection refused"))
```

### Setup helpers in test files

When a test file has a repeated setup block (e.g., creating a resolver with store + verifier + middleware), extract a file-level helper that calls `t.Helper()`:

```go
func newTestResolver(t *testing.T, inflightAllowance ...int) (*resolver.Resolver, store.ShopStore) {
    t.Helper()
    allowance := 0
    if len(inflightAllowance) > 0 {
        allowance = inflightAllowance[0]
    }

    shopStore := store.NewInMemoryStore()

    return resolver.NewResolver(resolver.Config{
        ShopStore:                shopStore,
        WebhookVerifier:          webhook.NewMiddleware(webhook.Config{ShopStore: shopStore}),
        InflightAllowanceSeconds: allowance,
    }), shopStore
}
```

### External vs internal test packages

- **External** (`package foo_test`): Prefer this for tests that use only exported API. It validates the public surface and prevents accidental coupling to internals.
- **Internal** (`package foo`): Use only when testing unexported functions or types.

If a file has both, split into two files (e.g., `verifier_test.go` for internal, `verifier_external_test.go` for external).

### Assertions inside goroutines

Never call `assert.*` or `require.*` inside a goroutine — `t.Fatal*` from a non-test goroutine causes undefined behavior. Collect errors via a channel and assert after `wg.Wait()`:

```go
errs := make(chan error, n)

for i := range n {
    wg.Add(1)
    go func(index int) {
        defer wg.Done()
        _, err := c.Get(ctx, fmt.Sprintf("/product/%d", index))
        if err != nil {
            errs <- err
        }
    }(i)
}

wg.Wait()
close(errs)

for err := range errs {
    require.NoError(t, err)
}
```

### SDK error assertions

When testing for specific SDK error types, use `testutil.AssertSDKError` instead of manually unwrapping:

```go
// Bad — repeated boilerplate
var sdkErr *sdkerrors.Error
require.ErrorAs(t, err, &sdkErr)
assert.Equal(t, sdkerrors.TypeInvalidRequest, sdkErr.Type)

// Good — one-liner, returns the error for further checks
sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidRequest)
assert.Contains(t, sdkErr.Message, "shop-id")
```

### Test file organization

- **Unit tests**: `*_test.go`
- **Benchmarks**: `*_bench_test.go` (separate file, same package as the tests they benchmark)
- **Test suites**: Use `testify/suite` for shared setup/teardown (e.g., store conformance tests)

### Test coverage

- Test error paths, not just happy paths.
- Test edge cases and boundary conditions.
- Coverage should not decrease in PRs (enforced by CI).

### Benchmarks

- Name benchmarks: `BenchmarkPackage_Operation`
- Reset timer after setup: `b.ResetTimer()`
- Use realistic data sizes

---

## Documentation

### Package Documentation

Every package must have a `doc.go` file:

```go
// Package store provides persistence abstractions for shop data.
//
// The store package defines the ShopStore interface and provides three
// implementations:
//   - InMemoryStore: For development and testing
//   - SQLiteStore: For single-instance deployments
//   - DynamoDBStore: For distributed, high-availability deployments
//
// Example usage:
//
//     config := store.SQLiteConfig{
//         DatabasePath: "./data/shops.db",
//     }
//     store, err := store.NewSQLiteStore(config, logger)
//     if err != nil {
//         log.Fatal(err)
//     }
//     defer store.Close()
package store
```

### Function Documentation

- Document all exported functions, types, and constants
- Start with the name of what you're documenting
- Include examples for complex APIs
- Document error conditions

**Example**:
```go
// GetShopByID retrieves a shop by its unique identifier.
//
// Returns an error if the shop is not found or if the database
// operation fails.
func (s *SQLiteStore) GetShopByID(ctx context.Context, shopID string) (*Shop, error) {
    // ...
}
```

### Examples

- Provide runnable examples in `doc.go` or separate `example_test.go` files
- Show error handling patterns
- Demonstrate common use cases

---

## Type Safety

### Boolean Fields for Simple States

Use boolean fields for simple active/inactive states:

```go
type Shop struct {
    ShopID       string
    ShopURL      string
    Active       bool      // Simple boolean - true for active, false for inactive
    // ...
}
```

**Benefits**:
- Simple and clear semantics
- No ambiguity about valid values
- Easy to check: `if shop.Active { ... }`
- Works well with database INTEGER columns (0/1)

### Type Usage

When working with shop active state:

```go
// Checking if shop is active
if !shop.Active {
    return nil, sdkerrors.NewShopNotActive(shop.ShopID)
}

// Setting shop active state
shop.Active = true
```

---

## Security Best Practices

### Signature Verification

- Always verify signatures before processing webhooks
- Use constant-time comparison for signatures
- Remove signature from query string before verification

### CORS Configuration

- Make CORS configurable, not hardcoded
- Validate origins against allowlist
- Default to restrictive settings

**Example**:
```go
type Config struct {
    CORSAllowedOrigins []string // e.g., ["https://app.example.com"]
}
```

### Defensive Programming

- Validate all inputs
- Sanitize paths (e.g., ensure leading `/` in API paths)
- Check for null bytes in sensitive strings

**Example**:
```go
// In API client
func (c *Client) Request(ctx context.Context, method, path string, body interface{}) (*Response, error) {
    if !strings.HasPrefix(path, "/") {
        path = "/" + path  // Defensive: ensure leading slash
    }
    // ...
}
```

### Token Management

- Implement proactive token refresh (before expiry)
- Implement reactive token refresh (retry on 401)
- Never expose tokens in logs

---

## Performance Considerations

### Concurrency

- Use `sync.RWMutex` for read-heavy operations
- Avoid goroutine leaks: always use `defer timer.Stop()` with timers
- Use channels for coordination, not shared memory

**Example**:
```go
// Good: Use timer correctly
timer := time.NewTimer(delay)
defer timer.Stop()

select {
case <-timer.C:
    // Handle timeout
case <-ctx.Done():
    return ctx.Err()
}

// Bad: Potential goroutine leak
select {
case <-time.After(delay):  // ❌ Cannot be stopped
    // Handle timeout
case <-ctx.Done():
    return ctx.Err()
}
```

### Database Operations

- Use connection pooling
- Use prepared statements where appropriate
- Add indexes for common queries
- Use WAL mode for SQLite

### Retry Logic

- Implement exponential backoff with jitter
- Make retry configuration tunable
- Log retry attempts for debugging

**Example**:
```go
delay := float64(baseDelay) * math.Pow(2, float64(attempt))
jitter := delay * 0.1 * (rand.Float64()*2 - 1)
finalDelay := time.Duration(delay + jitter)
```

---

## Git Workflow

### Commit Messages

Follow conventional commits format:

```
<type>(<scope>): <subject>

<body>

<footer>
```

**Types**:
- `feat`: New feature
- `fix`: Bug fix
- `refactor`: Code refactoring
- `docs`: Documentation changes
- `test`: Test additions or changes
- `chore`: Maintenance tasks

**Example**:
```
refactor(store): rename GetShop to GetShopByID and StoreShop to UpdateShop

Improve method naming clarity in ShopStore interface for better API semantics.

Changes:
- Rename GetShop() to GetShopByID() - more explicit about lookup method
- Rename StoreShop() to UpdateShop() - better reflects upsert behavior
- Update all implementations (memory, sqlite, dynamodb)
- Update all usages across packages

Benefits:
- GetShopByID: Clearer that lookup is by ID, not by other fields
- UpdateShop: Better conveys that it performs upsert (insert or update)
- More consistent with common naming conventions in Go APIs
```

### Commit Guidelines

- **One logical change per commit**: Don't mix refactoring with feature additions
- **Test before committing**: Ensure all tests pass and linting is clean
- **Don't amend pushed commits**: Once pushed, create new commits
- **Never force push to main/master**: Coordinate with team if needed

### Branch Naming

- Feature branches: `feat/feature-name`
- Bug fixes: `fix/bug-description`
- Refactoring: `refactor/what-is-refactored`

---

## Linting and Code Quality

### golangci-lint Configuration

The project uses a comprehensive `golangci-lint` configuration with 69+ linters enabled. Always run linting before committing:

```bash
golangci-lint run --timeout=5m
```

### Common Linting Issues

1. **Magic numbers (`mnd`)**: Extract to named constants
   ```go
   // Bad
   time.Sleep(30 * time.Second)
   
   // Good
   const defaultTimeout = 30 * time.Second
   time.Sleep(defaultTimeout)
   ```

2. **Error checking (`errcheck`)**: Always check returned errors
   ```go
   // Bad
   db.Close()
   
   // Good
   if err := db.Close(); err != nil {
       logger.Error("failed to close database", logger.Error(err))
   }
   ```

3. **Exit after defer (`gocritic`)**: Move `os.Exit` outside deferred functions
   ```go
   // Bad
   func main() {
       defer cleanup()
       os.Exit(1)  // defer won't run
   }
   
   // Good
   func main() {
       if err := run(); err != nil {
           log.Fatal(err)
       }
   }
   
   func run() error {
       defer cleanup()
       // ...
       return nil
   }
   ```

---

## CI/CD

### GitHub Actions Workflow

The project uses GitHub Actions for CI/CD with the following jobs:

1. **Lint**: Run `golangci-lint` with all enabled linters
2. **Test**: Run tests with coverage and race detection
3. **Test Benchmarks**: Verify benchmarks compile and run
4. **Build**: Verify the project builds

### Coverage Requirements

- Coverage should not decrease in PRs
- Baseline is computed from the base branch
- Coverage comparison only runs on PRs

---

## Examples

### Writing Good Examples

Examples should:
- Be realistic and practical
- Show proper error handling
- Include all necessary imports
- Be runnable

**Example**:
```go
package main

import (
    "log"

    "github.com/shopware/sdk-go/app"
    "github.com/shopware/sdk-go/store"
)

func main() {
    // Initialize store
    storeConfig := store.SQLiteConfig{
        DatabasePath: "./data/shops.db",
    }
    shopStore, err := store.NewSQLiteStore(storeConfig)
    if err != nil {
        log.Fatal("Failed to create store", err)
    }
    defer shopStore.Close()

    // Create SDK instance (the core SDK takes no logger; it returns typed errors)
    appConfig := app.Config{
        AppName:         "MyApp",
        AppSecret:       "my-secret",
        ConfirmationURL: "https://myapp.com/confirm",
        ShopStore:       shopStore,
    }

    sdk, err := app.New(appConfig)
    if err != nil {
        log.Fatal("Failed to create SDK", err)
    }

    // Use SDK...
}
```

---

## Review Checklist

Before submitting code for review, verify:

- [ ] All tests pass (`go test ./... -race -count=1`)
- [ ] Linting passes (`golangci-lint run ./...`)
- [ ] Code is formatted (`go fmt ./...`)
- [ ] Documentation is updated
- [ ] No panics in library code
- [ ] Errors are properly wrapped
- [ ] Sensitive data is not logged
- [ ] Custom types are used for enumerations
- [ ] Context is respected
- [ ] Commit messages follow conventions
- [ ] Examples are runnable and demonstrate proper error handling
- [ ] New tests use testify exclusively (no raw `t.Errorf`/`t.Fatalf`)
- [ ] No errors discarded in test code (`_, _ :=`)
- [ ] Shared helpers from `testutil/` are used where applicable
- [ ] Table-driven subtests include `t.Parallel()` when safe

---

## Common Pitfalls to Avoid

1. **Using `panic()` in library code**: Always return errors
2. **Ignoring context cancellation**: Always check `ctx.Err()`
3. **Hardcoding configuration**: Make everything configurable
4. **Logging secrets**: Never log sensitive information
5. **Mixing concerns**: Keep packages focused
6. **Not using interfaces**: Prefer interfaces for flexibility
7. **Forgetting to stop timers**: Always `defer timer.Stop()`
8. **Force pushing**: Don't rewrite pushed history
9. **Large commits**: Keep commits focused and atomic
10. **Missing tests**: Test error paths, not just happy paths
11. **Mixing raw `t.Errorf` with testify**: Pick one — we use testify exclusively
12. **Discarding errors in tests** (`_, _ :=`): Always check errors with `require.NoError`
13. **Duplicating testutil helpers**: Check `internal/testutil/` before writing a new fixture or assertion helper
14. **Defining per-file mock stores**: Use `testutil.MockShopStore` instead
15. **Calling `assert`/`require` inside goroutines**: Collect errors via channel, assert after `wg.Wait()`

---

## Questions?

When in doubt:
1. Check existing code for patterns
2. Consult Go best practices (Effective Go, Go Code Review Comments)
3. Follow the principle of least surprise
4. Prioritize clarity over cleverness
5. Document your reasoning in comments or commit messages

---

**Last Updated**: 2026-03-10  
**Maintainer**: Shopware SDK Team

