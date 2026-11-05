package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopware/app-sdk-go/pkg/app"
	sdkerrors "github.com/shopware/app-sdk-go/pkg/errors"
	"github.com/shopware/app-sdk-go/pkg/internal/testutil"
	sdkresolver "github.com/shopware/app-sdk-go/pkg/resolver"
	"github.com/shopware/app-sdk-go/pkg/signature"
	"github.com/shopware/app-sdk-go/pkg/store"
	"github.com/shopware/app-sdk-go/pkg/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertInvalidRequestError(t *testing.T, err error, msgContains string) {
	t.Helper()
	sdkErr := testutil.AssertSDKError(t, err, sdkerrors.TypeInvalidRequest)
	if msgContains != "" {
		assert.Contains(t, sdkErr.Message, msgContains)
	}
}

func TestNew_ValidConfig(t *testing.T) {
	// Setup
	config := app.Config{
		AppName:         "TestApp",
		AppSecret:       "test-secret-1234567890",
		ConfirmationURL: "https://test.com/confirm",
		ShopStore:       store.NewInMemoryStore(),
	}

	// Test
	sdk, err := app.New(config)

	// Assert
	require.NoError(t, err)
	assert.NotNil(t, sdk)
	assert.NotNil(t, sdk.RegistrationHandler())
	assert.NotNil(t, sdk.WebhookMiddleware())
	assert.NotNil(t, sdk.Resolver())
	assert.NotNil(t, sdk.Verifier())
	assert.NotNil(t, sdk.Store())

	// Verify config values
	returnedConfig := sdk.Config()
	assert.Equal(t, "TestApp", returnedConfig.AppName)
	assert.Equal(t, "test-secret-1234567890", returnedConfig.AppSecret)
	assert.Equal(t, "https://test.com/confirm", returnedConfig.ConfirmationURL)
}

func TestNew_MaxBodyBytesDefaultApplied(t *testing.T) {
	config := app.Config{
		AppName:   "TestApp",
		AppSecret: "test-secret-1234567890",
		ShopStore: store.NewInMemoryStore(),
		// MaxBodyBytes left at zero → should be normalized to the default.
	}

	sdk, err := app.New(config)
	require.NoError(t, err)
	assert.Equal(t, webhook.DefaultMaxBodyBytes, sdk.Config().MaxBodyBytes)
}

func TestNew_MaxBodyBytesCustomPreserved(t *testing.T) {
	config := app.Config{
		AppName:      "TestApp",
		AppSecret:    "test-secret-1234567890",
		ShopStore:    store.NewInMemoryStore(),
		MaxBodyBytes: 1234,
	}

	sdk, err := app.New(config)
	require.NoError(t, err)
	assert.Equal(t, int64(1234), sdk.Config().MaxBodyBytes)
}

func TestNew_MaxBodyBytesNegativeRejected(t *testing.T) {
	config := app.Config{
		AppName:      "TestApp",
		AppSecret:    "test-secret-1234567890",
		ShopStore:    store.NewInMemoryStore(),
		MaxBodyBytes: -1,
	}

	_, err := app.New(config)
	assertInvalidRequestError(t, err, "MaxBodyBytes")
}

func TestConfig_ReturnsCorrectValues(t *testing.T) {
	// Setup
	config := app.Config{

		AppName:         "MyTestApp",
		AppSecret:       "my-test-secret-1234567890",
		ConfirmationURL: "https://myapp.com/register/confirm",
		ShopStore:       store.NewInMemoryStore(),
	}

	// Test
	sdk, err := app.New(config)
	require.NoError(t, err)
	returnedConfig := sdk.Config()

	// Assert
	assert.Equal(t, config.AppName, returnedConfig.AppName)
	assert.Equal(t, config.AppSecret, returnedConfig.AppSecret)
	assert.Equal(t, config.ConfirmationURL, returnedConfig.ConfirmationURL)
}

func TestStore_ReturnsStoreInstance(t *testing.T) {
	// Setup
	config := app.Config{

		AppName:         "TestApp",
		AppSecret:       "test-secret-1234567890",
		ConfirmationURL: "https://test.com/confirm",
		ShopStore:       store.NewInMemoryStore(),
	}

	// Test
	sdk, err := app.New(config)
	require.NoError(t, err)
	returnedStore := sdk.Store()

	// Assert
	assert.NotNil(t, returnedStore)
	assert.Same(t, config.ShopStore, returnedStore)
}

func TestVerifier_ReturnsVerifierInstance(t *testing.T) {
	// Setup
	config := app.Config{

		AppName:         "TestApp",
		AppSecret:       "test-secret-1234567890",
		ConfirmationURL: "https://test.com/confirm",
		ShopStore:       store.NewInMemoryStore(),
	}

	// Test
	sdk, err := app.New(config)
	require.NoError(t, err)
	verifier := sdk.Verifier()

	// Assert
	assert.NotNil(t, verifier)

	// Test that verifier works
	secret := verifier.GenerateShopSecret()
	assert.NotEmpty(t, secret)
	assert.Len(t, secret, 64)
}

func TestRegistrationHandler_ReturnsHandler(t *testing.T) {
	// Setup
	config := app.Config{

		AppName:         "TestApp",
		AppSecret:       "test-secret-1234567890",
		ConfirmationURL: "https://test.com/confirm",
		ShopStore:       store.NewInMemoryStore(),
	}

	// Test
	sdk, err := app.New(config)
	require.NoError(t, err)
	handler := sdk.RegistrationHandler()

	// Assert
	assert.NotNil(t, handler)
}

func TestWebhookMiddleware_ReturnsMiddleware(t *testing.T) {
	// Setup
	config := app.Config{

		AppName:         "TestApp",
		AppSecret:       "test-secret-1234567890",
		ConfirmationURL: "https://test.com/confirm",
		ShopStore:       store.NewInMemoryStore(),
	}

	// Test
	sdk, err := app.New(config)
	require.NoError(t, err)
	middleware := sdk.WebhookMiddleware()

	// Assert
	assert.NotNil(t, middleware)
}

func TestResolver_ReturnsResolver(t *testing.T) {
	// Setup
	config := app.Config{

		AppName:         "TestApp",
		AppSecret:       "test-secret-1234567890",
		ConfirmationURL: "https://test.com/confirm",
		ShopStore:       store.NewInMemoryStore(),
	}

	// Test
	sdk, err := app.New(config)
	require.NoError(t, err)
	resolver := sdk.Resolver()

	// Assert
	assert.NotNil(t, resolver)
}

func TestNewClientForShop_Success(t *testing.T) {
	// Setup
	config := app.Config{

		AppName:         "TestApp",
		AppSecret:       "test-secret-1234567890",
		ConfirmationURL: "https://test.com/confirm",
		ShopStore:       store.NewInMemoryStore(),
	}

	sdk, err := app.New(config)
	require.NoError(t, err)

	// Create a shop
	shop := &store.Shop{
		ShopID:       "test-shop-123",
		ShopURL:      "https://test-shop.com",
		ShopSecret:   "test-shop-secret",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		Active:       true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	// Test
	client, err := sdk.NewClientForShop(shop)

	// Assert
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewClientForShop_NilShop(t *testing.T) {
	// Setup
	config := app.Config{

		AppName:         "TestApp",
		AppSecret:       "test-secret-1234567890",
		ConfirmationURL: "https://test.com/confirm",
		ShopStore:       store.NewInMemoryStore(),
	}

	sdk, err := app.New(config)
	require.NoError(t, err)

	// Test
	client, err := sdk.NewClientForShop(nil)

	// Assert: a nil shop is an error, not a panic
	require.Error(t, err)
	assert.Nil(t, client)
}

func TestNewClient_Success(t *testing.T) {
	// Setup
	config := app.Config{

		AppName:         "TestApp",
		AppSecret:       "test-secret-1234567890",
		ConfirmationURL: "https://test.com/confirm",
		ShopStore:       store.NewInMemoryStore(),
	}

	sdk, err := app.New(config)
	require.NoError(t, err)

	// Store a shop
	shop := &store.Shop{
		ShopID:       "test-shop-123",
		ShopURL:      "https://test-shop.com",
		ShopSecret:   "test-shop-secret",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		Active:       true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	err = config.ShopStore.UpdateShop(context.Background(), shop)
	require.NoError(t, err)

	// Test
	client, err := sdk.NewClient(context.Background(), "test-shop-123")

	// Assert
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewClient_ShopNotFound(t *testing.T) {
	// Setup
	config := app.Config{

		AppName:         "TestApp",
		AppSecret:       "test-secret-1234567890",
		ConfirmationURL: "https://test.com/confirm",
		ShopStore:       store.NewInMemoryStore(),
	}

	sdk, err := app.New(config)
	require.NoError(t, err)

	// Test - shop doesn't exist
	client, err := sdk.NewClient(context.Background(), "non-existent-shop")

	// Assert
	require.Error(t, err)
	assert.Nil(t, client)
}

func TestNew_ComponentsIntegration(t *testing.T) {
	// Setup
	config := app.Config{
		AppName:         "TestApp",
		AppSecret:       "test-secret-1234567890",
		ConfirmationURL: "https://test.com/confirm",
		ShopStore:       store.NewInMemoryStore(),
	}

	// Test - Create SDK and verify all components work together
	sdk, err := app.New(config)
	require.NoError(t, err)

	// Test 1: Registration handler can store a shop
	regReq := &struct {
		ShopID  string
		ShopURL string
	}{
		ShopID:  "integration-test-shop",
		ShopURL: "https://integration-test.com",
	}

	// Store shop through store
	shop := &store.Shop{
		ShopID:     regReq.ShopID,
		ShopURL:    regReq.ShopURL,
		ShopSecret: "integration-secret",
		Active:     false,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	err = sdk.Store().UpdateShop(context.Background(), shop)
	require.NoError(t, err)

	// Test 2: Store can retrieve the shop
	retrievedShop, err := sdk.Store().GetShopByID(context.Background(), regReq.ShopID)
	require.NoError(t, err)
	assert.Equal(t, regReq.ShopID, retrievedShop.ShopID)
	assert.Equal(t, regReq.ShopURL, retrievedShop.ShopURL)

	// Test 3: Can create client for the shop
	retrievedShop.ClientID = "test-client-id"
	retrievedShop.ClientSecret = "test-client-secret"
	retrievedShop.Active = true
	err = sdk.Store().UpdateShop(context.Background(), retrievedShop)
	require.NoError(t, err)

	client, err := sdk.NewClient(context.Background(), regReq.ShopID)
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNew_EmptyConfig(t *testing.T) {
	// Setup
	config := app.Config{
		// All empty
		AppName:         "",
		AppSecret:       "",
		ConfirmationURL: "",
		ShopStore:       store.NewInMemoryStore(),
	}

	// Test - SDK should still be created, validation happens at usage time
	sdk, err := app.New(config)

	// Assert - Should fail validation
	require.Error(t, err)
	assert.Nil(t, sdk)
	assertInvalidRequestError(t, err, "AppName is required")
}

func TestNew_AllComponentsInitialized(t *testing.T) {
	// Setup
	config := app.Config{
		AppName:         "TestApp",
		AppSecret:       "test-secret-1234567890",
		ConfirmationURL: "https://test.com/confirm",
		ShopStore:       store.NewInMemoryStore(),
	}

	// Test
	sdk, err := app.New(config)
	require.NoError(t, err)

	// Assert all components are properly initialized
	assert.NotNil(t, sdk.Config())
	assert.NotNil(t, sdk.Store())
	assert.NotNil(t, sdk.Verifier())
	assert.NotNil(t, sdk.RegistrationHandler())
	assert.NotNil(t, sdk.WebhookMiddleware())
	assert.NotNil(t, sdk.Resolver())

	// Verify they are working instances
	testShop := &store.Shop{
		ShopID:    "test",
		ShopURL:   "https://test.com",
		Active:    true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err = sdk.Store().UpdateShop(context.Background(), testShop)
	require.NoError(t, err)

	secret := sdk.Verifier().GenerateShopSecret()
	assert.Len(t, secret, 64)
}

func TestMultipleSDKInstances(t *testing.T) {
	// Test that multiple SDK instances wouldn't share global state and ensures encapsulation

	config1 := app.Config{
		AppName:         "App1",
		AppSecret:       "secret1-1234567890",
		ConfirmationURL: "https://app1.com/confirm",
		ShopStore:       store.NewInMemoryStore(),
	}
	config2 := app.Config{
		AppName:         "App2",
		AppSecret:       "secret2-1234567890",
		ConfirmationURL: "https://app2.com/confirm",
		ShopStore:       store.NewInMemoryStore(),
	}

	// Create two SDK instances
	sdk1, err := app.New(config1)
	require.NoError(t, err)
	sdk2, err := app.New(config2)
	require.NoError(t, err)

	// Assert they are independent
	assert.NotNil(t, sdk1)
	assert.NotNil(t, sdk2)
	assert.NotEqual(t, sdk1.Config().AppName, sdk2.Config().AppName)
	assert.NotSame(t, sdk1.Store(), sdk2.Store())

	// Store shop in sdk1
	shop1 := &store.Shop{
		ShopID:    "shop1",
		ShopURL:   "https://shop1.com",
		Active:    true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err = sdk1.Store().UpdateShop(context.Background(), shop1)
	require.NoError(t, err)

	// Verify shop1 exists in sdk1 but not in sdk2
	_, err = sdk1.Store().GetShopByID(context.Background(), "shop1")
	require.NoError(t, err)

	_, err = sdk2.Store().GetShopByID(context.Background(), "shop1")
	require.Error(t, err) // Should not exist in sdk2
}

func TestConfig_Validate_Success(t *testing.T) {
	config := app.Config{
		AppName:         "ValidApp",
		AppSecret:       "valid-secret-1234567890",
		ConfirmationURL: "https://example.com/confirm",
		ShopStore:       store.NewInMemoryStore(),
	}

	err := config.Validate()
	require.NoError(t, err)
}

func TestConfig_Validate_EmptyAppName(t *testing.T) {
	config := app.Config{
		AppName:   "",
		AppSecret: "valid-secret-1234567890",
		ShopStore: store.NewInMemoryStore(),
	}

	err := config.Validate()
	require.Error(t, err)
	assertInvalidRequestError(t, err, "AppName is required")
}

func TestConfig_Validate_WhitespaceAppName(t *testing.T) {
	config := app.Config{
		AppName:   "   ",
		AppSecret: "valid-secret-1234567890",
		ShopStore: store.NewInMemoryStore(),
	}

	err := config.Validate()
	require.Error(t, err)
	assertInvalidRequestError(t, err, "AppName is required")
}

func TestConfig_Validate_EmptyAppSecret(t *testing.T) {
	config := app.Config{
		AppName:   "ValidApp",
		AppSecret: "",
		ShopStore: store.NewInMemoryStore(),
	}

	err := config.Validate()
	require.Error(t, err)
	assertInvalidRequestError(t, err, "AppSecret is required")
}

func TestConfig_Validate_ShortAppSecret(t *testing.T) {
	config := app.Config{
		AppName:   "ValidApp",
		AppSecret: "short",
		ShopStore: store.NewInMemoryStore(),
	}

	err := config.Validate()
	require.Error(t, err)
	assertInvalidRequestError(t, err, "AppSecret must be at least")
}

func TestConfig_Validate_InvalidConfirmationURL(t *testing.T) {
	config := app.Config{
		AppName:         "ValidApp",
		AppSecret:       "valid-secret-1234567890",
		ConfirmationURL: "not-a-valid-url",
		ShopStore:       store.NewInMemoryStore(),
	}

	err := config.Validate()
	require.Error(t, err)
	assertInvalidRequestError(t, err, "http or https scheme")
}

func TestConfig_Validate_UnparseableConfirmationURL(t *testing.T) {
	// A control character makes url.Parse itself fail (as opposed to a URL that
	// parses but has the wrong scheme/host), exercising the Wrap error branch.
	config := app.Config{
		AppName:         "ValidApp",
		AppSecret:       "valid-secret-1234567890",
		ConfirmationURL: "http://exa\x7fmple.com",
		ShopStore:       store.NewInMemoryStore(),
	}

	err := config.Validate()
	require.Error(t, err)
	assertInvalidRequestError(t, err, "ConfirmationURL must be a valid URL")
}

func TestConfig_Validate_InvalidConfirmationURLScheme(t *testing.T) {
	config := app.Config{
		AppName:         "ValidApp",
		AppSecret:       "valid-secret-1234567890",
		ConfirmationURL: "ftp://example.com/confirm",
		ShopStore:       store.NewInMemoryStore(),
	}

	err := config.Validate()
	require.Error(t, err)
	assertInvalidRequestError(t, err, "ConfirmationURL must use http or https scheme")
}

func TestConfig_Validate_EmptyConfirmationURL(t *testing.T) {
	config := app.Config{
		AppName:         "ValidApp",
		AppSecret:       "valid-secret-1234567890",
		ConfirmationURL: "",
		ShopStore:       store.NewInMemoryStore(),
	}

	err := config.Validate()
	require.NoError(t, err)
}

func TestConfig_Validate_AppName(t *testing.T) {
	tests := []struct {
		name        string
		appName     string
		wantErr     bool
		errContains string
	}{
		// Valid cases
		{name: "valid simple", appName: "ValidApp", wantErr: false},
		{name: "valid with hyphens", appName: "valid-app", wantErr: false},
		{name: "valid with underscores", appName: "valid_app", wantErr: false},
		{name: "valid mixed", appName: "Valid-App_123", wantErr: false},
		{name: "valid uppercase", appName: "APP123", wantErr: false},
		{name: "valid multiple hyphens", appName: "app-name-with-hyphens", wantErr: false},
		{name: "valid multiple underscores", appName: "app_name_with_underscores", wantErr: false},
		{name: "valid complex", appName: "MixedCase-App_Name123", wantErr: false},

		// Invalid cases
		{name: "invalid spaces", appName: "App Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid @", appName: "App@Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid dot", appName: "App.Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid slash", appName: "App/Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid backslash", appName: "App\\Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid exclamation", appName: "App Name!", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid hash", appName: "App#Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid dollar", appName: "App$Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid percent", appName: "App%Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid ampersand", appName: "App&Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid asterisk", appName: "App*Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid parentheses", appName: "App(Name)", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid equals", appName: "App=Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid plus", appName: "App+Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid brackets", appName: "App[Name]", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid braces", appName: "App{Name}", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid pipe", appName: "App|Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid colon", appName: "App:Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid semicolon", appName: "App;Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid single quote", appName: "App'Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid double quote", appName: "App\"Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid angle brackets", appName: "App<Name>", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid comma", appName: "App,Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid question mark", appName: "App?Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid tilde", appName: "App~Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
		{name: "invalid backtick", appName: "App`Name", wantErr: true, errContains: "alphanumeric characters, hyphens, and underscores"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := app.Config{
				AppName:   tt.appName,
				AppSecret: "valid-secret-1234567890",
				ShopStore: store.NewInMemoryStore(),
			}

			err := config.Validate()

			if tt.wantErr {
				require.Error(t, err)
				assertInvalidRequestError(t, err, tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfig_Validate_AppSecret(t *testing.T) {
	tests := []struct {
		name        string
		appSecret   string
		wantErr     bool
		errContains string
	}{
		// Valid cases
		{name: "valid with hyphens", appSecret: "valid-secret-1234567890", wantErr: false},
		{name: "valid with special chars", appSecret: "Secret!@#$%^&*()123", wantErr: false},
		{name: "valid with brackets", appSecret: "Secret_+-=[]{}1234", wantErr: false},
		{name: "valid with punctuation", appSecret: "Secret;':\"\\|,.<>/?1", wantErr: false},
		{name: "valid mixed case", appSecret: "MixedCase123!@#$", wantErr: false},
		{name: "valid lowercase", appSecret: "abcdefghijklmnopqrstuvwxyz", wantErr: false},
		{name: "valid uppercase", appSecret: "ABCDEFGHIJKLMNOPQRSTUVWXYZ", wantErr: false},
		{name: "valid complex", appSecret: "0123456789!@#$%^&*()_+-=", wantErr: false},

		// Invalid cases
		{name: "invalid null byte", appSecret: "secret-with\x00null-byte", wantErr: true, errContains: "null bytes"},
		{name: "invalid newline", appSecret: "secret-with\nnewline-1234567890", wantErr: true, errContains: "invalid characters"},
		{name: "invalid tab", appSecret: "secret-with\ttab-1234567890", wantErr: true, errContains: "invalid characters"},
		{name: "invalid carriage return", appSecret: "secret-with\rcarriage-return-1234567890", wantErr: true, errContains: "invalid characters"},
		{name: "invalid spaces", appSecret: "secret with spaces 1234567890", wantErr: true, errContains: "invalid characters"},
		{name: "invalid unicode trademark", appSecret: "secret™trademark1234567890", wantErr: true, errContains: "invalid characters"},
		{name: "invalid unicode copyright", appSecret: "secret©copyright1234567890", wantErr: true, errContains: "invalid characters"},
		{name: "invalid unicode euro", appSecret: "secret€euro-1234567890", wantErr: true, errContains: "invalid characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := app.Config{
				AppName:   "ValidApp",
				AppSecret: tt.appSecret,
				ShopStore: store.NewInMemoryStore(),
			}

			err := config.Validate()

			if tt.wantErr {
				require.Error(t, err)
				assertInvalidRequestError(t, err, tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfig_Validate_ConfirmationURL(t *testing.T) {
	tests := []struct {
		name            string
		confirmationURL string
		wantErr         bool
		errContains     string
	}{
		// Valid cases
		{name: "valid https", confirmationURL: "https://example.com/confirm", wantErr: false},
		{name: "valid http", confirmationURL: "http://example.com/confirm", wantErr: false},
		{name: "valid with port", confirmationURL: "https://example.com:8080/confirm", wantErr: false},
		{name: "valid with path", confirmationURL: "https://example.com/app/register/confirm", wantErr: false},
		{name: "valid with query", confirmationURL: "https://example.com/confirm?param=value", wantErr: false},
		{name: "empty is valid", confirmationURL: "", wantErr: false},

		// Invalid cases
		{name: "invalid no scheme", confirmationURL: "example.com/confirm", wantErr: true, errContains: "http or https scheme"},
		{name: "invalid no host", confirmationURL: "https:///confirm", wantErr: true, errContains: "valid host"},
		{name: "invalid ftp scheme", confirmationURL: "ftp://example.com/confirm", wantErr: true, errContains: "http or https scheme"},
		{name: "invalid file scheme", confirmationURL: "file:///path/to/file", wantErr: true, errContains: "http or https scheme"},
		{name: "invalid mailto scheme", confirmationURL: "mailto:user@example.com", wantErr: true, errContains: "http or https scheme"},
		{name: "invalid tel scheme", confirmationURL: "tel:+1234567890", wantErr: true, errContains: "http or https scheme"},
		{name: "invalid data scheme", confirmationURL: "data:text/plain;base64,SGVsbG8=", wantErr: true, errContains: "http or https scheme"},
		{name: "invalid javascript scheme", confirmationURL: "javascript:alert('xss')", wantErr: true, errContains: "http or https scheme"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := app.Config{
				AppName:         "ValidApp",
				AppSecret:       "valid-secret-1234567890",
				ConfirmationURL: tt.confirmationURL,
				ShopStore:       store.NewInMemoryStore(),
			}

			err := config.Validate()

			if tt.wantErr {
				require.Error(t, err)
				assertInvalidRequestError(t, err, tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfig_Validate_MissingShopStore(t *testing.T) {
	config := app.Config{
		AppName:   "ValidApp",
		AppSecret: "valid-secret-1234567890",
		ShopStore: nil,
	}

	err := config.Validate()
	require.Error(t, err)
	assertInvalidRequestError(t, err, "ShopStore is required")
}

// A negative duration in a policy would silently invert the freshness window,
// so each of the six fields is rejected rather than normalized.
func TestConfig_Validate_NegativePolicyDurations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mutate      func(*app.Config)
		errContains string
	}{
		{
			name:        "QueryPolicy.MaxAge",
			mutate:      func(c *app.Config) { c.QueryPolicy = sdkresolver.QueryPolicy{MaxAge: -time.Second} },
			errContains: "QueryPolicy.MaxAge must not be negative",
		},
		{
			name:        "QueryPolicy.ClockSkew",
			mutate:      func(c *app.Config) { c.QueryPolicy = sdkresolver.QueryPolicy{ClockSkew: -time.Second} },
			errContains: "QueryPolicy.ClockSkew must not be negative",
		},
		{
			name:        "PostPolicy.MaxAge",
			mutate:      func(c *app.Config) { c.PostPolicy = sdkresolver.PostPolicy{MaxAge: -time.Second} },
			errContains: "PostPolicy.MaxAge must not be negative",
		},
		{
			name:        "PostPolicy.ClockSkew",
			mutate:      func(c *app.Config) { c.PostPolicy = sdkresolver.PostPolicy{ClockSkew: -time.Second} },
			errContains: "PostPolicy.ClockSkew must not be negative",
		},
		{
			name: "WebhookPolicy.Freshness.MaxAge",
			mutate: func(c *app.Config) {
				c.WebhookPolicy = webhook.Policy{Freshness: signature.FreshnessPolicy{MaxAge: -time.Second}}
			},
			errContains: "WebhookPolicy.Freshness.MaxAge must not be negative",
		},
		{
			name: "WebhookPolicy.Freshness.ClockSkew",
			mutate: func(c *app.Config) {
				c.WebhookPolicy = webhook.Policy{Freshness: signature.FreshnessPolicy{ClockSkew: -time.Second}}
			},
			errContains: "WebhookPolicy.Freshness.ClockSkew must not be negative",
		},
		{
			name:        "LifecyclePolicy.MaxAge",
			mutate:      func(c *app.Config) { c.LifecyclePolicy = sdkresolver.LifecyclePolicy{MaxAge: -time.Second} },
			errContains: "LifecyclePolicy.MaxAge must not be negative",
		},
		{
			name: "LifecyclePolicy.ClockSkew",
			mutate: func(c *app.Config) {
				c.LifecyclePolicy = sdkresolver.LifecyclePolicy{ClockSkew: -time.Second}
			},
			errContains: "LifecyclePolicy.ClockSkew must not be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config := app.Config{
				AppName:   "ValidApp",
				AppSecret: "valid-secret-1234567890",
				ShopStore: store.NewInMemoryStore(),
			}
			require.NoError(t, config.Validate(), "baseline config must be valid")

			tt.mutate(&config)

			err := config.Validate()
			require.Error(t, err)
			assertInvalidRequestError(t, err, tt.errContains)
		})
	}
}

func TestConfig_Validate_ZeroPoliciesAreValid(t *testing.T) {
	t.Parallel()

	// The zero value of every policy means "use the SDK default", so it must
	// pass validation — otherwise Config{} would be unusable.
	config := app.Config{
		AppName:         "ValidApp",
		AppSecret:       "valid-secret-1234567890",
		ShopStore:       store.NewInMemoryStore(),
		QueryPolicy:     sdkresolver.QueryPolicy{},
		PostPolicy:      sdkresolver.PostPolicy{},
		WebhookPolicy:   webhook.Policy{},
		LifecyclePolicy: sdkresolver.LifecyclePolicy{},
	}

	require.NoError(t, config.Validate())
}
