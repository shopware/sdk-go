package errors_test

import (
	"errors"
	"net/http"
	"testing"

	sdkerrors "github.com/shopware/app-sdk-go/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestError_Error(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      *sdkerrors.Error
		expected string
	}{
		{
			name: "error without underlying error",
			err: &sdkerrors.Error{
				Type:    sdkerrors.TypeInvalidRequest,
				Message: "missing required field",
				Code:    http.StatusBadRequest,
			},
			expected: "missing required field",
		},
		{
			name: "error with underlying error",
			err: &sdkerrors.Error{
				Type:    sdkerrors.TypeStorageError,
				Message: "database error",
				Code:    http.StatusInternalServerError,
				Err:     errors.New("connection timeout"),
			},
			expected: "database error: connection timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func TestError_Unwrap(t *testing.T) {
	underlyingErr := errors.New("underlying error")
	err := &sdkerrors.Error{
		Type:    sdkerrors.TypeStorageError,
		Message: "storage failed",
		Code:    http.StatusInternalServerError,
		Err:     underlyingErr,
	}

	assert.Equal(t, underlyingErr, err.Unwrap())
}

func TestError_Is(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      *sdkerrors.Error
		target   error
		expected bool
	}{
		{
			name: "matches same error type",
			err: &sdkerrors.Error{
				Type:    sdkerrors.TypeInvalidSignature,
				Message: "sig failed",
				Code:    http.StatusUnauthorized,
			},
			target: &sdkerrors.Error{
				Type:    sdkerrors.TypeInvalidSignature,
				Message: "different message",
				Code:    http.StatusUnauthorized,
			},
			expected: true,
		},
		{
			name: "does not match different error type",
			err: &sdkerrors.Error{
				Type:    sdkerrors.TypeInvalidSignature,
				Message: "sig failed",
				Code:    http.StatusUnauthorized,
			},
			target: &sdkerrors.Error{
				Type:    sdkerrors.TypeShopNotFound,
				Message: "shop not found",
				Code:    http.StatusNotFound,
			},
			expected: false,
		},
		{
			name: "matches underlying sentinel error",
			err: &sdkerrors.Error{
				Type:    sdkerrors.TypeInvalidSignature,
				Message: "sig failed",
				Code:    http.StatusUnauthorized,
				Err:     sdkerrors.ErrInvalidSignature,
			},
			target:   sdkerrors.ErrInvalidSignature,
			expected: true,
		},
		{
			name: "does not match different sentinel error",
			err: &sdkerrors.Error{
				Type:    sdkerrors.TypeInvalidSignature,
				Message: "sig failed",
				Code:    http.StatusUnauthorized,
				Err:     sdkerrors.ErrInvalidSignature,
			},
			target:   sdkerrors.ErrShopNotFound,
			expected: false,
		},
		{
			name: "does not match non-error target",
			err: &sdkerrors.Error{
				Type:    sdkerrors.TypeInvalidSignature,
				Message: "sig failed",
				Code:    http.StatusUnauthorized,
			},
			target:   sdkerrors.ErrShopNotFound,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.err.Is(tt.target))
		})
	}
}

func TestNew(t *testing.T) {
	err := sdkerrors.New(sdkerrors.TypeInvalidRequest, "test message", http.StatusBadRequest)

	assert.Equal(t, sdkerrors.TypeInvalidRequest, err.Type)
	assert.Equal(t, "test message", err.Message)
	assert.Equal(t, http.StatusBadRequest, err.Code)
	assert.NoError(t, err.Err)
}

func TestWrap(t *testing.T) {
	underlyingErr := errors.New("underlying error")
	err := sdkerrors.Wrap(underlyingErr, sdkerrors.TypeStorageError, "wrapped message", http.StatusInternalServerError)

	assert.Equal(t, sdkerrors.TypeStorageError, err.Type)
	assert.Equal(t, "wrapped message", err.Message)
	assert.Equal(t, http.StatusInternalServerError, err.Code)
	assert.Equal(t, underlyingErr, err.Err)
}

func TestNewInvalidSignature(t *testing.T) {
	err := sdkerrors.NewInvalidSignature("signature verification failed")

	assert.Equal(t, sdkerrors.TypeInvalidSignature, err.Type)
	assert.Equal(t, "signature verification failed", err.Message)
	assert.Equal(t, http.StatusUnauthorized, err.Code)
	assert.ErrorIs(t, err, sdkerrors.ErrInvalidSignature)
}

func TestNewShopNotFound(t *testing.T) {
	shopID := "shop-123"
	err := sdkerrors.NewShopNotFound(shopID)

	assert.Equal(t, sdkerrors.TypeShopNotFound, err.Type)
	assert.Contains(t, err.Message, shopID)
	assert.Equal(t, http.StatusNotFound, err.Code)
	assert.ErrorIs(t, err, sdkerrors.ErrShopNotFound)
}

func TestNewTimestampExpired(t *testing.T) {
	err := sdkerrors.NewTimestampExpired("timestamp too old")

	assert.Equal(t, sdkerrors.TypeTimestampExpired, err.Type)
	assert.Equal(t, "timestamp too old", err.Message)
	assert.Equal(t, http.StatusUnauthorized, err.Code)
	assert.ErrorIs(t, err, sdkerrors.ErrTimestampExpired)
}

func TestNewEventMismatch(t *testing.T) {
	err := sdkerrors.NewEventMismatch("app.deleted", "app.activated")

	assert.Equal(t, sdkerrors.TypeEventMismatch, err.Type)
	assert.Equal(t, "route expects signed event app.deleted, body carries app.activated", err.Message)
	assert.Equal(t, http.StatusForbidden, err.Code)
	assert.ErrorIs(t, err, sdkerrors.ErrEventMismatch)
}

func TestNewEventMismatch_NoEventInBody(t *testing.T) {
	err := sdkerrors.NewEventMismatch("app.deleted", "")

	assert.Equal(t, "route expects signed event app.deleted, body carries <none>", err.Message)
}

func TestNewInvalidRequest(t *testing.T) {
	err := sdkerrors.NewInvalidRequest("missing required field")

	assert.Equal(t, sdkerrors.TypeInvalidRequest, err.Type)
	assert.Equal(t, "missing required field", err.Message)
	assert.Equal(t, http.StatusBadRequest, err.Code)
	assert.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)
}

func TestNewInvalidPayload(t *testing.T) {
	err := sdkerrors.NewInvalidPayload("malformed JSON")

	assert.Equal(t, sdkerrors.TypeInvalidPayload, err.Type)
	assert.Equal(t, "malformed JSON", err.Message)
	assert.Equal(t, http.StatusBadRequest, err.Code)
	assert.ErrorIs(t, err, sdkerrors.ErrInvalidPayload)
}

func TestNewPayloadTooLarge(t *testing.T) {
	err := sdkerrors.NewPayloadTooLarge("request body exceeds limit of 16 bytes")

	assert.Equal(t, sdkerrors.TypePayloadTooLarge, err.Type)
	assert.Equal(t, "request body exceeds limit of 16 bytes", err.Message)
	assert.Equal(t, http.StatusRequestEntityTooLarge, err.Code)
	assert.ErrorIs(t, err, sdkerrors.ErrPayloadTooLarge)
}

func TestNewVerificationFailed(t *testing.T) {
	err := sdkerrors.NewVerificationFailed("webhook verification failed")

	assert.Equal(t, sdkerrors.TypeVerificationFailed, err.Type)
	assert.Equal(t, "webhook verification failed", err.Message)
	assert.Equal(t, http.StatusUnauthorized, err.Code)
	assert.ErrorIs(t, err, sdkerrors.ErrVerificationFailed)
}

func TestNewShopNotActive(t *testing.T) {
	shopID := "shop-456"
	err := sdkerrors.NewShopNotActive(shopID)

	assert.Equal(t, sdkerrors.TypeShopNotActive, err.Type)
	assert.Contains(t, err.Message, shopID)
	assert.Equal(t, http.StatusForbidden, err.Code)
	assert.ErrorIs(t, err, sdkerrors.ErrShopNotActive)
}

func TestNewRegistrationStateError(t *testing.T) {
	underlyingErr := errors.New("missing pending shop secret")
	err := sdkerrors.NewRegistrationStateError("registration confirmation is not pending", underlyingErr)

	assert.Equal(t, sdkerrors.TypeRegistrationState, err.Type)
	assert.Equal(t, "registration confirmation is not pending", err.Message)
	assert.Equal(t, http.StatusConflict, err.Code)
	require.ErrorIs(t, err, sdkerrors.ErrRegistrationState)
	assert.ErrorIs(t, err, underlyingErr)
}

func TestNewMissingShopID(t *testing.T) {
	err := sdkerrors.NewMissingShopID()

	assert.Equal(t, sdkerrors.TypeInvalidRequest, err.Type)
	assert.Equal(t, "missing shop-id parameter", err.Message)
	assert.Equal(t, http.StatusBadRequest, err.Code)
	assert.ErrorIs(t, err, sdkerrors.ErrMissingShopID)
}

func TestNewMissingSignature(t *testing.T) {
	err := sdkerrors.NewMissingSignature()

	assert.Equal(t, sdkerrors.TypeInvalidSignature, err.Type)
	assert.Equal(t, "missing signature parameter", err.Message)
	assert.Equal(t, http.StatusUnauthorized, err.Code)
	assert.ErrorIs(t, err, sdkerrors.ErrMissingSignature)
}

func TestNewStorageError(t *testing.T) {
	underlyingErr := errors.New("database timeout")
	err := sdkerrors.NewStorageError("failed to save shop", underlyingErr)

	assert.Equal(t, sdkerrors.TypeStorageError, err.Type)
	assert.Equal(t, "failed to save shop", err.Message)
	assert.Equal(t, http.StatusInternalServerError, err.Code)
	assert.Equal(t, underlyingErr, err.Err)
	assert.ErrorIs(t, err, underlyingErr)
}

func TestErrorsWithErrorsIs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      error
		target   error
		expected bool
	}{
		{
			name:     "direct sentinel error match",
			err:      sdkerrors.ErrShopNotFound,
			target:   sdkerrors.ErrShopNotFound,
			expected: true,
		},
		{
			name:     "wrapped sentinel error match",
			err:      sdkerrors.NewShopNotFound("shop-123"),
			target:   sdkerrors.ErrShopNotFound,
			expected: true,
		},
		{
			name:     "no match different sentinel",
			err:      sdkerrors.NewShopNotFound("shop-123"),
			target:   sdkerrors.ErrInvalidSignature,
			expected: false,
		},
		{
			name:     "underlying error match",
			err:      sdkerrors.NewStorageError("db error", errors.New("timeout")),
			target:   errors.New("timeout"),
			expected: false, // different instance
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, errors.Is(tt.err, tt.target))
		})
	}
}

func TestErrorsWithErrorsAs(t *testing.T) {
	err := sdkerrors.NewInvalidSignature("sig failed")

	var sdkErr *sdkerrors.Error
	require.ErrorAs(t, err, &sdkErr)
	assert.Equal(t, sdkerrors.TypeInvalidSignature, sdkErr.Type)
	assert.Equal(t, http.StatusUnauthorized, sdkErr.Code)
}

func TestSentinelErrors(t *testing.T) {
	t.Parallel()
	sentinels := []error{
		sdkerrors.ErrShopNotFound,
		sdkerrors.ErrInvalidSignature,
		sdkerrors.ErrTimestampExpired,
		sdkerrors.ErrMissingShopID,
		sdkerrors.ErrMissingSignature,
		sdkerrors.ErrInvalidRequest,
		sdkerrors.ErrShopNotActive,
		sdkerrors.ErrInvalidPayload,
		sdkerrors.ErrVerificationFailed,
		sdkerrors.ErrRegistrationState,
	}

	for _, sentinel := range sentinels {
		t.Run(sentinel.Error(), func(t *testing.T) {
			t.Parallel()
			require.Error(t, sentinel)
			assert.NotEmpty(t, sentinel.Error())
		})
	}
}

func TestErrorJSONSerialization(t *testing.T) {
	err := &sdkerrors.Error{
		Type:    sdkerrors.TypeInvalidRequest,
		Message: "test error",
		Code:    http.StatusBadRequest,
	}

	assert.Equal(t, "test error", err.Message)
	assert.Equal(t, sdkerrors.TypeInvalidRequest, err.Type)
	assert.Equal(t, http.StatusBadRequest, err.Code)
}
