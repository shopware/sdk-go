package webhook_test

import (
	"encoding/json"
	"testing"

	"github.com/shopware/sdk-go/internal/testutil"
	"github.com/shopware/sdk-go/sdkerrors"
	"github.com/shopware/sdk-go/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvent_Validate_Success(t *testing.T) {
	event := testutil.NewTestWebhookEvent(
		testutil.WithEventName("order.placed"),
		testutil.WithEntityData("order", "insert", "order-123"),
	)

	require.NoError(t, event.Validate())
}

func TestEvent_Validate_MissingShopID(t *testing.T) {
	event := &webhook.Event{
		Source: webhook.Source{ShopID: "", URL: "https://test-shop.com"},
		Data:   webhook.Data{Event: "order.placed"},
	}

	err := event.Validate()
	require.Error(t, err)
	var sdkErr *sdkerrors.Error
	require.ErrorAs(t, err, &sdkErr)
	assert.Equal(t, sdkerrors.TypeInvalidPayload, sdkErr.Type)
	assert.Equal(t, "shop ID is required", sdkErr.Message)
}

func TestEvent_Validate_MissingURL(t *testing.T) {
	event := &webhook.Event{
		Source: webhook.Source{ShopID: "test-shop-123", URL: ""},
		Data:   webhook.Data{Event: "order.placed"},
	}

	err := event.Validate()
	require.Error(t, err)
	var sdkErr *sdkerrors.Error
	require.ErrorAs(t, err, &sdkErr)
	assert.Equal(t, sdkerrors.TypeInvalidPayload, sdkErr.Type)
	assert.Equal(t, "shop URL is required", sdkErr.Message)
}

func TestEvent_Validate_MissingEventType(t *testing.T) {
	event := &webhook.Event{
		Source: webhook.Source{ShopID: "test-shop-123", URL: "https://test-shop.com"},
		Data:   webhook.Data{Event: ""},
	}

	err := event.Validate()
	require.Error(t, err)
	var sdkErr *sdkerrors.Error
	require.ErrorAs(t, err, &sdkErr)
	assert.Equal(t, sdkerrors.TypeInvalidPayload, sdkErr.Type)
	assert.Equal(t, "event type is required", sdkErr.Message)
}

func TestEvent_Validate_NilPayload(t *testing.T) {
	event := &webhook.Event{
		Source: webhook.Source{ShopID: "test-shop-123", URL: "https://test-shop.com"},
		Data:   webhook.Data{Event: "app.activated", Payload: nil},
	}
	require.NoError(t, event.Validate())
}

func TestEvent_Validate(t *testing.T) {
	tests := []struct {
		name         string
		event        webhook.Event
		expectError  bool
		expectedType string
		expectedMsg  string
	}{
		{
			name: "valid event with payload",
			event: webhook.Event{
				Source: webhook.Source{ShopID: "shop-123", URL: "https://shop.com"},
				Data:   webhook.Data{Event: "order.placed", Payload: json.RawMessage(`[{"entity":"order"}]`)},
			},
		},
		{
			name: "valid event nil payload",
			event: webhook.Event{
				Source: webhook.Source{ShopID: "shop-123", URL: "https://shop.com"},
				Data:   webhook.Data{Event: "app.activated"},
			},
		},
		{
			name: "valid event object payload",
			event: webhook.Event{
				Source: webhook.Source{ShopID: "shop-123", URL: "https://shop.com"},
				Data:   webhook.Data{Event: "app.deleted", Payload: json.RawMessage(`{"keepUserData":false}`)},
			},
		},
		{
			name: "missing shopId",
			event: webhook.Event{
				Source: webhook.Source{ShopID: "", URL: "https://shop.com"},
				Data:   webhook.Data{Event: "order.placed"},
			},
			expectError:  true,
			expectedType: sdkerrors.TypeInvalidPayload,
			expectedMsg:  "shop ID is required",
		},
		{
			name: "missing URL",
			event: webhook.Event{
				Source: webhook.Source{ShopID: "shop-123", URL: ""},
				Data:   webhook.Data{Event: "order.placed"},
			},
			expectError:  true,
			expectedType: sdkerrors.TypeInvalidPayload,
			expectedMsg:  "shop URL is required",
		},
		{
			name: "missing event type",
			event: webhook.Event{
				Source: webhook.Source{ShopID: "shop-123", URL: "https://shop.com"},
				Data:   webhook.Data{Event: ""},
			},
			expectError:  true,
			expectedType: sdkerrors.TypeInvalidPayload,
			expectedMsg:  "event type is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if tt.expectError {
				require.Error(t, err)
				var sdkErr *sdkerrors.Error
				require.ErrorAs(t, err, &sdkErr)
				assert.Equal(t, tt.expectedType, sdkErr.Type)
				assert.Equal(t, tt.expectedMsg, sdkErr.Message)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// UnmarshalPayload tests

func TestData_UnmarshalPayload_EntityChanges(t *testing.T) {
	raw := `{
		"event": "product.written",
		"payload": [
			{"entity":"product","operation":"update","primaryKey":"p-1","updatedFields":["price","stock"]},
			{"entity":"product","operation":"insert","primaryKey":"p-2"}
		]
	}`

	var data webhook.Data
	require.NoError(t, json.Unmarshal([]byte(raw), &data))

	var changes []webhook.EntityChange
	require.NoError(t, data.UnmarshalPayload(&changes))

	require.Len(t, changes, 2)
	assert.Equal(t, "product", changes[0].Entity)
	assert.Equal(t, "update", changes[0].Operation)
	assert.Equal(t, "p-1", changes[0].PrimaryKey)
	assert.Equal(t, []string{"price", "stock"}, changes[0].UpdatedFields)
	assert.Equal(t, "insert", changes[1].Operation)
	assert.Equal(t, "p-2", changes[1].PrimaryKey)
}

func TestData_UnmarshalPayload_ObjectPayload_AppDeleted(t *testing.T) {
	raw := `{
		"event": "app.deleted",
		"payload": {"keepUserData": false}
	}`

	var data webhook.Data
	require.NoError(t, json.Unmarshal([]byte(raw), &data))

	var payload struct {
		KeepUserData bool `json:"keepUserData"`
	}
	require.NoError(t, data.UnmarshalPayload(&payload))
	assert.False(t, payload.KeepUserData)
}

func TestData_UnmarshalPayload_ObjectPayload_MediaUploaded(t *testing.T) {
	raw := `{
		"event": "media.uploaded",
		"payload": {"mediaId": "019c7a51639d7020bff2bdd4e3a1ef5b"}
	}`

	var data webhook.Data
	require.NoError(t, json.Unmarshal([]byte(raw), &data))

	var payload struct {
		MediaID string `json:"mediaId"`
	}
	require.NoError(t, data.UnmarshalPayload(&payload))
	assert.Equal(t, "019c7a51639d7020bff2bdd4e3a1ef5b", payload.MediaID)
}

func TestData_UnmarshalPayload_EmptyArray(t *testing.T) {
	raw := `{"event": "app.activated", "payload": []}`

	var data webhook.Data
	require.NoError(t, json.Unmarshal([]byte(raw), &data))

	var changes []webhook.EntityChange
	require.NoError(t, data.UnmarshalPayload(&changes))
	assert.Empty(t, changes)
}

func TestData_UnmarshalPayload_NullPayload(t *testing.T) {
	raw := `{"event": "custom.event", "payload": null}`

	var data webhook.Data
	require.NoError(t, json.Unmarshal([]byte(raw), &data))

	var changes []webhook.EntityChange
	require.NoError(t, data.UnmarshalPayload(&changes))
	assert.Nil(t, changes)
}

func TestData_UnmarshalPayload_MissingPayload(t *testing.T) {
	raw := `{"event": "custom.event"}`

	var data webhook.Data
	require.NoError(t, json.Unmarshal([]byte(raw), &data))

	var changes []webhook.EntityChange
	require.NoError(t, data.UnmarshalPayload(&changes))
	assert.Nil(t, changes)
}

func TestEvent_FullUnmarshal_ObjectPayload(t *testing.T) {
	raw := `{
		"data": {"payload": {"keepUserData": false}, "event": "app.deleted"},
		"source": {"shopId": "shop-123", "url": "https://shop.example.com"},
		"timestamp": 1771498783
	}`

	var event webhook.Event
	require.NoError(t, json.Unmarshal([]byte(raw), &event))

	assert.Equal(t, "app.deleted", event.Data.Event)
	assert.Equal(t, "shop-123", event.Source.ShopID)
	assert.Equal(t, int64(1771498783), event.Timestamp)
	assert.JSONEq(t, `{"keepUserData": false}`, string(event.Data.Payload))
	require.NoError(t, event.Validate())
}

func TestEvent_FullUnmarshal_ArrayPayload(t *testing.T) {
	raw := `{
		"data": {"payload": [{"entity":"product","operation":"update","primaryKey":"p-1"}], "event": "product.written"},
		"source": {"shopId": "shop-123", "url": "https://shop.example.com"},
		"timestamp": 1771503599
	}`

	var event webhook.Event
	require.NoError(t, json.Unmarshal([]byte(raw), &event))

	assert.Equal(t, "product.written", event.Data.Event)

	var changes []webhook.EntityChange
	require.NoError(t, event.Data.UnmarshalPayload(&changes))
	require.Len(t, changes, 1)
	assert.Equal(t, "product", changes[0].Entity)
	assert.Equal(t, "p-1", changes[0].PrimaryKey)

	require.NoError(t, event.Validate())
}

func TestData_UnmarshalPayload_ObjectIntoSlice_ReturnsError(t *testing.T) {
	raw := `{"event": "app.deleted", "payload": {"keepUserData": false}}`

	var data webhook.Data
	require.NoError(t, json.Unmarshal([]byte(raw), &data))

	var changes []webhook.EntityChange
	err := data.UnmarshalPayload(&changes)
	require.Error(t, err)
}

func TestData_UnmarshalPayload_ArrayIntoStruct_ReturnsError(t *testing.T) {
	raw := `{"event": "product.written", "payload": [{"entity":"product","operation":"update","primaryKey":"p-1","updatedFields":["name"]}]}`

	var data webhook.Data
	require.NoError(t, json.Unmarshal([]byte(raw), &data))

	var p struct {
		KeepUserData bool `json:"keepUserData"`
	}
	err := data.UnmarshalPayload(&p)
	require.Error(t, err)
}

func TestData_UnmarshalPayload_InvalidJSON_ReturnsError(t *testing.T) {
	data := webhook.Data{
		Event:   "test",
		Payload: json.RawMessage(`{invalid`),
	}

	var target map[string]interface{}
	err := data.UnmarshalPayload(&target)
	require.Error(t, err)
}
