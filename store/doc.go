// Package store provides persistence interfaces and implementations for shop data.
//
// The store package defines the ShopStore interface that all storage backends
// must implement, along with built-in implementations for common use cases.
//
// # ShopStore Interface
//
// All storage backends implement this interface:
//
//	type ShopStore interface {
//	    GetShopByID(ctx context.Context, shopID string) (*Shop, error)
//	    UpdateShop(ctx context.Context, shop *Shop) error
//	    SetShopActive(ctx context.Context, shopID string, active bool) error
//	    DeleteShop(ctx context.Context, shopID string) error
//	    HealthCheck(ctx context.Context) error
//	}
//
// # Built-in Implementations
//
// ## In-Memory Store (Development/Testing)
//
//	store := store.NewInMemoryStore()
//
// Features:
//   - Fast, no external dependencies
//   - Safe for concurrent use
//   - Data lost on restart
//   - Ideal for development and testing
//
// ## SQLite Store (Small-Medium Deployments)
//
//	config := store.SQLiteConfig{
//	    DatabasePath: "/data/shops.db",
//	}
//	sqliteStore, err := store.NewSQLiteStore(config, logger)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer sqliteStore.Close()
//
// Features:
//   - Persistent storage
//   - Single-file database
//   - ACID transactions
//   - Automatic table creation
//   - Suitable for single-instance deployments
//
// ## DynamoDB Store (Production/Scalable)
//
//	config := store.DynamoDBConfig{
//	    TableName: "shopware-shops",
//	    Region:    "us-east-1",
//	    // Optional: custom endpoint for local development
//	    // Endpoint: "http://localhost:8000",
//	    // Optional: retry configuration
//	    // MaxRetries:     3,
//	    // BaseDelay:      100 * time.Millisecond,
//	    // MaxDelay:       20 * time.Second,
//	    // RequestTimeout: 30 * time.Second,
//	}
//	dynamoStore, err := store.NewDynamoDBStore(ctx, config)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// Features:
//   - Fully managed, serverless
//   - Auto-scaling
//   - Multi-region support
//   - Exponential backoff retry with jitter
//   - Suitable for production deployments
//
// # Shop Model
//
// The Shop struct represents a registered Shopware shop:
//
//	type Shop struct {
//	    ShopID       string    // Unique shop identifier (stored as "id")
//	    ShopURL      string    // Shop base URL (stored as "url")
//	    ShopSecret   string    // Secret for signature verification (stored as "secret")
//	    ClientID     string    // Shopware client ID (stored as "clientId")
//	    ClientSecret string    // Shopware client secret (stored as "clientSecret")
//	    Active       bool      // Whether the shop is active
//	    RegistrationConfirmed bool // Whether registration confirmation completed
//	    CreatedAt    time.Time // Registration timestamp
//	    UpdatedAt    time.Time // Last update timestamp
//	}
//
// # Context Support
//
// All operations support context for cancellation and timeouts:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//
//	shop, err := store.GetShop(ctx, "shop-id")
//
// # Error Handling
//
// Stores return standard errors from the errors package:
//
//	shop, err := store.GetShop(ctx, "shop-id")
//	if errors.Is(err, sdkerrors.ErrShopNotFound) {
//	    // Handle not found
//	}
//
// # Custom Implementations
//
// Implement ShopStore for your storage backend:
//
//	type RedisStore struct {
//	    client *redis.Client
//	}
//
//	func (r *RedisStore) GetShop(ctx context.Context, shopID string) (*store.Shop, error) {
//	    // your implementation
//	}
//	// ... implement other methods
//
// # Thread Safety
//
// All built-in store implementations are safe for concurrent use.
package store
