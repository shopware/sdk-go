package main

//nolint:gci // Import grouping intentionally simplified for example clarity
import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shopware/sdk-go/app"
	"github.com/shopware/sdk-go/clients/adminapi"
	ginintegration "github.com/shopware/sdk-go/integrations/gin"
	"github.com/shopware/sdk-go/store"
	"github.com/shopware/sdk-go/webhook"
)

const (
	maxBodyBytes    = 5 << 20 // 5 MiB
	readTimeout     = 15 * time.Second
	writeTimeout    = 15 * time.Second
	idleTimeout     = 60 * time.Second
	shutdownTimeout = 30 * time.Second

	// apiTimeout bounds the Admin API calls made after a webhook was answered.
	apiTimeout = 10 * time.Second

	keyError = "error"
)

func main() {
	// Initialize logger (application-level logging)
	appLogger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Load configuration from environment
	config := loadConfig(appLogger)

	// Initialize shop store
	shopStore, err := initializeStore(config, appLogger)
	if err != nil {
		fatal(appLogger, "Failed to initialize store", slog.Any(keyError, err))
	}

	// Admin API token storage: in memory, or shared in Redis/Valkey when the
	// app runs as several instances (see tokenstorage.go).
	tokenStorage, closeTokenStorage, err := newTokenStorage(context.Background(), config, appLogger)
	if err != nil {
		fatal(appLogger, "Failed to initialize token storage", slog.Any(keyError, err))
	}

	// Create SDK instance (minimal setup - no logger required)
	sdk, err := app.New(app.Config{
		AppName:         config.AppName,
		AppSecret:       config.AppSecret,
		ConfirmationURL: config.ConfirmationURL,
		ShopStore:       shopStore,
		// MaxBodyBytes limits the size of incoming request bodies buffered before
		// authentication, guarding public endpoints against memory-exhaustion DoS.
		// Optional: 0 falls back to the default of 5 MiB.
		MaxBodyBytes: maxBodyBytes,
		// TokenStorage keeps the Admin API tokens of all shops. nil keeps them
		// in memory; all clients of a shop share one token either way.
		TokenStorage: tokenStorage,
	})
	if err != nil {
		fatal(appLogger, "Failed to create SDK instance", slog.Any(keyError, err))
	}

	// Setup Gin router
	router := setupRouter(sdk, appLogger, config.Environment)

	// Create HTTP server
	srv := &http.Server{
		Addr:         config.ServerAddr,
		Handler:      router,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	// Start server in goroutine
	go func() {
		appLogger.Info("Starting Shopware App Service",
			slog.String("address", config.ServerAddr),
			slog.String("app_name", config.AppName),
			slog.String("environment", config.Environment),
		)

		if serverErr := srv.ListenAndServe(); serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
			fatal(appLogger, "Server failed", slog.Any(keyError, serverErr))
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	appLogger.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if shutdownErr := srv.Shutdown(ctx); shutdownErr != nil {
		fatal(appLogger, "Server forced to shutdown", slog.Any(keyError, shutdownErr))
	}
	if closeErr := closeTokenStorage(); closeErr != nil {
		appLogger.Warn("Failed to close token storage", slog.Any(keyError, closeErr))
	}

	appLogger.Info("Server exited")
}

func fatal(logger *slog.Logger, msg string, args ...any) {
	logger.Error(msg, args...)
	os.Exit(1)
}

// Config holds application configuration
type Config struct {
	AppName          string
	AppSecret        string
	ConfirmationURL  string
	ServerAddr       string
	Environment      string
	StoreType        string
	DatabasePath     string
	DynamoDBTable    string
	DynamoDBRegion   string
	DynamoDBEndpoint string

	// Admin API token storage
	TokenStorage       string // "memory" (default) or "redis"
	RedisAddr          string
	RedisPassword      string
	TokenKeyPrefix     string
	TokenEncryptionKey string // optional, base64 of 32 bytes
}

func loadConfig(logger *slog.Logger) Config {
	config := Config{
		AppName:          getEnv("SHOPWARE_APP_NAME", "GinShopwareApp"),
		AppSecret:        getEnv("SHOPWARE_APP_SECRET", ""),
		ConfirmationURL:  getEnv("SHOPWARE_CONFIRMATION_URL", "http://localhost:8080/app/register/confirm"),
		ServerAddr:       getEnv("SERVER_ADDR", ":8080"),
		Environment:      getEnv("ENVIRONMENT", "development"),
		StoreType:        getEnv("STORE_TYPE", "sqlite"),
		DatabasePath:     getEnv("DATABASE_PATH", "./data/shops.db"),
		DynamoDBTable:    getEnv("DYNAMODB_TABLE", "shopware-shops"),
		DynamoDBRegion:   getEnv("DYNAMODB_REGION", "us-east-1"),
		DynamoDBEndpoint: getEnv("DYNAMODB_ENDPOINT", ""),

		TokenStorage:       getEnv("TOKEN_STORAGE", tokenStorageMemory),
		RedisAddr:          getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:      getEnv("REDIS_PASSWORD", ""),
		TokenKeyPrefix:     getEnv("TOKEN_KEY_PREFIX", "gin-service:token:"),
		TokenEncryptionKey: getEnv("TOKEN_ENCRYPTION_KEY", ""),
	}

	if config.AppSecret == "" {
		fatal(logger, "SHOPWARE_APP_SECRET environment variable is required")
	}

	return config
}

func initializeStore(config Config, logger *slog.Logger) (store.ShopStore, error) {
	switch config.StoreType {
	case "sqlite":
		logger.Info("Using SQLite store", slog.String("database_path", config.DatabasePath))
		return store.NewSQLiteStore(store.SQLiteConfig{
			DatabasePath: config.DatabasePath,
		})
	case "dynamodb":
		logger.Info("Using DynamoDB store",
			slog.String("table", config.DynamoDBTable),
			slog.String("region", config.DynamoDBRegion),
		)
		return store.NewDynamoDBStore(store.DynamoDBConfig{
			TableName: config.DynamoDBTable,
			Region:    config.DynamoDBRegion,
			Endpoint:  config.DynamoDBEndpoint,
		})
	case "memory":
		logger.Warn("Using in-memory store (not recommended - data will be lost on restart)")
		return store.NewInMemoryStore(), nil
	default:
		return nil, fmt.Errorf("unknown store type: %s", config.StoreType)
	}
}

func setupRouter(sdk *app.App, appLogger *slog.Logger, environment string) *gin.Engine {
	// Set Gin mode based on environment
	if environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// Middleware
	router.Use(ginLogger(appLogger))
	router.Use(gin.Recovery())

	// Configure Shopware app integration - ONE LINE!
	// Logger is optional - if not provided, integration uses a no-op logger
	if err := ginintegration.Configure(router, ginintegration.Config{
		App:        sdk,
		Logger:     appLogger, // Optional: add logging to integration
		EnableCORS: true,
	}); err != nil {
		fatal(appLogger, "Failed to configure Shopware integration", slog.Any(keyError, err))
	}

	// Health check endpoint
	router.GET("/health", healthCheck(sdk))

	// Custom webhook handlers with signature verification
	// The WebhookMiddleware also accepts an optional logger
	webhookGroup := router.Group("/app/webhook")
	webhookGroup.Use(ginintegration.WebhookMiddleware(sdk, appLogger))
	{
		webhookGroup.POST("/product", handleProductWebhook(sdk, appLogger))
	}

	return router
}

func healthCheck(sdk *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check store health
		if err := sdk.Store().HealthCheck(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unhealthy",
				keyError: err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"timestamp": time.Now().Unix(),
		})
	}
}

func handleProductWebhook(sdk *app.App, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get shop from context (automatically set by integration)
		shop, exists := ginintegration.GetShop(c)
		if !exists {
			c.JSON(http.StatusBadRequest, gin.H{keyError: "Shop not found"})
			return
		}

		// Get verified webhook event
		event, exists := ginintegration.GetVerifiedEvent(c)
		if !exists {
			c.JSON(http.StatusBadRequest, gin.H{keyError: "Event not found"})
			return
		}

		logger.Info("Processing product webhook",
			slog.String("event", event.Data.Event),
			slog.String("shop_id", shop.ShopID),
		)

		var changes []webhook.EntityChange
		if err := event.Data.UnmarshalPayload(&changes); err != nil {
			logger.Error("Failed to unmarshal webhook payload",
				slog.String("event", event.Data.Event),
				slog.Any(keyError, err),
			)
			c.JSON(http.StatusBadRequest, gin.H{keyError: "invalid payload"})
			return
		}

		for _, change := range changes {
			logger.Info("Entity change detected",
				slog.String("entity", change.Entity),
				slog.String("operation", change.Operation),
				slog.String("id", change.PrimaryKey),
				slog.Any("updated_fields", change.UpdatedFields),
			)

			if change.Operation == "update" || change.Operation == "insert" {
				// The request context ends with the response; keep its values
				// but not its cancellation for the background work.
				go processEntityChange(context.WithoutCancel(c.Request.Context()), sdk, shop, change, logger)
			}
		}

		c.Status(http.StatusNoContent)
	}
}

// processEntityChange loads the changed entity from the shop's Admin API.
func processEntityChange(
	ctx context.Context, sdk *app.App, shop *store.Shop, change webhook.EntityChange, logger *slog.Logger,
) {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	client, err := sdk.NewClientForShop(shop)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to create Admin API client",
			slog.String("shop_id", shop.ShopID), slog.Any(keyError, err))
		return
	}

	// Entity names are snake_case ("sales_channel"), API paths use dashes.
	// Path escapes every segment, so IDs from the payload cannot change the path.
	path := adminapi.Path(strings.ReplaceAll(change.Entity, "_", "-"), change.PrimaryKey)
	resp, err := client.Get(ctx, path)
	if adminapi.IsNotFound(err) {
		logger.InfoContext(ctx, "Entity was deleted in the meantime",
			slog.String("entity", change.Entity), slog.String("id", change.PrimaryKey))
		return
	}
	if err != nil {
		logger.ErrorContext(ctx, "Failed to fetch entity", slog.String("path", path), slog.Any(keyError, err))
		return
	}

	var result struct {
		Data struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err = resp.JSON(&result); err != nil {
		logger.ErrorContext(ctx, "Failed to decode entity", slog.String("path", path), slog.Any(keyError, err))
		return
	}

	logger.InfoContext(ctx, "Entity fetched",
		slog.String("shop_id", shop.ShopID),
		slog.String("entity", change.Entity),
		slog.String("id", result.Data.ID),
		slog.String("name", result.Data.Name),
	)
}

func ginLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)

		logger.Info("HTTP request",
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.String("query", query),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("latency", latency),
			slog.String("ip", c.ClientIP()),
			slog.String("user_agent", c.Request.UserAgent()),
		)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
