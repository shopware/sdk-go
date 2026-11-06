package webhook

import (
	"encoding/json"

	"github.com/shopware/sdk-go/sdkerrors"
)

// LifecycleEvent names an app lifecycle transition. Each lifecycle route accepts
// exactly one; see Resolver.ResolveLifecycleWebhook.
type LifecycleEvent string

// The lifecycle events Shopware delivers to an app's activate, deactivate and
// delete routes.
const (
	EventAppActivated   LifecycleEvent = "app.activated"
	EventAppDeactivated LifecycleEvent = "app.deactivated"
	EventAppDeleted     LifecycleEvent = "app.deleted"
)

// Event represents a webhook event from Shopware
type Event struct {
	Source    Source       `json:"source"`
	Data      Data         `json:"data"`
	Timestamp int64        `json:"timestamp"`
	RawBody   []byte       `json:"-"`
	Headers   EventHeaders `json:"-"`
}

// Source contains the source information of a webhook event
type Source struct {
	ShopID         string  `json:"shopId"`
	URL            string  `json:"url"`
	AppVersion     string  `json:"appVersion,omitempty"`
	InAppPurchases *string `json:"inAppPurchases,omitempty"`
}

// Data contains the event type and payload.
// Payload is the raw JSON from data.payload — its shape depends on the event type.
// For entity webhooks (e.g. product.written) it is a JSON array; for lifecycle or
// custom events (e.g. app.deleted, media.uploaded) it may be a JSON object.
// Use UnmarshalPayload to decode it into the appropriate type.
type Data struct {
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`
}

// UnmarshalPayload decodes the raw payload into the given target.
// For entity webhooks pass a *[]EntityChange; for app.deleted pass a struct
// with a KeepUserData field, etc.
func (d *Data) UnmarshalPayload(target interface{}) error {
	if len(d.Payload) == 0 || string(d.Payload) == "null" {
		return nil
	}
	return json.Unmarshal(d.Payload, target)
}

// EntityChange represents a single entity change in a webhook event.
// This is the payload shape for entity-write webhooks (e.g. product.written).
type EntityChange struct {
	Entity        string   `json:"entity"`
	Operation     string   `json:"operation"`
	PrimaryKey    string   `json:"primaryKey"`
	UpdatedFields []string `json:"updatedFields,omitempty"`
}

// EventHeaders contains HTTP headers from the webhook request.
type EventHeaders struct {
	ShopwareShopSignature string
	ShopwareVersion       string
	UserAgent             string
}

// Validate checks if the event has all required fields
func (e *Event) Validate() error {
	if e.Source.ShopID == "" {
		return sdkerrors.NewInvalidPayload("shop ID is required")
	}

	if e.Source.URL == "" {
		return sdkerrors.NewInvalidPayload("shop URL is required")
	}

	if e.Data.Event == "" {
		return sdkerrors.NewInvalidPayload("event type is required")
	}

	return nil
}
