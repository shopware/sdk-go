package store

import (
	"context"
	"errors"
	"time"
)

var ErrShopNotFound = errors.New("shop not found")

// Shop represents a registered Shopware shop installation
type Shop struct {
	ShopID                         string     `json:"id"`
	ShopURL                        string     `json:"url"`
	ShopSecret                     string     `json:"secret"`
	ClientID                       string     `json:"clientId"`
	ClientSecret                   string     `json:"clientSecret"`
	Active                         bool       `json:"active"`
	RegistrationConfirmed          bool       `json:"registrationConfirmed"`
	PendingShopSecret              string     `json:"pendingSecret,omitempty"`
	PendingShopURL                 string     `json:"pendingUrl,omitempty"`
	PreviousShopSecret             string     `json:"previousSecret,omitempty"`
	SecretsRotatedAt               *time.Time `json:"secretsRotatedAt,omitempty"`
	HasVerifiedWithDoubleSignature bool       `json:"hasVerifiedWithDoubleSignature"`
	CreatedAt                      time.Time  `json:"created_at"`
	UpdatedAt                      time.Time  `json:"updated_at"`
}

// ShopStore defines the interface for shop data persistence
type ShopStore interface {
	GetShopByID(ctx context.Context, shopID string) (*Shop, error)
	UpdateShop(ctx context.Context, shop *Shop) error
	SetShopActive(ctx context.Context, shopID string, active bool) error
	DeleteShop(ctx context.Context, shopID string) error
	HealthCheck(ctx context.Context) error
}
