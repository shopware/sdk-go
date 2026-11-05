package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/shopware/app-sdk-go/clients/adminapi"
	"github.com/shopware/app-sdk-go/pkg/app"
	sdkerrors "github.com/shopware/app-sdk-go/pkg/errors"
	"github.com/shopware/app-sdk-go/pkg/store"
	"github.com/shopware/app-sdk-go/pkg/webhook"
)

const (
	serverAddr   = ":3000"
	readTimeout  = 15 * time.Second
	writeTimeout = 15 * time.Second
	idleTimeout  = 60 * time.Second

	// maxBodyBytes is 5 MiB.
	maxBodyBytes = 5 << 20

	// apiTimeout bounds the Admin API calls made after a webhook was answered.
	apiTimeout = 10 * time.Second
)

func main() {
	// Initialize and run the server
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Server failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Initialize logger (application-level logging)
	appLogger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Create in-memory shop store (use DynamoDB or SQLite in production)
	shopStore := store.NewInMemoryStore()

	// Create SDK instance
	sdk, sdkErr := app.New(app.Config{
		AppName:         "SimpleApp",
		AppSecret:       "your-app-secret-from-manifest-min8chars",
		ConfirmationURL: "http://localhost:3000/register/confirm",
		ShopStore:       shopStore,
		// MaxBodyBytes limits the size of incoming request bodies buffered before
		// authentication, guarding public endpoints against memory-exhaustion DoS.
		// Optional: 0 falls back to the default of 5 MiB.
		MaxBodyBytes: maxBodyBytes,
		// Admin API tokens are kept in memory by default and shared by all
		// clients of a shop. When you run several instances, set TokenStorage
		// to a shared store (Redis, Valkey, ...); see the gin-service example.
	})
	if sdkErr != nil {
		return fmt.Errorf("failed to create SDK instance: %w", sdkErr)
	}

	// Setup HTTP routes
	http.HandleFunc("/register", handleRegistration(sdk, appLogger))
	http.HandleFunc("/register/confirm", handleConfirmation(sdk, appLogger))
	http.HandleFunc("/webhook", handleWebhook(sdk, appLogger))
	http.HandleFunc("/health", handleHealth(sdk))

	// Start server with timeouts
	server := &http.Server{
		Addr:         serverAddr,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	appLogger.Info("Starting Simple Shopware App",
		slog.String("address", serverAddr),
		slog.String("app_name", "SimpleApp"),
	)

	if serverErr := server.ListenAndServe(); serverErr != nil {
		return fmt.Errorf("server failed: %w", serverErr)
	}

	return nil
}

func handleRegistration(sdk *app.App, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log.InfoContext(ctx, "Registration request received")

		// Use resolver to verify and extract shop from query parameters
		shop, err := sdk.Resolver().ResolveFromQuery(ctx, r)
		if err != nil {
			handleSDKError(w, err, log)
			return
		}

		log.InfoContext(ctx, "Registration successful",
			slog.String("shop_id", shop.ShopID),
			slog.String("shop_url", shop.ShopURL),
		)

		// Return registration response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"proof":            shop.ShopSecret,
			"secret":           shop.ShopSecret,
			"confirmation_url": sdk.Config().ConfirmationURL,
		})
	}
}

func handleConfirmation(sdk *app.App, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log.InfoContext(ctx, "Confirmation request received")

		// Use resolver to verify webhook signature and parse request
		verified, err := sdk.WebhookMiddleware().VerifyWebhook(ctx, r, "confirmation")
		if err != nil {
			handleSDKError(w, err, log)
			return
		}

		log.InfoContext(ctx, "Confirmation successful",
			slog.String("shop_id", verified.Shop.ShopID),
			slog.String("shop_url", verified.Shop.ShopURL),
		)

		// Success response
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleWebhook(sdk *app.App, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Generate trace ID for request tracking
		traceID := r.Header.Get("X-Request-ID")
		if traceID == "" {
			traceID = fmt.Sprintf("webhook-%d", time.Now().UnixNano())
		}

		log.InfoContext(ctx, "Webhook request received", slog.String("trace_id", traceID))

		// Verify webhook
		verified, verifyErr := sdk.WebhookMiddleware().VerifyWebhook(ctx, r, traceID)
		if verifyErr != nil {
			handleSDKError(w, verifyErr, log)
			return
		}

		log.InfoContext(ctx, "Webhook verified successfully",
			slog.String("shop_id", verified.Shop.ShopID),
			slog.String("event", verified.Event.Data.Event),
			slog.String("trace_id", traceID),
		)

		log.InfoContext(ctx, "Processing webhook event",
			slog.String("event", verified.Event.Data.Event),
			slog.String("shop_id", verified.Shop.ShopID),
		)

		var changes []webhook.EntityChange
		if err := verified.Event.Data.UnmarshalPayload(&changes); err == nil {
			for _, change := range changes {
				log.InfoContext(ctx, "Entity change detected",
					slog.String("entity", change.Entity),
					slog.String("operation", change.Operation),
					slog.String("id", change.PrimaryKey),
					slog.Any("updated_fields", change.UpdatedFields),
				)

				if change.Operation == "update" || change.Operation == "insert" {
					go fetchEntityDetails(context.WithoutCancel(ctx), sdk, verified.Shop, change, log)
				}
			}
		}

		// Send success response
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleHealth(sdk *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check store health
		if healthErr := sdk.Store().HealthCheck(r.Context()); healthErr != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "unhealthy",
				"error":  healthErr.Error(),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now().Unix(),
		})
	}
}

// fetchEntityDetails loads the changed entity from the shop's Admin API. It
// runs after the webhook was answered, so the caller passes a context that is
// not cancelled with the request (context.WithoutCancel); it gets its own
// timeout here.
func fetchEntityDetails(ctx context.Context, sdk *app.App, shop *store.Shop, change webhook.EntityChange, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	client, err := sdk.NewClientForShop(shop)
	if err != nil {
		log.ErrorContext(ctx, "Failed to create Admin API client", slog.String("shop_id", shop.ShopID), slog.Any("error", err))
		return
	}

	// Entity names are snake_case ("sales_channel"), API paths use dashes.
	// Path escapes every segment, so IDs from the payload cannot change the path.
	path := adminapi.Path(strings.ReplaceAll(change.Entity, "_", "-"), change.PrimaryKey)
	resp, err := client.Get(ctx, path)
	if adminapi.IsNotFound(err) {
		log.InfoContext(ctx, "Entity was deleted in the meantime", slog.String("entity", change.Entity), slog.String("id", change.PrimaryKey))
		return
	}
	if err != nil {
		log.ErrorContext(ctx, "Failed to fetch entity", slog.String("path", path), slog.Any("error", err))
		return
	}

	var result struct {
		Data struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err = resp.JSON(&result); err != nil {
		log.ErrorContext(ctx, "Failed to decode entity", slog.String("path", path), slog.Any("error", err))
		return
	}

	log.InfoContext(ctx, "Entity fetched",
		slog.String("shop_id", shop.ShopID),
		slog.String("entity", change.Entity),
		slog.String("id", result.Data.ID),
		slog.String("name", result.Data.Name),
	)
}

func handleSDKError(w http.ResponseWriter, err error, log *slog.Logger) {
	var sdkErr *sdkerrors.Error
	if errors.As(err, &sdkErr) {
		log.Warn("SDK error",
			slog.String("type", sdkErr.Type),
			slog.String("message", sdkErr.Message),
			slog.Int("code", sdkErr.Code),
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(sdkErr.Code)
		if encodeErr := json.NewEncoder(w).Encode(sdkErr); encodeErr != nil {
			log.Error("Failed to encode error response", slog.Any("error", encodeErr))
		}
		return
	}

	log.Error("Internal error", slog.Any("error", err))
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
