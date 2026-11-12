package response_test

import (
	"encoding/json"
	"testing"

	"github.com/shopware/sdk-go/response"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func marshal(t *testing.T, v any) string {
	t.Helper()

	data, err := json.Marshal(v)
	require.NoError(t, err)

	return string(data)
}

// TestNotificationResponse tests the base notification response function
func TestNotificationResponse(t *testing.T) {
	t.Parallel()

	resp := response.NotificationResponse("success", "Operation completed")

	assert.Equal(t, "notification", resp.ActionType)
	assert.Equal(t, "success", resp.Status)
	assert.Equal(t, "Operation completed", resp.Message)
	assert.Nil(t, resp.Payload)
}

// TestNotifications_JSON checks that status and message are sent inside
// "payload", where Shopware reads them.
func TestNotifications_JSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response response.ActionButtonResponse
		want     string
	}{
		{
			name:     "success",
			response: response.NotificationSuccess("Item created successfully"),
			want:     `{"actionType":"notification","payload":{"status":"success","message":"Item created successfully"}}`,
		},
		{
			name:     "error",
			response: response.NotificationError("Failed to process request"),
			want:     `{"actionType":"notification","payload":{"status":"error","message":"Failed to process request"}}`,
		},
		{
			name:     "warning",
			response: response.NotificationWarning("This action cannot be undone"),
			want:     `{"actionType":"notification","payload":{"status":"warning","message":"This action cannot be undone"}}`,
		},
		{
			name:     "info",
			response: response.NotificationInfo("Processing will continue in the background"),
			want:     `{"actionType":"notification","payload":{"status":"info","message":"Processing will continue in the background"}}`,
		},
		{
			name:     "empty message",
			response: response.NotificationSuccess(""),
			want:     `{"actionType":"notification","payload":{"status":"success"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.JSONEq(t, tt.want, marshal(t, tt.response))
		})
	}
}

// TestNotificationBuilders checks the fields set by each notification builder.
func TestNotificationBuilders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		response   response.ActionButtonResponse
		wantStatus string
	}{
		{name: "success", response: response.NotificationSuccess("m"), wantStatus: response.NotificationStatusSuccess},
		{name: "error", response: response.NotificationError("m"), wantStatus: response.NotificationStatusError},
		{name: "warning", response: response.NotificationWarning("m"), wantStatus: response.NotificationStatusWarning},
		{name: "info", response: response.NotificationInfo("m"), wantStatus: response.NotificationStatusInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, response.ActionTypeNotification, tt.response.ActionType)
			assert.Equal(t, tt.wantStatus, tt.response.Status)
			assert.Equal(t, "m", tt.response.Message)
			assert.Nil(t, tt.response.Payload)
		})
	}
}

// TestReloadResponse tests reload action response
func TestReloadResponse(t *testing.T) {
	t.Parallel()

	resp := response.ReloadResponse()

	assert.Equal(t, "reload", resp.ActionType)
	assert.Empty(t, resp.Status)
	assert.Empty(t, resp.Message)
	assert.Nil(t, resp.Payload)

	// Shopware rejects action button responses without a "payload" key.
	assert.JSONEq(t, `{"actionType":"reload","payload":{}}`, marshal(t, resp))
}

// TestOpenNewTabResponse tests opening a new tab
func TestOpenNewTabResponse(t *testing.T) {
	t.Parallel()

	resp := response.OpenNewTabResponse("https://example.com/external")

	assert.Equal(t, "openNewTab", resp.ActionType)

	payload, ok := resp.Payload.(map[string]string)
	require.True(t, ok, "Payload should be a map[string]string")
	assert.Equal(t, "https://example.com/external", payload["redirectUrl"])

	assert.JSONEq(t,
		`{"actionType":"openNewTab","payload":{"redirectUrl":"https://example.com/external"}}`,
		marshal(t, resp))
}

// TestOpenNewTabResponse_EmptyURL tests opening a new tab with empty URL
func TestOpenNewTabResponse_EmptyURL(t *testing.T) {
	t.Parallel()

	resp := response.OpenNewTabResponse("")

	assert.Equal(t, "openNewTab", resp.ActionType)
	payload, ok := resp.Payload.(map[string]string)
	require.True(t, ok)
	assert.Empty(t, payload["redirectUrl"])
}

// TestOpenModalResponse tests opening a modal
func TestOpenModalResponse(t *testing.T) {
	t.Parallel()

	resp := response.OpenModalResponse("https://example.com/modal-content")

	assert.Equal(t, "openModal", resp.ActionType)

	payload, ok := resp.Payload.(map[string]any)
	require.True(t, ok, "Payload should be a map[string]any")
	assert.Equal(t, "https://example.com/modal-content", payload["iframeUrl"])
	assert.Equal(t, "medium", payload["size"])
	assert.Equal(t, false, payload["expand"])

	assert.JSONEq(t,
		`{"actionType":"openModal","payload":{"iframeUrl":"https://example.com/modal-content","size":"medium","expand":false}}`,
		marshal(t, resp))
}

// TestOpenModalResponse_EmptyURL tests opening a modal with empty URL
func TestOpenModalResponse_EmptyURL(t *testing.T) {
	t.Parallel()

	resp := response.OpenModalResponse("")

	payload, ok := resp.Payload.(map[string]any)
	require.True(t, ok)
	assert.Empty(t, payload["iframeUrl"])
}

// TestActionButtonResponse_MarshalJSON covers responses built by hand.
func TestActionButtonResponse_MarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response response.ActionButtonResponse
		want     string
	}{
		{
			name:     "only action type",
			response: response.ActionButtonResponse{ActionType: response.ActionTypeReload},
			want:     `{"actionType":"reload","payload":{}}`,
		},
		{
			name: "payload wins over status and message",
			response: response.ActionButtonResponse{
				ActionType: response.ActionTypeOpenNewTab,
				Status:     "success",
				Message:    "ignored",
				Payload:    map[string]string{"redirectUrl": "https://example.com"},
			},
			want: `{"actionType":"openNewTab","payload":{"redirectUrl":"https://example.com"}}`,
		},
		{
			name: "status and message go into payload",
			response: response.ActionButtonResponse{
				ActionType: response.ActionTypeNotification,
				Status:     "error",
				Message:    "boom",
			},
			want: `{"actionType":"notification","payload":{"status":"error","message":"boom"}}`,
		},
		{
			name:     "builder result",
			response: response.NotificationInfo("hi"),
			want:     `{"actionType":"notification","payload":{"status":"info","message":"hi"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.JSONEq(t, tt.want, marshal(t, tt.response))
			assert.JSONEq(t, tt.want, marshal(t, &tt.response), "a pointer is encoded the same way")
		})
	}
}

// TestPaymentResponses checks the status sent for each payment builder.
// Shopware uses the status as a transition name; an empty status keeps the
// payment open.
func TestPaymentResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response response.PaymentResponse
		want     string
	}{
		{name: "paid", response: response.PaymentPaid(), want: `{"status":"paid"}`},
		{name: "open", response: response.PaymentOpen(), want: `{}`},
		{
			name:     "failed",
			response: response.PaymentFailed("Insufficient funds"),
			want:     `{"status":"fail","message":"Insufficient funds"}`,
		},
		{name: "failed without message", response: response.PaymentFailed(""), want: `{"status":"fail"}`},
		{name: "cancelled", response: response.PaymentCancelled(), want: `{"status":"cancel"}`},
		{name: "authorized", response: response.PaymentAuthorized(), want: `{"status":"authorize"}`},
		{
			name:     "redirect",
			response: response.PaymentRedirect("https://payment-gateway.com/checkout?id=12345"),
			want:     `{"redirectUrl":"https://payment-gateway.com/checkout?id=12345"}`,
		},
		{name: "redirect without URL", response: response.PaymentRedirect(""), want: `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.JSONEq(t, tt.want, marshal(t, tt.response))
		})
	}
}

// TestRefundResponses checks the status sent for each refund builder.
func TestRefundResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response response.RefundResponse
		want     string
	}{
		{name: "success", response: response.RefundSuccess(), want: `{"status":"complete"}`},
		{
			name:     "failed",
			response: response.RefundFailed("Original transaction cannot be refunded"),
			want:     `{"status":"fail","message":"Original transaction cannot be refunded"}`,
		},
		{name: "failed without message", response: response.RefundFailed(""), want: `{"status":"fail"}`},
		{name: "processing", response: response.RefundProcessing(), want: `{"status":"process"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.JSONEq(t, tt.want, marshal(t, tt.response))
		})
	}
}
