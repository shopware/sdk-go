// Package middleware provides optional middleware for store.ShopStore implementations.
//
// Middleware can be used to add cross-cutting concerns like retry logic, logging,
// caching, or metrics to any ShopStore implementation without modifying the store itself.
//
// # Retry Middleware
//
// The retry middleware adds automatic retry logic with exponential backoff and jitter
// for transient errors. It can wrap any ShopStore implementation:
//
//	// Create your base store
//	baseStore, err := store.NewDynamoDBStore(store.DynamoDBConfig{
//	    TableName: "shops",
//	    Region:    "us-east-1",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Wrap with retry middleware
//	storeWithRetry := middleware.NewRetryMiddleware(baseStore, middleware.RetryConfig{
//	    MaxRetries:     3,
//	    BaseDelay:      100 * time.Millisecond,
//	    MaxDelay:       20 * time.Second,
//	    RequestTimeout: 30 * time.Second,
//	})
//
//	// Use the wrapped store
//	sdk, err := app.New(app.Config{
//	    AppName:   "MyApp",
//	    AppSecret: "secret",
//	    ShopStore: storeWithRetry,
//	})
//
// The retry middleware will automatically retry operations on:
//   - DynamoDB throttling errors (ProvisionedThroughputExceededException, RequestLimitExceeded)
//   - Internal server errors
//   - Network timeouts and connection errors
//
// # Retry Configuration
//
// All retry configuration fields are optional and have sensible defaults:
//
//	type RetryConfig struct {
//	    MaxRetries     int           // Default: 3
//	    BaseDelay      time.Duration // Default: 100ms
//	    MaxDelay       time.Duration // Default: 20s
//	    RequestTimeout time.Duration // Default: 30s
//	}
//
// # Exponential Backoff with Jitter
//
// The retry middleware uses exponential backoff with jitter to avoid thundering herd:
//
//   - Attempt 0: No delay
//   - Attempt 1: ~100ms (baseDelay * 2^0 ± 10%)
//   - Attempt 2: ~200ms (baseDelay * 2^1 ± 10%)
//   - Attempt 3: ~400ms (baseDelay * 2^2 ± 10%)
//   - Capped at maxDelay
//
// # Context Cancellation
//
// The retry middleware respects context cancellation and will stop retrying
// immediately if the context is cancelled:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//
//	shop, err := storeWithRetry.GetShopByID(ctx, "shop-id")
//	// Will stop retrying if context times out
//
// # Composing Middleware
//
// Multiple middleware can be composed together:
//
//	// Start with base store
//	baseStore := store.NewInMemoryStore()
//
//	// Add retry logic
//	storeWithRetry := middleware.NewRetryMiddleware(baseStore, middleware.RetryConfig{
//	    MaxRetries: 3,
//	})
//
//	// Add logging (hypothetical)
//	storeWithLogging := middleware.NewLoggingMiddleware(storeWithRetry, logger)
//
//	// Add metrics (hypothetical)
//	storeWithMetrics := middleware.NewMetricsMiddleware(storeWithLogging, metrics)
//
// # Custom Middleware
//
// You can create custom middleware by implementing the store.ShopStore interface
// and wrapping another store:
//
//	type MyMiddleware struct {
//	    store store.ShopStore
//	}
//
//	func NewMyMiddleware(s store.ShopStore) store.ShopStore {
//	    return &MyMiddleware{store: s}
//	}
//
//	func (m *MyMiddleware) GetShopByID(ctx context.Context, shopID string) (*store.Shop, error) {
//	    // Add your custom logic before
//	    shop, err := m.store.GetShopByID(ctx, shopID)
//	    // Add your custom logic after
//	    return shop, err
//	}
//	// ... implement other methods
package middleware
