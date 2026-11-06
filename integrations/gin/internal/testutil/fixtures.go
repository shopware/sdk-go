package testutil

import (
	"encoding/json"
	"time"

	"github.com/shopware/sdk-go/store"
	"github.com/shopware/sdk-go/webhook"
)

// ShopOption configures a test shop
type ShopOption func(*store.Shop)

// NewTestShop creates a test shop with sensible defaults
func NewTestShop(opts ...ShopOption) *store.Shop {
	shop := &store.Shop{
		ShopID:                "test-shop-123",
		ShopURL:               "https://test-shop.com",
		ShopSecret:            "test-secret-456",
		ClientID:              "test-client-id",
		ClientSecret:          "test-client-secret",
		Active:                true,
		RegistrationConfirmed: true,
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}
	for _, opt := range opts {
		opt(shop)
	}
	return shop
}

// WithShopID sets the shop ID
func WithShopID(id string) ShopOption {
	return func(s *store.Shop) { s.ShopID = id }
}

// WithShopURL sets the shop URL
func WithShopURL(url string) ShopOption {
	return func(s *store.Shop) { s.ShopURL = url }
}

// WithShopSecret sets the shop secret
func WithShopSecret(secret string) ShopOption {
	return func(s *store.Shop) { s.ShopSecret = secret }
}

// WithShopActive sets the shop active state
func WithShopActive(active bool) ShopOption {
	return func(s *store.Shop) { s.Active = active }
}

// WithRegistrationConfirmed sets whether registration has been confirmed
func WithRegistrationConfirmed(confirmed bool) ShopOption {
	return func(s *store.Shop) { s.RegistrationConfirmed = confirmed }
}

// WithClientID sets the client ID
func WithClientID(id string) ShopOption {
	return func(s *store.Shop) { s.ClientID = id }
}

// WithClientSecret sets the client secret
func WithClientSecret(secret string) ShopOption {
	return func(s *store.Shop) { s.ClientSecret = secret }
}

// WithPreviousShopSecret sets the previous shop secret kept after a rotation
func WithPreviousShopSecret(secret string) ShopOption {
	return func(s *store.Shop) { s.PreviousShopSecret = secret }
}

// WithSecretsRotatedAt sets when the shop secret was last rotated
func WithSecretsRotatedAt(at *time.Time) ShopOption {
	return func(s *store.Shop) { s.SecretsRotatedAt = at }
}

// EventOption configures a test webhook event
type EventOption func(*webhook.Event)

// NewTestWebhookEvent creates a test webhook event with sensible defaults
func NewTestWebhookEvent(opts ...EventOption) *webhook.Event {
	event := &webhook.Event{
		Source: webhook.Source{
			ShopID: "test-shop-123",
			URL:    "https://test-shop.local",
		},
		Data: webhook.Data{
			Event:   "test.event",
			Payload: mustMarshal([]webhook.EntityChange{{Entity: "test_entity", Operation: "insert", PrimaryKey: "test-123"}}),
		},
		Timestamp: time.Now().Unix(),
	}
	for _, opt := range opts {
		opt(event)
	}
	return event
}

// WithShopIDInEvent sets the shop ID in the event
func WithShopIDInEvent(id string) EventOption {
	return func(e *webhook.Event) { e.Source.ShopID = id }
}

// WithShopURLInEvent sets the shop URL in the event
func WithShopURLInEvent(url string) EventOption {
	return func(e *webhook.Event) { e.Source.URL = url }
}

// WithEventName sets the event name
func WithEventName(name string) EventOption {
	return func(e *webhook.Event) { e.Data.Event = name }
}

// WithEntityData sets entity change data as the payload
func WithEntityData(entity, operation string, ids ...string) EventOption {
	return func(e *webhook.Event) {
		changes := make([]webhook.EntityChange, len(ids))
		for i, id := range ids {
			changes[i] = webhook.EntityChange{
				Entity:     entity,
				Operation:  operation,
				PrimaryKey: id,
			}
		}
		e.Data.Payload = mustMarshal(changes)
	}
}

func mustMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
