package adminapi

import (
	"encoding/json"
	"net/http"
)

// Response is a successful response of the Admin API.
type Response struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// JSON decodes the response body into target.
func (r *Response) JSON(target any) error {
	return json.Unmarshal(r.Body, target)
}
