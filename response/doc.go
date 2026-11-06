// Package response provides type-safe builders for Shopware response types.
//
// This package simplifies creating properly formatted responses for various
// Shopware app scenarios including notifications, actions, and payment flows.
//
// # Notifications
//
//	response.NotificationSuccess("Operation completed")
//	response.NotificationError("Something went wrong")
//	response.NotificationInfo("Processing...")
//	response.NotificationWarning("Check your settings")
//
// # Actions
//
//	response.ReloadResponse()
//	response.OpenNewTabResponse("https://example.com")
//
// # Payment Responses
//
//	response.PaymentPaid()
//	response.PaymentFailed("Payment declined")
//	response.PaymentCancelled()
//	response.PaymentRedirect("https://payment-gateway.com/checkout")
//
// All builders return properly structured JSON responses that conform to
// Shopware's app response specifications.
package response
