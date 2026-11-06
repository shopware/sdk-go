package app

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	adminstorage "github.com/shopware/sdk-go/clients/adminapi/tokenstorage"
	"github.com/shopware/sdk-go/registration"
	"github.com/shopware/sdk-go/resolver"
	"github.com/shopware/sdk-go/sdkerrors"
	"github.com/shopware/sdk-go/signature"
	"github.com/shopware/sdk-go/store"
	"github.com/shopware/sdk-go/webhook"
)

const (
	// MinAppNameLength is the minimum required length for AppName
	MinAppNameLength = 1
	// MinAppSecretLength is the minimum required length for AppSecret (security requirement)
	MinAppSecretLength = 8
)

var (
	// appNameRegex matches valid app names (alphanumeric, hyphens, underscores)
	// Must match Shopware's validation rules
	appNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

	// appSecretRegex ensures AppSecret contains only safe printable ASCII characters
	// Allows alphanumeric and common safe special characters, excludes whitespace and control characters
	appSecretRegex = regexp.MustCompile(`^[a-zA-Z0-9!@#$%^&*()_+\-=\[\]{};':"\\|,.<>/?]+$`)
)

// App is the main SDK entry point that ties all components together
type App struct {
	config              Config
	store               store.ShopStore
	verifier            signature.Verifier
	registrationHandler registration.HandlerInterface
	webhookMiddleware   *webhook.Middleware
	resolver            *resolver.Resolver
	tokenStorage        adminstorage.TokenStorage
	tokenLocker         adminstorage.Locker
}

// Config contains the configuration for the Shopware app
type Config struct {
	// AppName is the name of the app
	AppName string

	// AppSecret is the app secret
	AppSecret string

	// ConfirmationURL is the URL where Shopware will send the registration confirmation
	ConfirmationURL string

	// ShopStore is the repository for shop data
	// Required for storing shop credentials and state
	ShopStore store.ShopStore

	// AllowUnsignedReregistration re-enables a known-vulnerable path: it lets a confirmed
	// shop be re-registered without proving knowledge of its current shop secret, which
	// permits takeover by anyone holding the app secret (GHSA-c4p7-rwrg-pf6p).
	//
	// Needed only for shops on Shopware older than 6.6.10.15 / 6.7.8.1, which do not send
	// the shop signature on re-registration. It never applies to a shop that has already
	// proved knowledge of its secret, so an upgraded shop protects itself again.
	AllowUnsignedReregistration bool

	// InflightAllowance is the grace window for accepting signatures made with
	// the previous secret after a secret rotation. When <= 0,
	// signature.DefaultInflightAllowance (1 minute) is used.
	InflightAllowance time.Duration

	// MaxBodyBytes limits the size (in bytes) of incoming request bodies buffered
	// before authentication. This mitigates memory-exhaustion DoS via oversized
	// payloads on public endpoints. When 0, webhook.DefaultMaxBodyBytes (5 MiB) is used.
	MaxBodyBytes int64

	// QueryPolicy governs freshness and shop-state checks for signed GET query
	// authentication (admin modules, action button tabs, custom signed links).
	// The zero value enforces the strictest policy: a timestamp is required,
	// signed queries expire after 5 minutes, and inactive shops are rejected.
	QueryPolicy resolver.QueryPolicy

	// PostPolicy governs the freshness check for signed JSON POST authentication
	// that is not a webhook event (payment, tax provider, gateways). The zero
	// value checks the body's timestamp only when one is present, because
	// Shopware does not stamp these payloads.
	PostPolicy resolver.PostPolicy

	// WebhookPolicy governs the freshness check for webhook authentication.
	// The zero value requires a timestamp within 5 minutes, which is what
	// Shopware sends on every delivery attempt.
	WebhookPolicy webhook.Policy

	// LifecyclePolicy governs the freshness check on the activate, deactivate and
	// delete routes. The zero value requires a timestamp within 5 minutes. The
	// event binding those routes apply is not configurable.
	LifecyclePolicy resolver.LifecyclePolicy

	// TokenStorage keeps the Admin API tokens of the shops, so all clients of
	// a shop share one token. If nil, tokens are kept in memory and shared by
	// all clients this App creates. Use tokenstorage.NewKV from
	// github.com/shopware/sdk-go/clients/adminapi/tokenstorage to share
	// tokens between instances, for example in Redis or Valkey.
	TokenStorage adminstorage.TokenStorage

	// Locker serializes token fetches of a shop. If nil, the clients this App
	// creates fetch one token per shop at a time within this process. With a
	// TokenStorage shared by several instances, set a distributed Locker (for
	// example on Redis SET NX PX) to fetch one token per shop across all of
	// them. Without it each instance may fetch its own token once, which is
	// harmless: every token works.
	Locker adminstorage.Locker

	// HTTPClient sends the Admin API requests of the clients this App
	// creates. If nil, a client with a 30 second timeout is used.
	HTTPClient *http.Client

	// UserAgent replaces the default User-Agent header of Admin API requests.
	UserAgent string
}

// Validate checks if the configuration is valid
func (c Config) Validate() error {
	if strings.TrimSpace(c.AppName) == "" {
		return sdkerrors.NewInvalidRequest("AppName is required")
	}

	if len(c.AppName) < MinAppNameLength {
		return sdkerrors.NewInvalidRequest(fmt.Sprintf("AppName must be at least %d character(s)", MinAppNameLength))
	}

	if !appNameRegex.MatchString(c.AppName) {
		return sdkerrors.NewInvalidRequest("AppName must contain only alphanumeric characters, hyphens, and underscores")
	}

	if strings.TrimSpace(c.AppSecret) == "" {
		return sdkerrors.NewInvalidRequest("AppSecret is required")
	}

	if len(c.AppSecret) < MinAppSecretLength {
		return sdkerrors.NewInvalidRequest(
			fmt.Sprintf("AppSecret must be at least %d characters for security", MinAppSecretLength),
		)
	}

	if strings.ContainsRune(c.AppSecret, '\x00') {
		return sdkerrors.NewInvalidRequest("AppSecret must not contain null bytes")
	}

	if !appSecretRegex.MatchString(c.AppSecret) {
		return sdkerrors.NewInvalidRequest("AppSecret contains invalid characters")
	}

	if c.ShopStore == nil {
		return sdkerrors.NewInvalidRequest("ShopStore is required")
	}

	if c.MaxBodyBytes < 0 {
		return sdkerrors.NewInvalidRequest("MaxBodyBytes must not be negative")
	}

	if c.QueryPolicy.MaxAge < 0 {
		return sdkerrors.NewInvalidRequest("QueryPolicy.MaxAge must not be negative")
	}

	if c.QueryPolicy.ClockSkew < 0 {
		return sdkerrors.NewInvalidRequest("QueryPolicy.ClockSkew must not be negative")
	}

	if c.PostPolicy.MaxAge < 0 {
		return sdkerrors.NewInvalidRequest("PostPolicy.MaxAge must not be negative")
	}

	if c.PostPolicy.ClockSkew < 0 {
		return sdkerrors.NewInvalidRequest("PostPolicy.ClockSkew must not be negative")
	}

	if c.WebhookPolicy.Freshness.MaxAge < 0 {
		return sdkerrors.NewInvalidRequest("WebhookPolicy.Freshness.MaxAge must not be negative")
	}

	if c.WebhookPolicy.Freshness.ClockSkew < 0 {
		return sdkerrors.NewInvalidRequest("WebhookPolicy.Freshness.ClockSkew must not be negative")
	}

	if c.LifecyclePolicy.MaxAge < 0 {
		return sdkerrors.NewInvalidRequest("LifecyclePolicy.MaxAge must not be negative")
	}

	if c.LifecyclePolicy.ClockSkew < 0 {
		return sdkerrors.NewInvalidRequest("LifecyclePolicy.ClockSkew must not be negative")
	}

	if c.ConfirmationURL != "" {
		parsedURL, err := url.Parse(c.ConfirmationURL)
		if err != nil {
			return sdkerrors.Wrap(
				err,
				sdkerrors.TypeInvalidRequest,
				"ConfirmationURL must be a valid URL",
				http.StatusBadRequest,
			)
		}

		if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			return sdkerrors.NewInvalidRequest("ConfirmationURL must use http or https scheme")
		}

		if parsedURL.Host == "" {
			return sdkerrors.NewInvalidRequest("ConfirmationURL must have a valid host")
		}
	}

	return nil
}

// New creates a new Shopware app instance
func New(config Config) (*App, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Normalize the body-size limit so App.Config() always reports the effective
	// value (downstream readers such as the Gin confirmation handler rely on it).
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = webhook.DefaultMaxBodyBytes
	}

	verifier := signature.NewVerifier()
	dualVerifier := signature.NewDualVerifier(verifier, config.InflightAllowance)

	regConfig := registration.Config{
		AppName:                     config.AppName,
		AppSecret:                   config.AppSecret,
		AllowUnsignedReregistration: config.AllowUnsignedReregistration,
	}

	regHandler := registration.NewHandler(regConfig, config.ShopStore, verifier, dualVerifier)
	webhookMW := webhook.NewMiddleware(webhook.Config{
		ShopStore:    config.ShopStore,
		Verifier:     verifier,
		DualVerifier: dualVerifier,
		MaxBodyBytes: config.MaxBodyBytes,
		Policy:       config.WebhookPolicy,
	})
	ctxResolver := resolver.NewResolver(resolver.Config{
		ShopStore:         config.ShopStore,
		WebhookVerifier:   webhookMW,
		Verifier:          verifier,
		DualVerifier:      dualVerifier,
		InflightAllowance: config.InflightAllowance,
		MaxBodyBytes:      config.MaxBodyBytes,
		QueryPolicy:       config.QueryPolicy,
		PostPolicy:        config.PostPolicy,
		LifecyclePolicy:   config.LifecyclePolicy,
	})

	tokenStorage := config.TokenStorage
	if tokenStorage == nil {
		tokenStorage = adminstorage.NewInMemory()
	}
	var tokenLocker adminstorage.Locker = newKeyLocker()
	if config.Locker != nil {
		tokenLocker = config.Locker
	}

	return &App{
		config:   config,
		store:    config.ShopStore,
		verifier: verifier,
		registrationHandler: tokenCleanupHandler{
			HandlerInterface: regHandler,
			store:            config.ShopStore,
			storage:          tokenStorage,
			locker:           tokenLocker,
		},
		webhookMiddleware: webhookMW,
		resolver:          ctxResolver,
		tokenStorage:      tokenStorage,
		tokenLocker:       tokenLocker,
	}, nil
}

// RegistrationHandler returns the registration handler for HTTP routing
func (a *App) RegistrationHandler() registration.HandlerInterface {
	return a.registrationHandler
}

// WebhookMiddleware returns the webhook middleware for HTTP routing
func (a *App) WebhookMiddleware() *webhook.Middleware {
	return a.webhookMiddleware
}

// Resolver returns the context resolver for resolving shops from HTTP requests
func (a *App) Resolver() *resolver.Resolver {
	return a.resolver
}

// Verifier returns the signature verifier
func (a *App) Verifier() signature.Verifier {
	return a.verifier
}

// Store returns the shop store
func (a *App) Store() store.ShopStore {
	return a.store
}

// Config returns the app configuration
func (a *App) Config() Config {
	return a.config
}
