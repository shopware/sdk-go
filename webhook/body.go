package webhook

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/shopware/sdk-go/sdkerrors"
)

// ReadLimitedBody reads an incoming request body into memory, enforcing a
// maximum size before any downstream processing (including authentication).
//
// This guards public, unauthenticated endpoints against memory-exhaustion DoS
// caused by oversized payloads. If maxBytes is <= 0, DefaultMaxBodyBytes is used.
//
// When the body exceeds the limit, a *sdkerrors.Error with HTTP status 413
// (Payload Too Large) is returned. On success, the request body is restored so
// it can be read again by downstream handlers.
func ReadLimitedBody(r *http.Request, maxBytes int64) ([]byte, error) {
	if r.Body == nil {
		return nil, errors.New("request body is nil")
	}

	if maxBytes <= 0 {
		maxBytes = DefaultMaxBodyBytes
	}

	// Read at most maxBytes+1 so we can detect (but never buffer beyond) an
	// over-limit body without trusting a possibly-absent/spoofed Content-Length.
	limit := maxBytes + 1
	if limit < 0 { // overflow when maxBytes == math.MaxInt64
		limit = maxBytes
	}
	rawBody, readErr := io.ReadAll(io.LimitReader(r.Body, limit))
	if readErr != nil {
		return nil, readErr
	}

	if int64(len(rawBody)) > maxBytes {
		return nil, sdkerrors.NewPayloadTooLarge(
			fmt.Sprintf("request body exceeds limit of %d bytes", maxBytes),
		)
	}

	// Restore body for potential downstream use
	r.Body = io.NopCloser(bytes.NewReader(rawBody))
	return rawBody, nil
}
