// Package errors provides common error types and sentinel errors for the SDK.
//
// This package defines a unified error handling strategy across all SDK packages,
// providing structured errors with HTTP status codes and error types that can be
// programmatically inspected.
package errors

import (
	"errors"
	"fmt"
	"net/http"
)

// Sentinel errors for programmatic error checking
var (
	// ErrShopNotFound is returned when a shop doesn't exist in the store
	ErrShopNotFound = errors.New("shop not found")

	// ErrInvalidSignature is returned when HMAC signature verification fails
	ErrInvalidSignature = errors.New("invalid signature")

	// ErrTimestampExpired is returned when a request timestamp is outside the replay window
	ErrTimestampExpired = errors.New("timestamp expired")

	// ErrMissingShopID is returned when shop-id parameter is missing
	ErrMissingShopID = errors.New("missing shop-id parameter")

	// ErrMissingSignature is returned when signature parameter/header is missing
	ErrMissingSignature = errors.New("missing signature")

	// ErrInvalidRequest is returned when request data is malformed
	ErrInvalidRequest = errors.New("invalid request")

	// ErrShopNotActive is returned when a shop exists but is not in active status
	ErrShopNotActive = errors.New("shop not active")

	// ErrInvalidPayload is returned when webhook/request payload validation fails
	ErrInvalidPayload = errors.New("invalid payload")

	// ErrVerificationFailed is returned when webhook verification fails
	ErrVerificationFailed = errors.New("verification failed")

	// ErrRegistrationState is returned when registration cannot proceed because
	// the stored shop state is not compatible with the requested transition.
	ErrRegistrationState = errors.New("registration state invalid")

	// ErrPayloadTooLarge is returned when a request body exceeds the configured size limit
	ErrPayloadTooLarge = errors.New("payload too large")

	// ErrEventMismatch is returned when a lifecycle body carries an event other
	// than the one its route accepts.
	ErrEventMismatch = errors.New("signed event does not match the route")
)

// Error type constants for consistent error categorization
const (
	TypeInvalidSignature   = "invalid_signature"
	TypeShopNotFound       = "shop_not_found"
	TypeTimestampExpired   = "timestamp_expired"
	TypeInvalidRequest     = "invalid_request"
	TypeShopExists         = "shop_exists"
	TypeInvalidPayload     = "invalid_payload"
	TypeVerificationFailed = "verification_failed"
	TypeShopNotActive      = "shop_not_active"
	TypeRegistrationState  = "registration_state"
	TypeStorageError       = "storage_error"
	TypePayloadTooLarge    = "payload_too_large"
	TypeEventMismatch      = "event_mismatch"
)

// Error represents a structured SDK error with type, message, HTTP code, and wrapped error
type Error struct {
	Type string `json:"type"`

	Message string `json:"message"`

	Code int `json:"code"`

	Err error `json:"-"`
}

// New creates a new Error with the given type, message, and HTTP code
func New(errType, message string, code int) *Error {
	return &Error{
		Type:    errType,
		Message: message,
		Code:    code,
	}
}

// Wrap wraps an existing error with additional context and SDK error metadata
func Wrap(err error, errType, message string, code int) *Error {
	return &Error{
		Type:    errType,
		Message: message,
		Code:    code,
		Err:     err,
	}
}

// NewInvalidSignature creates an error for signature verification failures
func NewInvalidSignature(message string) *Error {
	return &Error{
		Type:    TypeInvalidSignature,
		Message: message,
		Code:    http.StatusUnauthorized,
		Err:     ErrInvalidSignature,
	}
}

// NewShopNotFound creates an error for shop not found cases
func NewShopNotFound(shopID string) *Error {
	return &Error{
		Type:    TypeShopNotFound,
		Message: fmt.Sprintf("shop not found: %s", shopID),
		Code:    http.StatusNotFound,
		Err:     ErrShopNotFound,
	}
}

// NewTimestampExpired creates an error for timestamp replay window violations
func NewTimestampExpired(message string) *Error {
	return &Error{
		Type:    TypeTimestampExpired,
		Message: message,
		Code:    http.StatusUnauthorized,
		Err:     ErrTimestampExpired,
	}
}

// NewEventMismatch creates an error for a lifecycle request whose signed event
// does not match the one its route accepts. The status is 403, not 401: the
// signature is valid, it just does not cover this transition.
func NewEventMismatch(expected, actual string) *Error {
	if actual == "" {
		actual = "<none>"
	}

	return &Error{
		Type:    TypeEventMismatch,
		Message: fmt.Sprintf("route expects signed event %s, body carries %s", expected, actual),
		Code:    http.StatusForbidden,
		Err:     ErrEventMismatch,
	}
}

// NewInvalidRequest creates an error for malformed request data
func NewInvalidRequest(message string) *Error {
	return &Error{
		Type:    TypeInvalidRequest,
		Message: message,
		Code:    http.StatusBadRequest,
		Err:     ErrInvalidRequest,
	}
}

// NewMissingShopID creates an error for requests missing the shop-id parameter
func NewMissingShopID() *Error {
	return &Error{
		Type:    TypeInvalidRequest,
		Message: "missing shop-id parameter",
		Code:    http.StatusBadRequest,
		Err:     ErrMissingShopID,
	}
}

// NewMissingSignature creates an error for requests missing the signature parameter/header
func NewMissingSignature() *Error {
	return &Error{
		Type:    TypeInvalidSignature,
		Message: "missing signature parameter",
		Code:    http.StatusUnauthorized,
		Err:     ErrMissingSignature,
	}
}

// NewInvalidPayload creates an error for webhook/request payload validation failures
func NewInvalidPayload(message string) *Error {
	return &Error{
		Type:    TypeInvalidPayload,
		Message: message,
		Code:    http.StatusBadRequest,
		Err:     ErrInvalidPayload,
	}
}

// NewVerificationFailed creates an error for webhook verification failures
func NewVerificationFailed(message string) *Error {
	return &Error{
		Type:    TypeVerificationFailed,
		Message: message,
		Code:    http.StatusUnauthorized,
		Err:     ErrVerificationFailed,
	}
}

// NewShopNotActive creates an error when a shop exists but is not in active status
func NewShopNotActive(shopID string) *Error {
	return &Error{
		Type:    TypeShopNotActive,
		Message: fmt.Sprintf("shop is not active: %s", shopID),
		Code:    http.StatusForbidden,
		Err:     ErrShopNotActive,
	}
}

// NewRegistrationStateError creates an error for invalid registration state transitions
func NewRegistrationStateError(message string, err error) *Error {
	if err == nil {
		err = ErrRegistrationState
	} else {
		err = fmt.Errorf("%w: %w", ErrRegistrationState, err)
	}

	return &Error{
		Type:    TypeRegistrationState,
		Message: message,
		Code:    http.StatusConflict,
		Err:     err,
	}
}

// NewPayloadTooLarge creates an error when a request body exceeds the configured size limit
func NewPayloadTooLarge(message string) *Error {
	return &Error{
		Type:    TypePayloadTooLarge,
		Message: message,
		Code:    http.StatusRequestEntityTooLarge,
		Err:     ErrPayloadTooLarge,
	}
}

// NewStorageError creates an error for storage/database operation failures
func NewStorageError(message string, err error) *Error {
	return &Error{
		Type:    TypeStorageError,
		Message: message,
		Code:    http.StatusInternalServerError,
		Err:     err,
	}
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Err
}

func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		if e.Err != nil {
			return errors.Is(e.Err, target)
		}
		return false
	}
	return e.Type == t.Type
}
