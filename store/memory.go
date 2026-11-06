package store

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// InMemoryStore is a simple in-memory implementation of ShopStore for development and testing
type InMemoryStore struct {
	shops map[string]*Shop
	mu    sync.RWMutex
}

var _ ShopStore = (*InMemoryStore)(nil)

// NewInMemoryStore creates a new in-memory shop store
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		shops: make(map[string]*Shop),
	}
}

// GetShopByID retrieves a shop by ID
func (s *InMemoryStore) GetShopByID(ctx context.Context, shopID string) (*Shop, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Lock only for map access
	s.mu.RLock()
	shop, exists := s.shops[shopID]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrShopNotFound, shopID)
	}

	// Return a copy to prevent external modifications
	shopCopy := *shop
	return &shopCopy, nil
}

// UpdateShop stores or updates a shop
func (s *InMemoryStore) UpdateShop(ctx context.Context, shop *Shop) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Create a copy to store
	shopCopy := *shop
	s.shops[shop.ShopID] = &shopCopy

	return nil
}

// SetShopActive updates a shop's active state
func (s *InMemoryStore) SetShopActive(ctx context.Context, shopID string, active bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	shop, exists := s.shops[shopID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrShopNotFound, shopID)
	}

	shop.Active = active
	shop.UpdatedAt = time.Now()

	return nil
}

// DeleteShop deletes a shop by ID
func (s *InMemoryStore) DeleteShop(ctx context.Context, shopID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.shops[shopID]; !exists {
		return fmt.Errorf("%w: %s", ErrShopNotFound, shopID)
	}

	delete(s.shops, shopID)
	return nil
}

// HealthCheck always returns nil for in-memory store
func (s *InMemoryStore) HealthCheck(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
