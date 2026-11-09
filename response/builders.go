package response

import "encoding/json"

const (
	// ActionTypeNotification represents a notification action type
	ActionTypeNotification = "notification"
	// ActionTypeReload represents a reload action type
	ActionTypeReload = "reload"
	// ActionTypeOpenNewTab represents an open new tab action type
	ActionTypeOpenNewTab = "openNewTab"
	// ActionTypeOpenModal represents an open modal action type
	ActionTypeOpenModal = "openModal"

	// NotificationStatusSuccess represents a successful notification status
	NotificationStatusSuccess = "success"
	// NotificationStatusError represents an error notification status
	NotificationStatusError = "error"
	// NotificationStatusWarning represents a warning notification status
	NotificationStatusWarning = "warning"
	// NotificationStatusInfo represents an info notification status
	NotificationStatusInfo = "info"

	// Payment statuses are the names of Shopware state machine transitions
	// for the order transaction, not the names of the resulting states.

	// PaymentStatusPaid moves the payment to "paid".
	PaymentStatusPaid = "paid"
	// PaymentStatusOpen is not a Shopware transition.
	//
	// Deprecated: "open" is not a Shopware transition and is rejected by
	// Shopware. Leave PaymentResponse.Status empty to keep the payment open.
	PaymentStatusOpen = "open"
	// PaymentStatusFailed moves the payment to "failed".
	PaymentStatusFailed = "fail"
	// PaymentStatusCancelled moves the payment to "cancelled"; Shopware treats
	// it as a cancellation by the customer.
	PaymentStatusCancelled = "cancel"
	// PaymentStatusAuthorized moves the payment to "authorized".
	PaymentStatusAuthorized = "authorize"

	// Refund statuses are the names of Shopware state machine transitions
	// for the refund, not the names of the resulting states.

	// RefundStatusSuccess moves the refund to "completed".
	RefundStatusSuccess = "complete"
	// RefundStatusFailed moves the refund to "failed".
	RefundStatusFailed = "fail"
	// RefundStatusProcessing moves the refund to "in_progress".
	RefundStatusProcessing = "process"
)

// ActionButtonResponse represents a response to an action button request.
//
// Shopware reads everything except the action type from the "payload" object,
// so the response is always encoded as {"actionType": ..., "payload": {...}}.
// See MarshalJSON for how the fields are placed.
type ActionButtonResponse struct {
	ActionType string `json:"actionType"`
	// Status is the notification status. It is encoded inside "payload".
	Status string `json:"status,omitempty"`
	// Message is the notification text. It is encoded inside "payload".
	Message string `json:"message,omitempty"`
	// Payload is the action payload. When set, it is encoded as is and
	// Status and Message are ignored.
	Payload any `json:"payload,omitempty"`
}

// MarshalJSON encodes the response in the format Shopware expects.
//
// The "payload" key is always present: Payload when set; otherwise an object
// with Status and Message when either is set; otherwise an empty object.
func (r ActionButtonResponse) MarshalJSON() ([]byte, error) {
	payload := r.Payload
	if payload == nil {
		notification := struct {
			Status  string `json:"status,omitempty"`
			Message string `json:"message,omitempty"`
		}{Status: r.Status, Message: r.Message}
		payload = notification
	}

	return json.Marshal(struct {
		ActionType string `json:"actionType"`
		Payload    any    `json:"payload"`
	}{ActionType: r.ActionType, Payload: payload})
}

// NotificationResponse creates a notification response
func NotificationResponse(status, message string) ActionButtonResponse {
	return ActionButtonResponse{
		ActionType: ActionTypeNotification,
		Status:     status,
		Message:    message,
	}
}

// NotificationSuccess creates a success notification
func NotificationSuccess(message string) ActionButtonResponse {
	return NotificationResponse(NotificationStatusSuccess, message)
}

// NotificationError creates an error notification
func NotificationError(message string) ActionButtonResponse {
	return NotificationResponse(NotificationStatusError, message)
}

// NotificationWarning creates a warning notification
func NotificationWarning(message string) ActionButtonResponse {
	return NotificationResponse(NotificationStatusWarning, message)
}

// NotificationInfo creates an info notification
func NotificationInfo(message string) ActionButtonResponse {
	return NotificationResponse(NotificationStatusInfo, message)
}

// ReloadResponse creates a reload response that refreshes the page
func ReloadResponse() ActionButtonResponse {
	return ActionButtonResponse{
		ActionType: ActionTypeReload,
	}
}

// OpenNewTabResponse creates a response that opens a new tab
func OpenNewTabResponse(url string) ActionButtonResponse {
	return ActionButtonResponse{
		ActionType: ActionTypeOpenNewTab,
		Payload: map[string]string{
			"redirectUrl": url,
		},
	}
}

// OpenModalResponse creates a response that opens a modal
func OpenModalResponse(url string) ActionButtonResponse {
	return ActionButtonResponse{
		ActionType: ActionTypeOpenModal,
		Payload: map[string]any{
			"iframeUrl": url,
			"size":      "medium",
			"expand":    false,
		},
	}
}

// PaymentResponse represents a response to a payment request.
//
// Status is the name of a Shopware transition (see the PaymentStatus
// constants). An empty Status requests no transition, so the payment stays
// in its current state.
type PaymentResponse struct {
	Status      string `json:"status,omitempty"`
	Message     string `json:"message,omitempty"`
	RedirectURL string `json:"redirectUrl,omitempty"`
}

// PaymentPaid creates a response indicating payment was successful
func PaymentPaid() PaymentResponse {
	return PaymentResponse{
		Status: PaymentStatusPaid,
	}
}

// PaymentOpen creates a response that keeps the payment open. It sets no
// status, because Shopware has no "open" transition.
func PaymentOpen() PaymentResponse {
	return PaymentResponse{}
}

// PaymentFailed creates a response indicating payment failed
func PaymentFailed(message string) PaymentResponse {
	return PaymentResponse{
		Status:  PaymentStatusFailed,
		Message: message,
	}
}

// PaymentCancelled creates a response indicating payment was cancelled
func PaymentCancelled() PaymentResponse {
	return PaymentResponse{
		Status: PaymentStatusCancelled,
	}
}

// PaymentAuthorized creates a response indicating payment was authorized (but not yet captured)
func PaymentAuthorized() PaymentResponse {
	return PaymentResponse{
		Status: PaymentStatusAuthorized,
	}
}

// PaymentRedirect creates a response that redirects the customer to an
// external payment page. It sets no status, so the payment stays open until
// the finalize request.
func PaymentRedirect(url string) PaymentResponse {
	return PaymentResponse{
		RedirectURL: url,
	}
}

// RefundResponse represents a response to a refund request.
//
// Status is the name of a Shopware transition (see the RefundStatus
// constants).
type RefundResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// RefundSuccess creates a response indicating refund was successful
func RefundSuccess() RefundResponse {
	return RefundResponse{
		Status: RefundStatusSuccess,
	}
}

// RefundFailed creates a response indicating refund failed
func RefundFailed(message string) RefundResponse {
	return RefundResponse{
		Status:  RefundStatusFailed,
		Message: message,
	}
}

// RefundProcessing creates a response indicating refund is being processed
func RefundProcessing() RefundResponse {
	return RefundResponse{
		Status: RefundStatusProcessing,
	}
}
