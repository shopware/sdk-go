// Package sdkerrors provides unified error handling for the Shopware SDK.
//
// This package defines standardized error types that enable both programmatic
// error checking and meaningful error messages for debugging.
//
// # Structured Errors
//
// All SDK errors implement the Error type:
//
//	type Error struct {
//	    Type    string // Error type for programmatic checking
//	    Message string // Human-readable error message
//	    Code    int    // HTTP status code
//	    Err     error  // Wrapped underlying error
//	}
//
// # Sentinel Errors
//
// Pre-defined sentinel errors for common cases:
//
//	var (
//	    ErrShopNotFound     = errors.New("shop not found")
//	    ErrInvalidSignature = errors.New("invalid signature")
//	    ErrTimestampExpired = errors.New("timestamp expired")
//	    // ... more
//	)
//
// # Error Checking
//
// Use standard error checking with errors.Is and errors.As:
//
//	if errors.Is(err, sdkerrors.ErrShopNotFound) {
//	    // Handle shop not found
//	}
//
//	var sdkErr *sdkerrors.Error
//	if errors.As(err, &sdkErr) {
//	    log.Error("SDK error", "type", sdkErr.Type, "code", sdkErr.Code)
//	}
//
// # Creating Errors
//
// Use constructor functions for common error types:
//
//	// Invalid request with custom message
//	err := sdkerrors.NewInvalidRequest("missing required field 'shop_id'")
//
//	// Shop not found
//	err := sdkerrors.NewShopNotFound("shop-123")
//
//	// Storage error with wrapped underlying error
//	err := sdkerrors.NewStorageError("failed to connect", dbErr)
//
// # Wrapping Errors
//
// Wrap existing errors to add context:
//
//	err := sdkerrors.Wrap(
//	    underlyingErr,
//	    sdkerrors.TypeStorageError,
//	    "failed to save shop",
//	    http.StatusInternalServerError,
//	)
//
// # Error Types
//
// Available error type constants:
//   - TypeInvalidSignature: Signature verification failed
//   - TypeShopNotFound: Shop doesn't exist in store
//   - TypeTimestampExpired: Request timestamp too old
//   - TypeInvalidRequest: Malformed request data
//   - TypeShopExists: Shop already registered
//   - TypeInvalidPayload: Invalid webhook/request payload
//   - TypeVerificationFailed: Generic verification failure
//   - TypeShopNotActive: Shop exists but not active
//   - TypeRegistrationState: Registration state transition cannot proceed
//   - TypeStorageError: Database/storage operation failed
//
// # HTTP Integration
//
// Error.Code provides the appropriate HTTP status code for API responses:
//
//	var sdkErr *sdkerrors.Error
//	if errors.As(err, &sdkErr) {
//	    c.JSON(sdkErr.Code, sdkErr)
//	}
package sdkerrors
