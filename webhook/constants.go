package webhook

import "net/http"

// HTTP status codes used in webhook errors
const (
	StatusBadRequest      = http.StatusBadRequest            // 400
	StatusUnauthorized    = http.StatusUnauthorized          // 401
	StatusForbidden       = http.StatusForbidden             // 403
	StatusNotFound        = http.StatusNotFound              // 404
	StatusPayloadTooLarge = http.StatusRequestEntityTooLarge // 413
)

// DefaultMaxBodyBytes is the default maximum size (in bytes) for an incoming
// request body when no explicit limit is configured. It caps how much memory a
// single unauthenticated request can force the server to buffer, mitigating DoS
// via oversized payloads. Default: 5 MiB.
const DefaultMaxBodyBytes int64 = 5 * 1024 * 1024
