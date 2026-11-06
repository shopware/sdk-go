package response

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

	// PaymentStatusPaid represents a paid payment status
	PaymentStatusPaid = "paid"
	// PaymentStatusOpen represents an open payment status
	PaymentStatusOpen = "open"
	// PaymentStatusFailed represents a failed payment status
	PaymentStatusFailed = "failed"
	// PaymentStatusCancelled represents a cancelled payment status
	PaymentStatusCancelled = "cancelled"
	// PaymentStatusAuthorized represents an authorized payment status
	PaymentStatusAuthorized = "authorized"

	// RefundStatusSuccess represents a successful refund status
	RefundStatusSuccess = "success"
	// RefundStatusFailed represents a failed refund status
	RefundStatusFailed = "failed"
	// RefundStatusProcessing represents a processing refund status
	RefundStatusProcessing = "processing"
)

// ActionButtonResponse represents a response to an action button request
type ActionButtonResponse struct {
	ActionType string      `json:"actionType"`
	Status     string      `json:"status,omitempty"`
	Message    string      `json:"message,omitempty"`
	Payload    interface{} `json:"payload,omitempty"`
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
		Payload: map[string]interface{}{
			"iframeUrl": url,
			"size":      "medium",
			"expand":    false,
		},
	}
}

// PaymentResponse represents a response to a payment request
type PaymentResponse struct {
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
	RedirectURL string `json:"redirectUrl,omitempty"`
}

// PaymentPaid creates a response indicating payment was successful
func PaymentPaid() PaymentResponse {
	return PaymentResponse{
		Status: PaymentStatusPaid,
	}
}

// PaymentOpen creates a response indicating payment is still open
func PaymentOpen() PaymentResponse {
	return PaymentResponse{
		Status: PaymentStatusOpen,
	}
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

// PaymentRedirect creates a response that redirects the customer to an external payment page
func PaymentRedirect(url string) PaymentResponse {
	return PaymentResponse{
		Status:      PaymentStatusOpen,
		RedirectURL: url,
	}
}

// RefundResponse represents a response to a refund request
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
