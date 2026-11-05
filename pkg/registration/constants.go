package registration

import "net/http"

// HTTP status codes used in registration errors
const (
	StatusBadRequest          = http.StatusBadRequest          // 400
	StatusUnauthorized        = http.StatusUnauthorized        // 401
	StatusNotFound            = http.StatusNotFound            // 404
	StatusConflict            = http.StatusConflict            // 409
	StatusInternalServerError = http.StatusInternalServerError // 500
)
