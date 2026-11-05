package testutil

import (
	"testing"

	sdkerrors "github.com/shopware/app-sdk-go/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AssertSDKError checks if error is of expected SDK error type and returns
// the unwrapped error so callers can assert additional fields (Code, Message, etc.).
func AssertSDKError(t *testing.T, err error, expectedType string) *sdkerrors.Error {
	t.Helper()
	var sdkErr *sdkerrors.Error
	require.ErrorAs(t, err, &sdkErr)
	assert.Equal(t, expectedType, sdkErr.Type)
	return sdkErr
}
