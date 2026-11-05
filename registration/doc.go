// Package registration handles Shopware app registration and confirmation flows.
//
// The registration package provides handlers for the two-step app installation
// process required by Shopware 6:
//
// 1. Registration: Initial handshake where Shopware sends shop details
// 2. Confirmation: Shopware sends API credentials after admin approval
//
// # Registration Flow
//
//	handler := registration.NewHandler(config, store, verifier, logger)
//	resp, err := handler.HandleRegistration(ctx, registrationRequest)
//	// Returns: proof, shop secret, confirmation URL
//
// # Confirmation Flow
//
//	resp, err := handler.HandleConfirmation(ctx, confirmationRequest)
//	// Stores: API key, secret key, activates shop
//
// All requests are verified using HMAC-SHA256 signatures. Failed verifications
// result in automatic shop deletion to prevent credential injection attacks.
package registration
