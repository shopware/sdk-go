package gin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopware/sdk-go/app"
	"github.com/shopware/sdk-go/registration"
	"github.com/shopware/sdk-go/resolver"
	"github.com/shopware/sdk-go/sdkerrors"
	"github.com/shopware/sdk-go/signature"
	"github.com/shopware/sdk-go/store"
	"github.com/shopware/sdk-go/webhook"
)

// Config defines the configuration for integrating the Shopware SDK with Gin
type Config struct {
	// App is an already constructed App instance
	// If provided, AppName, AppSecret, and ShopStore are not required
	App *app.App

	// AppName is the name of the Shopware app
	// Required if App is not provided
	AppName string

	// AppSecret is the app secret from manifest.xml
	// Required if App is not provided
	AppSecret string

	// ShopStore is the repository for shop data
	// Required if App is not provided
	ShopStore store.ShopStore

	// Logger is the logger instance
	// If not provided, a no-op logger will be used
	Logger *slog.Logger

	// ConfirmationURL is the full URL where Shopware sends registration confirmation
	// If not provided, it will be auto-generated from the request
	ConfirmationURL string

	// MaxBodyBytes limits the size (in bytes) of incoming request bodies buffered
	// before authentication, mitigating memory-exhaustion DoS via oversized payloads.
	// When 0, webhook.DefaultMaxBodyBytes (5 MiB) is used.
	// Note: only applied when this integration constructs the App (i.e. App is nil).
	// Negative values are rejected by app.Config.Validate().
	MaxBodyBytes int64

	// BasePath is the default base path for Shopware app endpoints
	// Default: "/app"
	BasePath string

	// RegistrationBasePath overrides the base path for registration endpoints
	// Applies to: /register and /register/confirm
	// Default: BasePath
	RegistrationBasePath string

	// EnableCORS enables CORS middleware for the app endpoints
	// Default: false
	EnableCORS bool

	// CORSAllowedOrigins specifies allowed origins for CORS
	// If empty and EnableCORS is true, defaults to "*" (allow all)
	// Example: []string{"https://example.com", "https://app.example.com"}
	CORSAllowedOrigins []string

	// QueryPolicy governs freshness and shop-state checks for signed GET query
	// authentication. The zero value requires a timestamp, expires signed
	// queries after 5 minutes and rejects inactive shops.
	// Note: only applied when this integration constructs the App (i.e. App is nil).
	QueryPolicy resolver.QueryPolicy

	// PostPolicy governs the freshness check for signed JSON POST authentication
	// that is not a webhook event. The zero value checks the body's timestamp
	// only when one is present.
	// Note: only applied when this integration constructs the App (i.e. App is nil).
	PostPolicy resolver.PostPolicy

	// WebhookPolicy governs the freshness check for webhook authentication.
	// The zero value requires a timestamp within 5 minutes.
	// Note: only applied when this integration constructs the App (i.e. App is nil).
	WebhookPolicy webhook.Policy

	// LifecyclePolicy governs the freshness check on the activate, deactivate and
	// delete routes. The zero value requires a timestamp within 5 minutes.
	// Note: only applied when this integration constructs the App (i.e. App is nil).
	LifecyclePolicy resolver.LifecyclePolicy

	// InflightAllowance is the grace window for accepting signatures made with
	// the previous secret after a rotation. When <= 0,
	// signature.DefaultInflightAllowance (1 minute) is used.
	// Note: only applied when this integration constructs the App (i.e. App is nil).
	InflightAllowance time.Duration

	// AllowUnsignedReregistration re-enables a known-vulnerable path; see the field
	// of the same name on app.Config. Leave false unless you must serve shops on
	// Shopware older than 6.6.10.15 / 6.7.8.1.
	// Note: only applied when this integration constructs the App (i.e. App is nil).
	AllowUnsignedReregistration bool
}

const (
	defaultBasePath                  = "/app"
	defaultServerReadTimeout         = 15 * time.Second
	defaultServerWriteTimeout        = 15 * time.Second
	contextKeyShop            string = "shopware_shop"
	contextKeyVerified        string = "shopware_verified_event"
	jsonKeyError              string = "error"
)

// responseBodyWriter buffers the body so it can be signed before headers commit.
type responseBodyWriter struct {
	gin.ResponseWriter

	body *bytes.Buffer
}

func (w *responseBodyWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

func (w *responseBodyWriter) WriteString(s string) (int, error) {
	return w.body.WriteString(s)
}

// WriteHeaderNow is suppressed so AbortWithStatus / explicit calls cannot commit
// headers before signing; the real commit happens in flush().
func (w *responseBodyWriter) WriteHeaderNow() {}

// Flush is suppressed: a signed response must be buffered in full, so early
// (streaming) flushes are not supported on signed routes.
func (w *responseBodyWriter) Flush() {}

// flush commits status, headers and body once, after signing.
func (w *responseBodyWriter) flush() {
	w.ResponseWriter.WriteHeaderNow()
	if w.body.Len() > 0 {
		_, _ = w.ResponseWriter.Write(w.body.Bytes())
	}
}

// Configure sets up all Shopware app endpoints on the provided Gin engine
//
// This automatically configures:
//   - GET  /app/register - Registration endpoint
//   - POST /app/register/confirm - Registration confirmation
//   - POST /app/activate - App activation webhook
//   - POST /app/deactivate - App deactivation webhook
//   - POST /app/delete - App deletion webhook
//   - Signature verification middleware for all base-path routes
//   - Response signing middleware
//
// Example:
//
//	router := gin.Default()
//	ginintegration.Configure(router, ginintegration.Config{
//	    AppName:   "MyApp",
//	    AppSecret: "my-secret",
//	    ShopStore: store.NewInMemoryStore(),
//	})
func Configure(router *gin.Engine, cfg Config) error {
	// Validate configuration
	if err := validateConfig(&cfg); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Set defaults
	if cfg.BasePath == "" {
		cfg.BasePath = defaultBasePath
	}
	if cfg.RegistrationBasePath == "" {
		cfg.RegistrationBasePath = cfg.BasePath
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}

	// Create App instance if not provided
	if cfg.App == nil {
		sdkApp, err := app.New(app.Config{
			AppName:                     cfg.AppName,
			AppSecret:                   cfg.AppSecret,
			ConfirmationURL:             cfg.ConfirmationURL,
			ShopStore:                   cfg.ShopStore,
			MaxBodyBytes:                cfg.MaxBodyBytes,
			QueryPolicy:                 cfg.QueryPolicy,
			PostPolicy:                  cfg.PostPolicy,
			WebhookPolicy:               cfg.WebhookPolicy,
			LifecyclePolicy:             cfg.LifecyclePolicy,
			InflightAllowance:           cfg.InflightAllowance,
			AllowUnsignedReregistration: cfg.AllowUnsignedReregistration,
		})
		if err != nil {
			return fmt.Errorf("failed to create app: %w", err)
		}
		cfg.App = sdkApp
	}

	if cfg.RegistrationBasePath == cfg.BasePath {
		// Create route group for app endpoints
		appGroup := router.Group(cfg.RegistrationBasePath)

		// Apply CORS if enabled
		if cfg.EnableCORS {
			appGroup.Use(corsMiddleware(cfg.CORSAllowedOrigins))
		}

		// Register routes
		registerRoutes(appGroup, cfg.App, cfg.Logger, cfg.RegistrationBasePath)
	} else {
		registrationGroup := router.Group(cfg.RegistrationBasePath)
		lifecycleGroup := router.Group(cfg.BasePath)

		if cfg.EnableCORS {
			registrationGroup.Use(corsMiddleware(cfg.CORSAllowedOrigins))
			lifecycleGroup.Use(corsMiddleware(cfg.CORSAllowedOrigins))
		}

		registerRegistrationRoutes(registrationGroup, cfg.App, cfg.Logger, cfg.RegistrationBasePath)
		registerLifecycleRoutes(lifecycleGroup, cfg.App, cfg.Logger)

		registrationExempt := buildExemptPaths(cfg.RegistrationBasePath, true, false)
		lifecycleExempt := buildExemptPaths(cfg.BasePath, false, true)

		registrationGroup.Use(signatureVerificationMiddlewareWithExemptPaths(cfg.App, cfg.Logger, registrationExempt))
		lifecycleGroup.Use(signatureVerificationMiddlewareWithExemptPaths(cfg.App, cfg.Logger, lifecycleExempt))
	}

	return nil
}

func validateConfig(cfg *Config) error {
	if cfg.App == nil {
		if cfg.AppName == "" {
			return errors.New("AppName is required when App is not provided")
		}
		if cfg.AppSecret == "" {
			return errors.New("AppSecret is required when App is not provided")
		}
		if cfg.ShopStore == nil {
			return errors.New("ShopStore is required when App is not provided")
		}
	}
	return nil
}

func registerRoutes(group *gin.RouterGroup, sdk *app.App, log *slog.Logger, basePath string) {
	registerRegistrationRoutes(group, sdk, log, basePath)
	registerLifecycleRoutes(group, sdk, log)

	// All other routes under the base path require signature verification and response signing
	// Users can add their own routes with signature verification middleware
	group.Use(signatureVerificationMiddleware(sdk, log, basePath))
}

func registerRegistrationRoutes(group *gin.RouterGroup, sdk *app.App, log *slog.Logger, basePath string) {
	group.GET("/register", handleRegistration(sdk, log, basePath))
	group.POST("/register/confirm", handleConfirmation(sdk, log))
}

func registerLifecycleRoutes(group *gin.RouterGroup, sdk *app.App, log *slog.Logger) {
	// These handle their own verification: unlike other routes they must serve
	// inactive shops, and each one names the signed event it accepts.
	group.POST("/activate", wrapWithResponseSigning(handleActivateWebhook(sdk, log), sdk))
	group.POST("/deactivate", wrapWithResponseSigning(handleDeactivateWebhook(sdk, log), sdk))
	group.POST("/delete", wrapWithResponseSigning(handleDeleteWebhook(sdk, log), sdk))
}

func handleRegistration(sdk *app.App, log *slog.Logger, basePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract query parameters
		shopID := c.Query("shop-id")
		shopURL := c.Query("shop-url")
		timestampStr := c.Query("timestamp")

		// Validate required parameters
		if shopID == "" || shopURL == "" || timestampStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				jsonKeyError: "Missing required parameters: shop-id, shop-url, timestamp",
			})
			return
		}

		// Parse timestamp
		timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				jsonKeyError: "Invalid timestamp format",
			})
			return
		}

		// Build raw query string for signature verification
		rawQuery := fmt.Sprintf("shop-id=%s&shop-url=%s&timestamp=%s",
			shopID, shopURL, timestampStr)

		req := &registration.Request{
			ShopID:        shopID,
			ShopURL:       shopURL,
			Timestamp:     timestamp,
			RawQuery:      rawQuery,
			AppSignature:  c.GetHeader(signature.AppSignatureHeader),
			ShopSignature: c.GetHeader(signature.ShopSignatureHeader),
		}

		resp, handleErr := sdk.RegistrationHandler().HandleRegistration(c.Request.Context(), req)
		if handleErr != nil {
			log.Warn("Registration failed",
				slog.String("shop_id", shopID),
				slog.String("shop_url", shopURL),
				slog.Any("error", handleErr),
			)
			handleSDKError(c, handleErr, log)
			return
		}

		// Set confirmation URL if not already set
		if resp.ConfirmationURL == "" {
			resp.ConfirmationURL = buildConfirmationURL(c.Request, sdk.Config().ConfirmationURL, basePath)
		}

		log.Info("Registration successful",
			slog.String("shop_id", shopID),
			slog.String("shop_url", shopURL),
			slog.String("confirmation_url", resp.ConfirmationURL),
		)
		c.JSON(http.StatusOK, resp)
	}
}

func handleConfirmation(sdk *app.App, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Read body, enforcing the configured size limit before authentication
		body, err := webhook.ReadLimitedBody(c.Request, sdk.Config().MaxBodyBytes)
		if err != nil {
			// Only over-limit bodies map to 413; other read failures stay a
			// generic 400 and must not leak internal error details.
			if errors.Is(err, sdkerrors.ErrPayloadTooLarge) {
				handleSDKError(c, err, log)
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{
				jsonKeyError: "Failed to read request body",
			})
			return
		}

		// Parse request
		var req registration.ConfirmationRequest
		if unmarshalErr := json.Unmarshal(body, &req); unmarshalErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				jsonKeyError: "Invalid JSON",
			})
			return
		}

		req.Signature = c.GetHeader(signature.ShopSignatureHeader)
		req.PreviousSignature = c.GetHeader(signature.ShopSignaturePreviousHeader)
		req.RawBody = body

		// Handle confirmation
		_, confirmErr := sdk.RegistrationHandler().HandleConfirmation(c.Request.Context(), &req)
		if confirmErr != nil {
			log.Warn("Confirmation failed",
				slog.String("shop_id", req.ShopID),
				slog.String("shop_url", req.ShopURL),
				slog.Any("error", confirmErr),
			)
			handleSDKError(c, confirmErr, log)
			return
		}

		log.Info("Confirmation successful",
			slog.String("shop_id", req.ShopID),
			slog.String("shop_url", req.ShopURL),
		)

		c.Status(http.StatusNoContent)
	}
}

// WebhookMiddleware returns a Gin middleware that verifies webhook signatures and injects
// verified shop and event data into the context. Use this for custom webhook handlers.
func WebhookMiddleware(sdk *app.App, log *slog.Logger) gin.HandlerFunc {
	return signatureVerificationMiddleware(sdk, log, "")
}

// GetRequestVerifier returns a Gin middleware that verifies GET requests with query parameter signatures.
// This is useful for admin modules, action buttons, and other browser-initiated requests from Shopware.
//
// The middleware expects the following query parameters:
//   - shop-id: The shop identifier
//   - shopware-shop-signature: HMAC-SHA256 signature of the query string (excluding the signature itself)
//   - timestamp: Unix epoch the request was created at
//
// Verification is delegated to the SDK resolver, so the request is checked
// against the app's configured resolver.QueryPolicy: the signature must match
// (including the previous secret during a rotation grace window), the request
// must be fresh, and the shop must be active.
//
// On successful verification, the shop is stored in the Gin context and can be retrieved using GetShop().
//
// Example usage:
//
//	router.GET("/admin/module", ginintegration.GetRequestVerifier(app, logger), func(c *gin.Context) {
//	    shop, _ := ginintegration.GetShop(c)
//	    // Handle admin module request
//	})
func GetRequestVerifier(sdk *app.App, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		shop, err := sdk.Resolver().ResolveFromQuery(c.Request.Context(), c.Request)
		if err != nil {
			abortWithQueryError(c, err, log)
			return
		}

		c.Set(contextKeyShop, shop)
		c.Next()
	}
}

// GetRequestVerifierWithPolicy behaves like GetRequestVerifier but applies an
// explicit policy instead of the app-wide one. Use it for individual routes
// that must deviate, for example a legacy endpoint whose signed links carry no
// timestamp:
//
//	router.GET("/legacy", ginintegration.GetRequestVerifierWithPolicy(app, logger,
//	    resolver.QueryPolicy{Timestamp: resolver.TimestampVerifyIfPresent}), handler)
func GetRequestVerifierWithPolicy(sdk *app.App, log *slog.Logger, policy resolver.QueryPolicy) gin.HandlerFunc {
	return func(c *gin.Context) {
		shop, err := sdk.Resolver().ResolveFromQueryWithPolicy(c.Request.Context(), c.Request, policy)
		if err != nil {
			abortWithQueryError(c, err, log)
			return
		}

		c.Set(contextKeyShop, shop)
		c.Next()
	}
}

func abortWithQueryError(c *gin.Context, err error, log *slog.Logger) {
	log.Warn("Signed query verification failed",
		slog.String("shop_id", c.Query("shop-id")),
		slog.Any("error", err),
	)
	handleSDKError(c, err, log)
	c.Abort()
}

func buildExemptPaths(basePath string, includeRegistration, includeLifecycle bool) []string {
	if basePath == "" {
		return nil
	}

	var exemptPaths []string
	if includeRegistration {
		exemptPaths = append(
			exemptPaths,
			basePath+"/register",
			basePath+"/register/confirm",
		)
	}
	if includeLifecycle {
		exemptPaths = append(
			exemptPaths,
			basePath+"/activate",
			basePath+"/deactivate",
			basePath+"/delete",
		)
	}

	return exemptPaths
}

func signatureVerificationMiddleware(sdk *app.App, log *slog.Logger, basePath string) gin.HandlerFunc {
	exemptPaths := buildExemptPaths(basePath, true, true)
	return signatureVerificationMiddlewareWithExemptPaths(sdk, log, exemptPaths)
}

func signatureVerificationMiddlewareWithExemptPaths(sdk *app.App, log *slog.Logger, exemptPaths []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip verification for registration and lifecycle endpoints
		if len(exemptPaths) > 0 {
			path := c.Request.URL.Path
			for _, exemptPath := range exemptPaths {
				if path == exemptPath {
					c.Next()
					return
				}
			}
		}

		// For GET requests, use the exported verifier
		if c.Request.Method == http.MethodGet {
			GetRequestVerifier(sdk, log)(c)
			return
		}

		// For POST requests, verify webhook
		verified, verifyErr := verifyWebhookRequest(c, sdk, log)
		if verifyErr != nil {
			c.Abort()
			return
		}

		// Store verified data in context
		c.Set(contextKeyShop, verified.Shop)
		c.Set(contextKeyVerified, verified)

		// Buffer body; sign + flush on the normal path, panic-safe on panic.
		bodyWriter := &responseBodyWriter{
			ResponseWriter: c.Writer,
			body:           bytes.NewBuffer(nil),
		}
		c.Writer = bodyWriter
		defer finalizeSignedResponse(c, bodyWriter)

		c.Next()
	}
}

func verifyWebhookRequest(c *gin.Context, sdk *app.App, log *slog.Logger) (*webhook.VerifiedRequest, error) {
	traceID := c.GetHeader("X-Request-ID")
	if traceID == "" {
		traceID = fmt.Sprintf("webhook-%d", time.Now().UnixNano())
	}

	verified, err := sdk.WebhookMiddleware().VerifyWebhook(c.Request.Context(), c.Request, traceID)
	if err != nil {
		handleSDKError(c, err, log)
		return nil, err
	}

	return verified, nil
}

func signResponse(w *responseBodyWriter, secret string) {
	// Get the captured response body
	body := w.body.Bytes()

	// Compute signature over the response body
	sig := signature.ComputeSignature(body, secret)

	// Add signature header
	w.Header().Set(signature.AppSignatureHeader, sig)
}

// finalizeSignedResponse must be deferred. On the normal path it signs (if a
// shop is in context) and commits the buffered response. On panic it restores
// the original writer and re-panics without flushing, so upstream recovery
// middleware (e.g. gin.Recovery) can write the intended response.
func finalizeSignedResponse(c *gin.Context, bodyWriter *responseBodyWriter) {
	if rec := recover(); rec != nil {
		// Never let a signature header leak onto the recovery (500) response.
		bodyWriter.ResponseWriter.Header().Del(signature.AppSignatureHeader)
		c.Writer = bodyWriter.ResponseWriter
		panic(rec)
	}

	if shop, exists := c.Get(contextKeyShop); exists {
		if s, ok := shop.(*store.Shop); ok {
			signResponse(bodyWriter, s.ShopSecret)
		}
	}
	bodyWriter.flush()
}

func handleSDKError(c *gin.Context, err error, log *slog.Logger) {
	var sdkErr *sdkerrors.Error
	if errors.As(err, &sdkErr) {
		log.Warn("SDK error",
			slog.String("type", sdkErr.Type),
			slog.String("message", sdkErr.Message),
			slog.Int("code", sdkErr.Code),
		)
		c.JSON(sdkErr.Code, sdkErr)
		return
	}

	log.Error("Internal error", slog.Any("error", err))
	c.JSON(http.StatusInternalServerError, gin.H{jsonKeyError: err.Error()})
}

func buildConfirmationURL(req *http.Request, configured, basePath string) string {
	if configured != "" {
		return configured
	}

	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}

	// Check X-Forwarded-Proto header
	if proto := req.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}

	host := req.Host
	if host == "" {
		host = "localhost"
	}

	trimmedBase := strings.TrimSuffix(basePath, "/")
	if trimmedBase == "" || trimmedBase == "/" {
		return fmt.Sprintf("%s://%s/register/confirm", scheme, host)
	}

	return fmt.Sprintf("%s://%s%s/register/confirm", scheme, host, trimmedBase)
}

func corsMiddleware(allowedOrigins []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		// Determine allowed origin
		allowedOrigin := "*"
		if len(allowedOrigins) > 0 {
			// Check if the request origin is in the allowed list
			allowed := false
			for _, allowedOrig := range allowedOrigins {
				if allowedOrig == origin {
					allowed = true
					allowedOrigin = origin
					break
				}
			}
			if !allowed {
				// Origin not allowed, don't set CORS headers
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
		}

		c.Writer.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set(
			"Access-Control-Allow-Headers",
			"Content-Type, Authorization, shopware-app-signature, Shopware-Shop-Signature",
		)

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// GetShop retrieves the Shop from the Gin context
// This is available after signature verification middleware
func GetShop(c *gin.Context) (*store.Shop, bool) {
	shop, exists := c.Get(contextKeyShop)
	if !exists {
		return nil, false
	}

	s, ok := shop.(*store.Shop)
	return s, ok
}

// GetVerifiedEvent retrieves the verified webhook event from the Gin context
// This is available in webhook handlers after verification
func GetVerifiedEvent(c *gin.Context) (*webhook.Event, bool) {
	verified, exists := c.Get(contextKeyVerified)
	if !exists {
		return nil, false
	}

	v, ok := verified.(*webhook.VerifiedRequest)
	if !ok {
		return nil, false
	}

	return v.Event, true
}

// wrapWithResponseSigning wraps a handler to capture and sign the response
func wrapWithResponseSigning(handler gin.HandlerFunc, _ *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Buffer body; sign + flush on the normal path, panic-safe on panic.
		bodyWriter := &responseBodyWriter{
			ResponseWriter: c.Writer,
			body:           bytes.NewBuffer(nil),
		}
		c.Writer = bodyWriter
		defer finalizeSignedResponse(c, bodyWriter)

		handler(c)
	}
}

// handleActivateWebhook handles the app.activated lifecycle webhook.
// It verifies the signature and the signed event, then sets the shop as active.
// The shop's active status is not checked.
func handleActivateWebhook(sdk *app.App, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Resolve and verify lifecycle webhook
		shop, event, err := sdk.Resolver().ResolveLifecycleWebhook(
			c.Request.Context(), c.Request, webhook.EventAppActivated,
		)
		if err != nil {
			handleSDKError(c, err, log)
			return
		}

		// Store shop in context for response signing
		c.Set(contextKeyShop, shop)

		// Set the shop as active
		if setActiveErr := sdk.Store().SetShopActive(c.Request.Context(), shop.ShopID, true); setActiveErr != nil {
			handleSDKError(c, setActiveErr, log)
			return
		}

		log.Info("App activated",
			slog.String("shop_id", shop.ShopID),
			slog.String("event", event.Data.Event),
		)

		c.Status(http.StatusNoContent)
	}
}

// handleDeactivateWebhook handles the app.deactivated lifecycle webhook.
// It verifies the signature and the signed event, then sets the shop as inactive.
// The shop's active status is not checked.
func handleDeactivateWebhook(sdk *app.App, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Resolve and verify lifecycle webhook
		shop, event, err := sdk.Resolver().ResolveLifecycleWebhook(
			c.Request.Context(), c.Request, webhook.EventAppDeactivated,
		)
		if err != nil {
			handleSDKError(c, err, log)
			return
		}

		// Store shop in context for response signing
		c.Set(contextKeyShop, shop)

		// Set the shop as inactive
		if setActiveErr := sdk.Store().SetShopActive(c.Request.Context(), shop.ShopID, false); setActiveErr != nil {
			handleSDKError(c, setActiveErr, log)
			return
		}

		log.Info("App deactivated",
			slog.String("shop_id", shop.ShopID),
			slog.String("event", event.Data.Event),
		)

		c.Status(http.StatusNoContent)
	}
}

// handleDeleteWebhook handles the app.deleted lifecycle webhook.
// It verifies the signature and the signed event, then deletes the shop.
// The shop's active status is not checked.
func handleDeleteWebhook(sdk *app.App, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Resolve and verify lifecycle webhook
		shop, event, err := sdk.Resolver().ResolveLifecycleWebhook(
			c.Request.Context(), c.Request, webhook.EventAppDeleted,
		)
		if err != nil {
			handleSDKError(c, err, log)
			return
		}

		// Store shop in context for response signing
		c.Set(contextKeyShop, shop)

		// Delete the shop
		if deleteErr := sdk.Store().DeleteShop(c.Request.Context(), shop.ShopID); deleteErr != nil {
			handleSDKError(c, deleteErr, log)
			return
		}

		// The shop is gone, so its Admin API token is useless. Failing to
		// remove it is not an error for Shopware: the token expires anyway.
		if clearErr := sdk.ClearShopToken(c.Request.Context(), shop); clearErr != nil {
			log.Warn("Failed to clear shop token",
				slog.String("shop_id", shop.ShopID),
				slog.String("error", clearErr.Error()),
			)
		}

		log.Info("App deleted",
			slog.String("shop_id", shop.ShopID),
			slog.String("event", event.Data.Event),
		)

		c.Status(http.StatusNoContent)
	}
}
