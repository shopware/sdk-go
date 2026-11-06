package testutil_test

import (
	"testing"

	"github.com/shopware/sdk-go/internal/testutil"
	"github.com/shopware/sdk-go/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTestShop_Defaults(t *testing.T) {
	shop := testutil.NewTestShop()

	assert.Equal(t, "test-shop-123", shop.ShopID)
	assert.Equal(t, "https://test-shop.com", shop.ShopURL)
	assert.Equal(t, "test-secret-456", shop.ShopSecret)
	assert.Equal(t, "test-client-id", shop.ClientID)
	assert.Equal(t, "test-client-secret", shop.ClientSecret)
	assert.True(t, shop.Active)
	assert.True(t, shop.RegistrationConfirmed)
	assert.NotZero(t, shop.CreatedAt)
	assert.NotZero(t, shop.UpdatedAt)
}

func TestNewTestShop_WithOptions(t *testing.T) {
	shop := testutil.NewTestShop(
		testutil.WithShopID("custom-shop"),
		testutil.WithShopURL("https://custom.com"),
		testutil.WithShopSecret("custom-secret"),
		testutil.WithShopActive(false),
		testutil.WithRegistrationConfirmed(false),
		testutil.WithClientID("custom-client-id"),
		testutil.WithClientSecret("custom-client-secret"),
	)

	assert.Equal(t, "custom-shop", shop.ShopID)
	assert.Equal(t, "https://custom.com", shop.ShopURL)
	assert.Equal(t, "custom-secret", shop.ShopSecret)
	assert.Equal(t, "custom-client-id", shop.ClientID)
	assert.Equal(t, "custom-client-secret", shop.ClientSecret)
	assert.False(t, shop.Active)
	assert.False(t, shop.RegistrationConfirmed)
}

func TestNewTestWebhookEvent_Defaults(t *testing.T) {
	event := testutil.NewTestWebhookEvent()

	assert.Equal(t, "test-shop-123", event.Source.ShopID)
	assert.Equal(t, "https://test-shop.local", event.Source.URL)
	assert.Equal(t, "test.event", event.Data.Event)
	assert.NotNil(t, event.Data.Payload)

	var changes []webhook.EntityChange
	require.NoError(t, event.Data.UnmarshalPayload(&changes))
	require.Len(t, changes, 1)
	assert.Equal(t, "test_entity", changes[0].Entity)
	assert.Equal(t, "insert", changes[0].Operation)
	assert.Equal(t, "test-123", changes[0].PrimaryKey)
	assert.NotZero(t, event.Timestamp)
}

func TestNewTestWebhookEvent_WithOptions(t *testing.T) {
	event := testutil.NewTestWebhookEvent(
		testutil.WithShopIDInEvent("custom-shop"),
		testutil.WithShopURLInEvent("https://custom.com"),
		testutil.WithEventName("product.updated"),
		testutil.WithEntityData("product", "update", "prod-1", "prod-2"),
	)

	assert.Equal(t, "custom-shop", event.Source.ShopID)
	assert.Equal(t, "https://custom.com", event.Source.URL)
	assert.Equal(t, "product.updated", event.Data.Event)

	var changes []webhook.EntityChange
	require.NoError(t, event.Data.UnmarshalPayload(&changes))
	require.Len(t, changes, 2)
	assert.Equal(t, "product", changes[0].Entity)
	assert.Equal(t, "update", changes[0].Operation)
	assert.Equal(t, "prod-1", changes[0].PrimaryKey)
	assert.Equal(t, "prod-2", changes[1].PrimaryKey)
}
