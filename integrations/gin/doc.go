// Package gin provides one-line Shopware app integration for the Gin web framework.
//
// This package automatically configures all required Shopware endpoints including
// registration, confirmation, lifecycle webhooks, signature verification, and CORS.
//
// # Quick Setup
//
//	import ginintegration "github.com/shopware/app-sdk-go/integrations/gin"
//
//	router := gin.Default()
//	err := ginintegration.Configure(router, ginintegration.Config{
//	    AppName:         "MyApp",
//	    AppSecret:       "your-app-secret",
//	    ConfirmationURL: "https://myapp.com/register/confirm",
//	    ShopStore:       store.NewInMemoryStore(),
//	    Logger:          slog.New(slog.NewJSONHandler(os.Stdout, nil)),
//	    EnableCORS:      true,
//	})
//
// # Automatically Configured Routes
//
//   - GET  /app/register           - Registration endpoint
//   - POST /app/register/confirm   - Confirmation endpoint
//   - POST /app/activate           - App activation webhook
//   - POST /app/deactivate         - App deactivation webhook
//   - POST /app/delete             - App deletion webhook
//
// BasePath defaults to "/app". RegistrationBasePath can be used to place
// registration endpoints on a separate base path.
//
// # Custom Webhook Handlers
//
//	router.POST("/app/webhook/product", func(c *gin.Context) {
//	    shop, _ := ginintegration.GetShop(c)
//	    event, _ := ginintegration.GetVerifiedEvent(c)
//	    // Process webhook
//	})
//
// All routes automatically include signature verification, response signing,
// and proper error handling.
package gin
