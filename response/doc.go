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
// Action button responses are always encoded with a "payload" object, which is
// where Shopware reads them from:
//
//	{"actionType":"notification","payload":{"status":"success","message":"Operation completed"}}
//
// # Actions
//
//	response.ReloadResponse()
//	response.OpenNewTabResponse("https://example.com")
//	response.OpenModalResponse("https://example.com/modal")
//
// # Payment and Refund Responses
//
//	response.PaymentPaid()
//	response.PaymentAuthorized()
//	response.PaymentFailed("Payment declined")
//	response.PaymentCancelled()
//	response.PaymentRedirect("https://payment-gateway.com/checkout")
//
//	response.RefundSuccess()
//	response.RefundProcessing()
//	response.RefundFailed("Refund period expired")
//
// The status of a payment or refund response is the name of a Shopware state
// machine transition (for example "authorize" or "fail"), not the name of the
// resulting state. A response without a status keeps the payment or refund in
// its current state; PaymentOpen and PaymentRedirect send no status.
package response
