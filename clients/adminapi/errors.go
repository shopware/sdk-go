package adminapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// maxErrorBodyLen caps how much of a response body is quoted in an error
// message.
const maxErrorBodyLen = 200

var (
	// ErrUnexpectedRedirect is returned when the shop answers with a
	// redirect. The Admin API never redirects on purpose; a redirect usually
	// means a wrong BaseURL (for example http instead of https). The client
	// does not follow it, so the access token is never sent elsewhere.
	ErrUnexpectedRedirect = errors.New("adminapi: unexpected redirect")

	// ErrResponseTooLarge is returned when a response body exceeds
	// Config.MaxResponseBytes.
	ErrResponseTooLarge = errors.New("adminapi: response body too large")

	// ErrInvalidTokenResponse is wrapped by a TokenError when the token
	// endpoint answered with a body that is not a valid token response.
	ErrInvalidTokenResponse = errors.New("invalid token response")
)

// TokenError is returned when the client could not get a token from the shop:
// the token request could not be built or sent, the shop rejected it (the
// wrapped error is then an *APIError), or the response was not a valid token
// (it then wraps ErrInvalidTokenResponse). Errors of the token storage or the
// Locker are not TokenErrors.
//
// Use errors.As to tell a failed login from a failed API request.
type TokenError struct {
	Err error
}

func (e *TokenError) Error() string { return e.Err.Error() }

// Unwrap returns the underlying error.
func (e *TokenError) Unwrap() error { return e.Err }

// APIError is a response with a status code of 400 or higher.
type APIError struct {
	// StatusCode is the HTTP status code of the response.
	StatusCode int

	// Errors holds the errors Shopware reported. It is empty when the body
	// was not in a known error format.
	Errors []ErrorDetail

	// Body is the raw response body.
	Body []byte
}

// ErrorDetail is one entry of the Shopware errors list.
type ErrorDetail struct {
	Status string          `json:"status"`
	Code   string          `json:"code"`
	Title  string          `json:"title"`
	Detail string          `json:"detail"`
	Source ErrorSource     `json:"source"`
	Meta   json.RawMessage `json:"meta,omitempty"`
}

// ErrorSource points to the part of the request an error is about.
type ErrorSource struct {
	Pointer string `json:"pointer"`
}

func (e *APIError) Error() string {
	if len(e.Errors) == 0 {
		return fmt.Sprintf("adminapi: status %d: %s", e.StatusCode, truncate(e.Body, maxErrorBodyLen))
	}

	first := e.Errors[0]
	msg := first.Detail
	if msg == "" {
		msg = first.Title
	}
	if msg == "" {
		msg = first.Code
	}
	if more := len(e.Errors) - 1; more > 0 {
		return fmt.Sprintf("adminapi: status %d: %s (and %d more)", e.StatusCode, msg, more)
	}
	return fmt.Sprintf("adminapi: status %d: %s", e.StatusCode, msg)
}

// IsNotFound reports whether err is an APIError with status 404.
func IsNotFound(err error) bool { return hasStatus(err, http.StatusNotFound) }

// IsUnauthorized reports whether err is an APIError with status 401.
func IsUnauthorized(err error) bool { return hasStatus(err, http.StatusUnauthorized) }

// IsConflict reports whether err is an APIError with status 409.
func IsConflict(err error) bool { return hasStatus(err, http.StatusConflict) }

func hasStatus(err error, status int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == status
}

// newAPIError builds an APIError from a response. It understands the
// Shopware errors envelope and the OAuth error format of the token endpoint.
func newAPIError(statusCode int, body []byte) *APIError {
	apiErr := &APIError{StatusCode: statusCode, Body: body}

	var envelope struct {
		Errors      []ErrorDetail `json:"errors"`
		Error       string        `json:"error"`
		Description string        `json:"error_description"`
		Message     string        `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return apiErr
	}

	switch {
	case len(envelope.Errors) > 0:
		apiErr.Errors = envelope.Errors
	case envelope.Error != "":
		detail := envelope.Description
		if detail == "" {
			detail = envelope.Message
		}
		apiErr.Errors = []ErrorDetail{{Code: envelope.Error, Detail: detail}}
	}
	return apiErr
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
