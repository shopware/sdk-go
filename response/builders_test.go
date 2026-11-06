package response_test

import (
	"encoding/json"
	"testing"

	"github.com/shopware/sdk-go/response"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNotificationResponse tests the base notification response function
func TestNotificationResponse(t *testing.T) {
	resp := response.NotificationResponse("success", "Operation completed")

	assert.Equal(t, "notification", resp.ActionType)
	assert.Equal(t, "success", resp.Status)
	assert.Equal(t, "Operation completed", resp.Message)
	assert.Nil(t, resp.Payload)
}

// TestNotificationSuccess tests success notification
func TestNotificationSuccess(t *testing.T) {
	resp := response.NotificationSuccess("Item created successfully")

	assert.Equal(t, "notification", resp.ActionType)
	assert.Equal(t, "success", resp.Status)
	assert.Equal(t, "Item created successfully", resp.Message)

	// Verify JSON marshaling
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"actionType":"notification"`)
	assert.Contains(t, string(jsonData), `"status":"success"`)
	assert.Contains(t, string(jsonData), `"message":"Item created successfully"`)
}

// TestNotificationError tests error notification
func TestNotificationError(t *testing.T) {
	resp := response.NotificationError("Failed to process request")

	assert.Equal(t, "notification", resp.ActionType)
	assert.Equal(t, "error", resp.Status)
	assert.Equal(t, "Failed to process request", resp.Message)

	// Verify JSON marshaling
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"actionType":"notification"`)
	assert.Contains(t, string(jsonData), `"status":"error"`)
	assert.Contains(t, string(jsonData), `"message":"Failed to process request"`)
}

// TestNotificationWarning tests warning notification
func TestNotificationWarning(t *testing.T) {
	resp := response.NotificationWarning("This action cannot be undone")

	assert.Equal(t, "notification", resp.ActionType)
	assert.Equal(t, "warning", resp.Status)
	assert.Equal(t, "This action cannot be undone", resp.Message)

	// Verify JSON marshaling
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"actionType":"notification"`)
	assert.Contains(t, string(jsonData), `"status":"warning"`)
}

// TestNotificationInfo tests info notification
func TestNotificationInfo(t *testing.T) {
	resp := response.NotificationInfo("Processing will continue in the background")

	assert.Equal(t, "notification", resp.ActionType)
	assert.Equal(t, "info", resp.Status)
	assert.Equal(t, "Processing will continue in the background", resp.Message)

	// Verify JSON marshaling
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"actionType":"notification"`)
	assert.Contains(t, string(jsonData), `"status":"info"`)
}

// TestNotification_EmptyMessage tests notifications with empty messages
func TestNotification_EmptyMessage(t *testing.T) {
	resp := response.NotificationSuccess("")

	assert.Equal(t, "notification", resp.ActionType)
	assert.Equal(t, "success", resp.Status)
	assert.Empty(t, resp.Message)

	// Should still marshal correctly
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"actionType":"notification"`)
}

// TestReloadResponse tests reload action response
func TestReloadResponse(t *testing.T) {
	resp := response.ReloadResponse()

	assert.Equal(t, "reload", resp.ActionType)
	assert.Empty(t, resp.Status)
	assert.Empty(t, resp.Message)
	assert.Nil(t, resp.Payload)

	// Verify JSON marshaling
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"actionType":"reload"`)

	// Status and message should be omitted (omitempty)
	assert.NotContains(t, string(jsonData), `"status"`)
	assert.NotContains(t, string(jsonData), `"message"`)
}

// TestOpenNewTabResponse tests opening a new tab
func TestOpenNewTabResponse(t *testing.T) {
	resp := response.OpenNewTabResponse("https://example.com/external")

	assert.Equal(t, "openNewTab", resp.ActionType)
	assert.NotNil(t, resp.Payload)

	// Verify payload structure
	payload, ok := resp.Payload.(map[string]string)
	require.True(t, ok, "Payload should be a map[string]string")
	assert.Equal(t, "https://example.com/external", payload["redirectUrl"])

	// Verify JSON marshaling
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"actionType":"openNewTab"`)
	assert.Contains(t, string(jsonData), `"redirectUrl":"https://example.com/external"`)
}

// TestOpenNewTabResponse_EmptyURL tests opening a new tab with empty URL
func TestOpenNewTabResponse_EmptyURL(t *testing.T) {
	resp := response.OpenNewTabResponse("")

	assert.Equal(t, "openNewTab", resp.ActionType)
	payload, ok := resp.Payload.(map[string]string)
	require.True(t, ok)
	assert.Empty(t, payload["redirectUrl"])
}

// TestOpenModalResponse tests opening a modal
func TestOpenModalResponse(t *testing.T) {
	resp := response.OpenModalResponse("https://example.com/modal-content")

	assert.Equal(t, "openModal", resp.ActionType)
	assert.NotNil(t, resp.Payload)

	// Verify payload structure
	payload, ok := resp.Payload.(map[string]interface{})
	require.True(t, ok, "Payload should be a map[string]interface{}")
	assert.Equal(t, "https://example.com/modal-content", payload["iframeUrl"])
	assert.Equal(t, "medium", payload["size"])
	assert.Equal(t, false, payload["expand"])

	// Verify JSON marshaling
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"actionType":"openModal"`)
	assert.Contains(t, string(jsonData), `"iframeUrl":"https://example.com/modal-content"`)
	assert.Contains(t, string(jsonData), `"size":"medium"`)
	assert.Contains(t, string(jsonData), `"expand":false`)
}

// TestOpenModalResponse_EmptyURL tests opening a modal with empty URL
func TestOpenModalResponse_EmptyURL(t *testing.T) {
	resp := response.OpenModalResponse("")

	payload, ok := resp.Payload.(map[string]interface{})
	require.True(t, ok)
	assert.Empty(t, payload["iframeUrl"])
}

// TestPaymentPaid tests successful payment response
func TestPaymentPaid(t *testing.T) {
	resp := response.PaymentPaid()

	assert.Equal(t, "paid", resp.Status)
	assert.Empty(t, resp.Message)
	assert.Empty(t, resp.RedirectURL)

	// Verify JSON marshaling
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"status":"paid"`)
	assert.NotContains(t, string(jsonData), `"message"`)
	assert.NotContains(t, string(jsonData), `"redirectUrl"`)
}

// TestPaymentOpen tests open payment response
func TestPaymentOpen(t *testing.T) {
	resp := response.PaymentOpen()

	assert.Equal(t, "open", resp.Status)
	assert.Empty(t, resp.Message)
	assert.Empty(t, resp.RedirectURL)

	// Verify JSON marshaling
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"status":"open"`)
}

// TestPaymentFailed tests failed payment response
func TestPaymentFailed(t *testing.T) {
	resp := response.PaymentFailed("Insufficient funds")

	assert.Equal(t, "failed", resp.Status)
	assert.Equal(t, "Insufficient funds", resp.Message)
	assert.Empty(t, resp.RedirectURL)

	// Verify JSON marshaling
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"status":"failed"`)
	assert.Contains(t, string(jsonData), `"message":"Insufficient funds"`)
}

// TestPaymentFailed_EmptyMessage tests failed payment with empty message
func TestPaymentFailed_EmptyMessage(t *testing.T) {
	resp := response.PaymentFailed("")

	assert.Equal(t, "failed", resp.Status)
	assert.Empty(t, resp.Message)
}

// TestPaymentCancelled tests cancelled payment response
func TestPaymentCancelled(t *testing.T) {
	resp := response.PaymentCancelled()

	assert.Equal(t, "cancelled", resp.Status)
	assert.Empty(t, resp.Message)
	assert.Empty(t, resp.RedirectURL)

	// Verify JSON marshaling
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"status":"cancelled"`)
}

// TestPaymentAuthorized tests authorized payment response
func TestPaymentAuthorized(t *testing.T) {
	resp := response.PaymentAuthorized()

	assert.Equal(t, "authorized", resp.Status)
	assert.Empty(t, resp.Message)
	assert.Empty(t, resp.RedirectURL)

	// Verify JSON marshaling
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"status":"authorized"`)
}

// TestPaymentRedirect tests payment redirect response
func TestPaymentRedirect(t *testing.T) {
	resp := response.PaymentRedirect("https://payment-gateway.com/checkout?id=12345")

	assert.Equal(t, "open", resp.Status)
	assert.Equal(t, "https://payment-gateway.com/checkout?id=12345", resp.RedirectURL)
	assert.Empty(t, resp.Message)

	// Verify JSON marshaling
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"status":"open"`)
	assert.Contains(t, string(jsonData), `"redirectUrl":"https://payment-gateway.com/checkout?id=12345"`)
}

// TestPaymentRedirect_EmptyURL tests payment redirect with empty URL
func TestPaymentRedirect_EmptyURL(t *testing.T) {
	resp := response.PaymentRedirect("")

	assert.Equal(t, "open", resp.Status)
	assert.Empty(t, resp.RedirectURL)
}

// TestRefundSuccess tests successful refund response
func TestRefundSuccess(t *testing.T) {
	resp := response.RefundSuccess()

	assert.Equal(t, "success", resp.Status)
	assert.Empty(t, resp.Message)

	// Verify JSON marshaling
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"status":"success"`)
	assert.NotContains(t, string(jsonData), `"message"`)
}

// TestRefundFailed tests failed refund response
func TestRefundFailed(t *testing.T) {
	resp := response.RefundFailed("Original transaction cannot be refunded")

	assert.Equal(t, "failed", resp.Status)
	assert.Equal(t, "Original transaction cannot be refunded", resp.Message)

	// Verify JSON marshaling
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"status":"failed"`)
	assert.Contains(t, string(jsonData), `"message":"Original transaction cannot be refunded"`)
}

// TestRefundFailed_EmptyMessage tests failed refund with empty message
func TestRefundFailed_EmptyMessage(t *testing.T) {
	resp := response.RefundFailed("")

	assert.Equal(t, "failed", resp.Status)
	assert.Empty(t, resp.Message)
}

// TestRefundProcessing tests refund processing response
func TestRefundProcessing(t *testing.T) {
	resp := response.RefundProcessing()

	assert.Equal(t, "processing", resp.Status)
	assert.Empty(t, resp.Message)

	// Verify JSON marshaling
	jsonData, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"status":"processing"`)
}

// TestActionButtonResponse_JSONStructure tests the complete JSON structure
func TestActionButtonResponse_JSONStructure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		response response.ActionButtonResponse
		expected map[string]interface{}
	}{
		{
			name:     "notification with all fields",
			response: response.NotificationError("Test error"),
			expected: map[string]interface{}{
				"actionType": "notification",
				"status":     "error",
				"message":    "Test error",
			},
		},
		{
			name:     "reload without optional fields",
			response: response.ReloadResponse(),
			expected: map[string]interface{}{
				"actionType": "reload",
			},
		},
		{
			name:     "openNewTab with payload",
			response: response.OpenNewTabResponse("https://example.com"),
			expected: map[string]interface{}{
				"actionType": "openNewTab",
				"payload": map[string]interface{}{
					"redirectUrl": "https://example.com",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			jsonData, err := json.Marshal(tt.response)
			require.NoError(t, err)

			var result map[string]interface{}
			err = json.Unmarshal(jsonData, &result)
			require.NoError(t, err)

			assert.Equal(t, tt.expected["actionType"], result["actionType"])

			if tt.expected["status"] != nil {
				assert.Equal(t, tt.expected["status"], result["status"])
			}

			if tt.expected["message"] != nil {
				assert.Equal(t, tt.expected["message"], result["message"])
			}

			if tt.expected["payload"] != nil {
				assert.NotNil(t, result["payload"])
			}
		})
	}
}

// TestPaymentResponse_JSONStructure tests payment response JSON structure
func TestPaymentResponse_JSONStructure(t *testing.T) {
	tests := []struct {
		name     string
		response response.PaymentResponse
		expected map[string]interface{}
	}{
		{
			name:     "paid without optional fields",
			response: response.PaymentPaid(),
			expected: map[string]interface{}{
				"status": "paid",
			},
		},
		{
			name:     "failed with message",
			response: response.PaymentFailed("Card declined"),
			expected: map[string]interface{}{
				"status":  "failed",
				"message": "Card declined",
			},
		},
		{
			name:     "redirect with URL",
			response: response.PaymentRedirect("https://payment.example.com"),
			expected: map[string]interface{}{
				"status":      "open",
				"redirectUrl": "https://payment.example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonData, err := json.Marshal(tt.response)
			require.NoError(t, err)

			var result map[string]interface{}
			err = json.Unmarshal(jsonData, &result)
			require.NoError(t, err)

			assert.Equal(t, tt.expected["status"], result["status"])

			if tt.expected["message"] != nil {
				assert.Equal(t, tt.expected["message"], result["message"])
			} else {
				assert.Nil(t, result["message"])
			}

			if tt.expected["redirectUrl"] != nil {
				assert.Equal(t, tt.expected["redirectUrl"], result["redirectUrl"])
			} else {
				assert.Nil(t, result["redirectUrl"])
			}
		})
	}
}

// TestRefundResponse_JSONStructure tests refund response JSON structure
func TestRefundResponse_JSONStructure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response response.RefundResponse
		expected map[string]interface{}
	}{
		{
			name:     "success without message",
			response: response.RefundSuccess(),
			expected: map[string]interface{}{
				"status": "success",
			},
		},
		{
			name:     "failed with message",
			response: response.RefundFailed("Refund period expired"),
			expected: map[string]interface{}{
				"status":  "failed",
				"message": "Refund period expired",
			},
		},
		{
			name:     "processing",
			response: response.RefundProcessing(),
			expected: map[string]interface{}{
				"status": "processing",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			jsonData, err := json.Marshal(tt.response)
			require.NoError(t, err)

			var result map[string]interface{}
			err = json.Unmarshal(jsonData, &result)
			require.NoError(t, err)

			assert.Equal(t, tt.expected["status"], result["status"])

			if tt.expected["message"] != nil {
				assert.Equal(t, tt.expected["message"], result["message"])
			} else {
				assert.Nil(t, result["message"])
			}
		})
	}
}
