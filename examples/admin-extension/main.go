package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/shopware/app-sdk-go/examples/admin-extension/auth"
	ginintegration "github.com/shopware/app-sdk-go/integrations/gin"
	"github.com/shopware/app-sdk-go/pkg/app"
	"github.com/shopware/app-sdk-go/pkg/store"
)

const (
	httpReadTimeout  = 15 * time.Second
	httpWriteTimeout = 15 * time.Second
	httpIdleTimeout  = 60 * time.Second
	shutdownTimeout  = 30 * time.Second

	// The bearer token is minted from a signed iframe URL, so it must stay
	// short-lived: a signed URL that leaks (browser history, Referer, proxy
	// logs) would otherwise be exchangeable for long-lived access.
	tokenDuration = 15 * time.Minute

	// The frontend refreshes the token via /api/auth/refresh. It cannot
	// re-authenticate by replaying the iframe's signed query, because the SDK
	// expires signed queries after a few minutes.
	tokenRenewGrace = 5 * time.Minute

	// Refreshing slides the session but never past this cap, otherwise a
	// leaked token would again grant open-ended access. After it elapses the
	// user has to reopen the module, which produces a freshly signed query.
	maxSessionDuration = 8 * time.Hour

	// maxBodyBytes is 5 MiB.
	maxBodyBytes = 5 << 20

	keyError  = "error"
	keyStatus = "status"
)

func main() {
	// Initialize logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Load configuration
	config := loadConfig(logger)

	// Initialize SQLite store
	shopStore, err := store.NewSQLiteStore(store.SQLiteConfig{
		DatabasePath: config.DatabasePath,
	})
	if err != nil {
		fatal(logger, "Failed to initialize store", slog.Any(keyError, err))
	}

	// Initialize token manager for frontend authentication
	tokenManager, err := auth.NewTokenManager(config.TokenSecret)
	if err != nil {
		fatal(logger, "Failed to initialize token manager", slog.Any(keyError, err))
	}

	// Create SDK instance
	sdk, err := app.New(app.Config{
		AppName:         config.AppName,
		AppSecret:       config.AppSecret,
		ConfirmationURL: config.ConfirmationURL,
		ShopStore:       shopStore,
		// MaxBodyBytes limits the size of incoming request bodies buffered before
		// authentication, guarding public endpoints against memory-exhaustion DoS.
		// Optional: 0 falls back to the default of 5 MiB.
		MaxBodyBytes: maxBodyBytes,
		// Admin API tokens are kept in memory and shared by all clients of a
		// shop, so the dashboard requests reuse one token per shop.
	})
	if err != nil {
		fatal(logger, "Failed to create SDK instance", slog.Any(keyError, err))
	}

	// Setup Gin router
	router := setupRouter(sdk, tokenManager, logger)

	// Create HTTP server
	srv := &http.Server{
		Addr:         config.ServerAddr,
		Handler:      router,
		ReadTimeout:  httpReadTimeout,
		WriteTimeout: httpWriteTimeout,
		IdleTimeout:  httpIdleTimeout,
	}

	// Start server
	go func() {
		logger.Info("Starting Admin Extension Service",
			slog.String("address", config.ServerAddr),
			slog.String("app_name", config.AppName),
		)

		if serverErr := srv.ListenAndServe(); serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
			fatal(logger, "Server failed", slog.Any(keyError, serverErr))
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if shutdownErr := srv.Shutdown(ctx); shutdownErr != nil {
		fatal(logger, "Server forced to shutdown", slog.Any(keyError, shutdownErr))
	}

	logger.Info("Server exited")
}

// fatal logs an error message and exits the process. slog has no Fatal level,
// so this mirrors the previous zap.Fatal behaviour for the example.
func fatal(logger *slog.Logger, msg string, args ...any) {
	logger.Error(msg, args...)
	os.Exit(1)
}

// Config holds application configuration
type Config struct {
	AppName         string
	AppSecret       string
	TokenSecret     string
	ConfirmationURL string
	ServerAddr      string
	DatabasePath    string
}

func loadConfig(logger *slog.Logger) Config {
	config := Config{
		AppName:     getEnv("SHOPWARE_APP_NAME", "AdminExtensionApp"),
		AppSecret:   getEnv("SHOPWARE_APP_SECRET", ""),
		TokenSecret: getEnv("TOKEN_SECRET", ""),
		ConfirmationURL: getEnv(
			"SHOPWARE_CONFIRMATION_URL",
			"http://host.docker.internal:8085/app/register/confirm",
		),
		ServerAddr:   getEnv("SERVER_ADDR", ":8080"),
		DatabasePath: getEnv("DATABASE_PATH", "./data/shops.db"),
	}

	if config.AppSecret == "" {
		fatal(logger, "SHOPWARE_APP_SECRET environment variable is required")
	}

	if config.TokenSecret == "" {
		fatal(logger, "TOKEN_SECRET environment variable is required (min 32 characters)")
	}

	return config
}

func setupRouter(sdk *app.App, tokenMgr *auth.TokenManager, logger *slog.Logger) *gin.Engine {
	router := gin.Default()

	// CORS configuration for local development
	// Allow frontend to call backend APIs
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://host.docker.internal:5173", "http://localhost:5173"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	// Configure Shopware app integration (lifecycle endpoints).
	// Note: the request body-size limit (MaxBodyBytes) is taken from the pre-built
	// App above; the gin Config.MaxBodyBytes field only applies when this
	// integration constructs the App itself (i.e. when App is nil).
	if err := ginintegration.Configure(router, ginintegration.Config{
		App:        sdk,
		EnableCORS: false, // Using cors.Default() above
	}); err != nil {
		fatal(logger, "Failed to configure Shopware integration", slog.Any(keyError, err))
	}

	// Health check
	router.GET("/health", healthCheck(sdk))

	// Frontend authentication: exchange the signed Shopware query for a
	// short-lived token, then slide it via refresh while the session lasts.
	router.GET("/api/auth/token", createAuthToken(sdk, tokenMgr, logger))
	router.POST("/api/auth/refresh", refreshAuthToken(sdk, tokenMgr, logger))

	// Protected API routes (require token authentication)
	api := router.Group("/api")
	api.Use(tokenAuthMiddleware(tokenMgr))
	{
		api.GET("/shop/info", getShopInfo(sdk, logger))
		api.GET("/dashboard/stats", getDashboardStats(sdk, logger))
	}

	return router
}

func healthCheck(sdk *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := sdk.Store().HealthCheck(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				keyStatus: "unhealthy",
				"error":   err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			keyStatus:   "healthy",
			"timestamp": time.Now().Unix(),
		})
	}
}

func createAuthToken(sdk *app.App, tokenMgr *auth.TokenManager, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Never log the raw query: it carries the signature, and a logged
		// signed URL is a credential someone can replay. Log the shop id only.
		logger.Debug("Token generation request received",
			slog.String("shop_id", c.Query("shop-id")),
		)

		// Resolve and verify shop from query parameters. The resolver applies
		// the app's QueryPolicy, so this also enforces request freshness and
		// rejects shops that are no longer active.
		shop, err := sdk.Resolver().ResolveFromQuery(c.Request.Context(), c.Request)
		if err != nil {
			logger.Warn("Failed to resolve shop for token generation",
				slog.Any(keyError, err),
				slog.String("shop_id", c.Query("shop-id")),
			)
			c.JSON(http.StatusUnauthorized, gin.H{keyError: "Invalid shop authentication"})
			return
		}

		// Generate a short-lived token for the admin iframe session
		token, err := tokenMgr.Generate(shop.ShopID, shop.ShopURL, tokenDuration)
		if err != nil {
			logger.Error("Failed to generate token",
				slog.Any(keyError, err),
				slog.String("shop_id", shop.ShopID),
			)
			c.JSON(http.StatusInternalServerError, gin.H{keyError: "Failed to generate token"})
			return
		}

		logger.Info("Generated authentication token",
			slog.String("shop_id", shop.ShopID),
			slog.String("shop_url", shop.ShopURL),
		)

		c.JSON(http.StatusOK, gin.H{
			"token":     token,
			"expiresIn": int(tokenDuration.Seconds()),
		})
	}
}

// refreshAuthToken slides the frontend session without touching the signed
// iframe URL again. The signed query expires within minutes, so replaying it is
// not an option; the still-valid (or barely expired) token is the credential.
func refreshAuthToken(sdk *app.App, tokenMgr *auth.TokenManager, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerToken(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{keyError: "Missing or malformed authorization header"})
			return
		}

		renewed, shopCtx, err := tokenMgr.Renew(token, auth.RenewOptions{
			Duration:      tokenDuration,
			Grace:         tokenRenewGrace,
			MaxSessionAge: maxSessionDuration,
		})
		if err != nil {
			// A dead session is not an error the frontend can retry: it has to
			// reload the module to obtain a freshly signed query.
			logger.Info("Token refresh rejected", slog.Any(keyError, err))
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":       "Session expired, reload the module",
				"reauthorize": true,
			})
			return
		}

		// The token says who the shop was when it was issued; the store says
		// whether that shop may still be served. Re-check on every refresh so a
		// deactivated shop cannot keep sliding its session.
		shop, err := sdk.Store().GetShopByID(c.Request.Context(), shopCtx.ShopID)
		if err != nil {
			logger.Warn("Shop lookup failed during refresh",
				slog.String("shop_id", shopCtx.ShopID),
				slog.Any(keyError, err),
			)
			// A store failure says nothing about the session. Answer retryable, or
			// the frontend ends a session that is still perfectly valid.
			c.JSON(http.StatusServiceUnavailable, gin.H{keyError: "Shop lookup temporarily failed"})
			return
		}
		if !shop.Active {
			logger.Info("Refusing refresh for inactive shop", slog.String("shop_id", shop.ShopID))
			c.JSON(http.StatusForbidden, gin.H{keyError: "Shop is not active"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"token":     renewed,
			"expiresIn": int(time.Until(shopCtx.ExpiresAt).Seconds()),
		})
	}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
func bearerToken(c *gin.Context) (string, bool) {
	const bearerPrefix = "Bearer "

	authHeader := c.GetHeader("Authorization")
	if len(authHeader) <= len(bearerPrefix) || authHeader[:len(bearerPrefix)] != bearerPrefix {
		return "", false
	}

	return authHeader[len(bearerPrefix):], true
}

func tokenAuthMiddleware(tokenMgr *auth.TokenManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerToken(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{keyError: "Missing or malformed authorization header"})
			c.Abort()
			return
		}

		// Validate token
		shopCtx, err := tokenMgr.Validate(token)
		if err != nil {
			if errors.Is(err, auth.ErrExpiredToken) {
				c.JSON(http.StatusUnauthorized, gin.H{keyError: "Token expired"})
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{keyError: "Invalid token"})
			}
			c.Abort()
			return
		}

		// Store shop context in request context
		c.Set("shopContext", shopCtx)
		c.Set("shopID", shopCtx.ShopID)
		c.Set("shopURL", shopCtx.ShopURL)

		c.Next()
	}
}

func getShopInfo(sdk *app.App, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		shopCtx, exists := c.Get("shopContext")
		if !exists {
			c.JSON(http.StatusInternalServerError, gin.H{keyError: "Shop context not found"})
			return
		}

		ctx, ok := shopCtx.(*auth.ShopContext)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{keyError: "Invalid shop context"})
			return
		}

		logger.InfoContext(c.Request.Context(), "Fetching shop info", slog.String("shop_id", ctx.ShopID))

		info := gin.H{
			"shopId":  ctx.ShopID,
			"shopUrl": ctx.ShopURL,
			keyStatus: "active",
		}

		// The Shopware version comes from the shop's Admin API. It is extra
		// information, so a failure is logged and the rest is still returned.
		client, err := sdk.NewClient(c.Request.Context(), ctx.ShopID)
		if err == nil {
			var version string
			if version, err = shopwareVersion(c.Request.Context(), client); err == nil {
				info["shopwareVersion"] = version
			}
		}
		if err != nil {
			logger.WarnContext(c.Request.Context(), "Failed to read Shopware version",
				slog.String("shop_id", ctx.ShopID), slog.Any(keyError, err))
		}

		c.JSON(http.StatusOK, info)
	}
}

func getDashboardStats(sdk *app.App, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		shopID, exists := c.Get("shopID")
		if !exists {
			c.JSON(http.StatusInternalServerError, gin.H{keyError: "Shop ID not found"})
			return
		}

		shopIDStr, ok := shopID.(string)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{keyError: "Invalid shop ID type"})
			return
		}

		reqCtx := c.Request.Context()
		logger.InfoContext(reqCtx, "Fetching dashboard stats", slog.String("shop_id", shopIDStr))

		client, err := sdk.NewClient(reqCtx, shopIDStr)
		if err != nil {
			logger.ErrorContext(reqCtx, "Failed to create Admin API client",
				slog.String("shop_id", shopIDStr), slog.Any(keyError, err))
			c.JSON(http.StatusBadGateway, gin.H{keyError: "failed to load stats from the shop"})
			return
		}

		stats, err := loadDashboardStats(reqCtx, client)
		if err != nil {
			logger.ErrorContext(reqCtx, "Failed to load dashboard stats",
				slog.String("shop_id", shopIDStr), slog.Any(keyError, err))
			c.JSON(http.StatusBadGateway, gin.H{keyError: "failed to load stats from the shop"})
			return
		}

		c.JSON(http.StatusOK, stats)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
