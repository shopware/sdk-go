package registration

import (
	sdkerrors "github.com/shopware/app-sdk-go/pkg/errors"
)

// Request represents the initial registration request from Shopware
type Request struct {
	ShopID    string `json:"shop-id"`
	ShopURL   string `json:"shop-url"`
	Timestamp int64  `json:"timestamp"`

	// RawQuery is the full query string from the request (e.g. r.URL.RawQuery).
	// The handler normalizes encoding internally before signature verification.
	RawQuery      string `json:"-"`
	AppSignature  string `json:"-"`
	ShopSignature string `json:"-"`
}

// Response is the app's response to a registration request
type Response struct {
	Proof           string `json:"proof"`
	Secret          string `json:"secret"`
	ConfirmationURL string `json:"confirmation_url"`
}

// ConfirmationRequest represents the confirmation request from Shopware after initial registration
type ConfirmationRequest struct {
	ShopID            string `json:"shopId"`
	ShopURL           string `json:"shopUrl"`
	APIKey            string `json:"apiKey"`
	SecretKey         string `json:"secretKey"`
	Timestamp         string `json:"timestamp"`
	Signature         string `json:"-"`
	PreviousSignature string `json:"-"`
	RawBody           []byte `json:"-"`
}

// ConfirmationResponse is the app's response to a confirmation request
type ConfirmationResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

func (r *Request) Validate() error {
	if r.ShopID == "" {
		return sdkerrors.NewInvalidRequest("shop-id is required")
	}
	if r.ShopURL == "" {
		return sdkerrors.NewInvalidRequest("shop-url is required")
	}
	if r.Timestamp == 0 {
		return sdkerrors.NewInvalidRequest("timestamp is required")
	}
	return nil
}

func (c *ConfirmationRequest) Validate() error {
	if c.ShopID == "" {
		return sdkerrors.NewInvalidRequest("shopId is required")
	}
	if c.ShopURL == "" {
		return sdkerrors.NewInvalidRequest("shopUrl is required")
	}
	if c.APIKey == "" {
		return sdkerrors.NewInvalidRequest("apiKey is required")
	}
	if c.SecretKey == "" {
		return sdkerrors.NewInvalidRequest("secretKey is required")
	}
	return nil
}
