package store_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/shopware/app-sdk-go/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// InMemoryStoreTestSuite runs the shared test suite for InMemoryStore
type InMemoryStoreTestSuite struct {
	StoreTestSuite
}

// SetupTest creates a new InMemoryStore for each test
func (suite *InMemoryStoreTestSuite) SetupTest() {
	suite.store = store.NewInMemoryStore()
}

// TestInMemoryStoreTestSuite runs the test suite
func TestInMemoryStoreTestSuite(t *testing.T) {
	suite.Run(t, new(InMemoryStoreTestSuite))
}

// TestInMemoryStore_ThreadSafety tests concurrent access to the in-memory store
func TestInMemoryStore_ThreadSafety(t *testing.T) {
	ctx := context.Background()
	inMemStore := store.NewInMemoryStore()

	var wg sync.WaitGroup
	numGoroutines := 100
	numOperations := 10
	errs := make(chan error, numGoroutines*numOperations*5)

	for i := range numGoroutines {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			for j := range numOperations {
				shopID := fmt.Sprintf("thread-safe-shop-%d-%d", index, j)
				shop := createTestShop(shopID)

				if err := inMemStore.UpdateShop(ctx, shop); err != nil {
					errs <- fmt.Errorf("UpdateShop %s: %w", shopID, err)
					continue
				}

				if _, err := inMemStore.GetShopByID(ctx, shopID); err != nil {
					errs <- fmt.Errorf("GetShopByID %s: %w", shopID, err)
					continue
				}

				if err := inMemStore.SetShopActive(ctx, shopID, false); err != nil {
					errs <- fmt.Errorf("SetShopActive %s: %w", shopID, err)
					continue
				}

				retrieved, err := inMemStore.GetShopByID(ctx, shopID)
				if err != nil {
					errs <- fmt.Errorf("GetShopByID after update %s: %w", shopID, err)
					continue
				}
				if retrieved.Active {
					errs <- fmt.Errorf("expected Active=false for %s", shopID)
				}

				if delErr := inMemStore.DeleteShop(ctx, shopID); delErr != nil {
					errs <- fmt.Errorf("DeleteShop %s: %w", shopID, delErr)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	require.NoError(t, inMemStore.HealthCheck(ctx))
}

// TestInMemoryStore_IsolationBetweenInstances tests that instances don't share state
func TestInMemoryStore_IsolationBetweenInstances(t *testing.T) {
	ctx := context.Background()

	// Create two separate instances
	store1 := store.NewInMemoryStore()
	store2 := store.NewInMemoryStore()

	shop1 := createTestShop("isolation-shop-1")
	shop2 := createTestShop("isolation-shop-2")

	// Store shop1 in store1
	err := store1.UpdateShop(ctx, shop1)
	require.NoError(t, err)

	// Store shop2 in store2
	err = store2.UpdateShop(ctx, shop2)
	require.NoError(t, err)

	// Verify shop1 exists in store1 but not in store2
	_, err = store1.GetShopByID(ctx, shop1.ShopID)
	require.NoError(t, err)

	_, err = store2.GetShopByID(ctx, shop1.ShopID)
	require.Error(t, err)

	// Verify shop2 exists in store2 but not in store1
	_, err = store2.GetShopByID(ctx, shop2.ShopID)
	require.NoError(t, err)

	_, err = store1.GetShopByID(ctx, shop2.ShopID)
	require.Error(t, err)
}

// TestInMemoryStore_GetShopReturnsACopy tests that GetShop returns a copy, not a reference
func TestInMemoryStore_GetShopReturnsACopy(t *testing.T) {
	ctx := context.Background()
	inMemStore := store.NewInMemoryStore()

	shop := createTestShop("copy-test-shop")
	err := inMemStore.UpdateShop(ctx, shop)
	require.NoError(t, err)

	// Get the shop twice
	retrieved1, err := inMemStore.GetShopByID(ctx, shop.ShopID)
	require.NoError(t, err)

	retrieved2, err := inMemStore.GetShopByID(ctx, shop.ShopID)
	require.NoError(t, err)

	// Modify retrieved1
	retrieved1.Active = false
	retrieved1.ClientID = "modified-key"

	// Verify retrieved2 is not affected
	assert.Equal(t, shop.Active, retrieved2.Active)
	assert.Equal(t, shop.ClientID, retrieved2.ClientID)

	// Verify stored shop is not affected
	retrieved3, err := inMemStore.GetShopByID(ctx, shop.ShopID)
	require.NoError(t, err)
	assert.Equal(t, shop.Active, retrieved3.Active)
	assert.Equal(t, shop.ClientID, retrieved3.ClientID)
}

// TestInMemoryStore_StoreShopCreatesACopy tests that StoreShop stores a copy
func TestInMemoryStore_StoreShopCreatesACopy(t *testing.T) {
	ctx := context.Background()
	inMemStore := store.NewInMemoryStore()

	shop := createTestShop("store-copy-test")
	err := inMemStore.UpdateShop(ctx, shop)
	require.NoError(t, err)

	// Modify the original shop
	originalActive := shop.Active
	shop.Active = false
	shop.ClientID = "modified-key"

	// Verify stored shop is not affected
	retrieved, err := inMemStore.GetShopByID(ctx, shop.ShopID)
	require.NoError(t, err)
	assert.Equal(t, originalActive, retrieved.Active)
	assert.NotEqual(t, "modified-key", retrieved.ClientID)
}

// TestInMemoryStore_HealthCheck tests health check always succeeds
func TestInMemoryStore_HealthCheck(t *testing.T) {
	ctx := context.Background()
	inMemStore := store.NewInMemoryStore()

	// Health check should always succeed for in-memory store
	err := inMemStore.HealthCheck(ctx)
	require.NoError(t, err)

	// Even with empty store
	err = inMemStore.HealthCheck(ctx)
	require.NoError(t, err)

	// Even after operations
	shop := createTestShop("health-check-shop")
	require.NoError(t, inMemStore.UpdateShop(ctx, shop))
	err = inMemStore.HealthCheck(ctx)
	require.NoError(t, err)
}

// TestInMemoryStore_EmptyStore tests operations on empty store
func TestInMemoryStore_EmptyStore(t *testing.T) {
	ctx := context.Background()
	inMemStore := store.NewInMemoryStore()

	// GetShop on empty store should return error
	_, err := inMemStore.GetShopByID(ctx, "non-existent")
	require.Error(t, err)

	// DeleteShop on empty store should return error
	err = inMemStore.DeleteShop(ctx, "non-existent")
	require.Error(t, err)

	// SetShopActive on empty store should return error
	err = inMemStore.SetShopActive(ctx, "non-existent", true)
	require.Error(t, err)

	// HealthCheck should still succeed
	err = inMemStore.HealthCheck(ctx)
	require.NoError(t, err)
}

func TestInMemoryStore_ContextCancellation(t *testing.T) {
	s := store.NewInMemoryStore()

	shop := createTestShop("cancel-test")
	err := s.UpdateShop(context.Background(), shop)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = s.GetShopByID(ctx, "cancel-test")
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)

	err = s.SetShopActive(ctx, "cancel-test", true)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)

	err = s.DeleteShop(ctx, "cancel-test")
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)

	err = s.HealthCheck(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}
