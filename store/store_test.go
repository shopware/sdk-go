package store_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shopware/sdk-go/internal/testutil"
	"github.com/shopware/sdk-go/store"
	"github.com/stretchr/testify/suite"
)

// StoreTestSuite is a shared test suite that all store implementations must pass
type StoreTestSuite struct {
	suite.Suite

	store store.ShopStore
}

// createTestShop creates a test shop with given ID
func createTestShop(shopID string) *store.Shop {
	return testutil.NewTestShop(
		testutil.WithShopID(shopID),
		testutil.WithShopURL(fmt.Sprintf("https://%s.shopware.test", shopID)),
		testutil.WithShopSecret(fmt.Sprintf("secret-%s", shopID)),
		testutil.WithClientID(fmt.Sprintf("client-id-%s", shopID)),
		testutil.WithClientSecret(fmt.Sprintf("client-secret-%s", shopID)),
	)
}

// TestStoreShop_Success tests storing a new shop
func (s *StoreTestSuite) TestStoreShop_Success() {
	ctx := context.Background()
	shop := createTestShop("test-shop-1")

	err := s.store.UpdateShop(ctx, shop)
	s.Require().NoError(err)

	// Verify shop was stored
	retrieved, err := s.store.GetShopByID(ctx, shop.ShopID)
	s.Require().NoError(err)
	s.Equal(shop.ShopID, retrieved.ShopID)
	s.Equal(shop.ShopURL, retrieved.ShopURL)
	s.Equal(shop.ShopSecret, retrieved.ShopSecret)
	s.Equal(shop.ClientID, retrieved.ClientID)
	s.Equal(shop.ClientSecret, retrieved.ClientSecret)
	s.Equal(shop.Active, retrieved.Active)
}

// TestStoreShop_Update tests updating an existing shop
func (s *StoreTestSuite) TestStoreShop_Update() {
	ctx := context.Background()
	shop := createTestShop("test-shop-update")

	// Store initial shop
	err := s.store.UpdateShop(ctx, shop)
	s.Require().NoError(err)

	// Update shop fields
	shop.Active = false
	shop.ClientID = "new-client-id"
	shop.ClientSecret = "new-client-secret"

	// Store updated shop
	err = s.store.UpdateShop(ctx, shop)
	s.Require().NoError(err)

	// Verify updates
	retrieved, err := s.store.GetShopByID(ctx, shop.ShopID)
	s.Require().NoError(err)
	s.False(retrieved.Active)
	s.Equal("new-client-id", retrieved.ClientID)
	s.Equal("new-client-secret", retrieved.ClientSecret)
}

// TestGetShop_Success tests retrieving an existing shop
func (s *StoreTestSuite) TestGetShop_Success() {
	ctx := context.Background()
	shop := createTestShop("test-shop-get")

	err := s.store.UpdateShop(ctx, shop)
	s.Require().NoError(err)

	retrieved, err := s.store.GetShopByID(ctx, shop.ShopID)
	s.Require().NoError(err)
	s.NotNil(retrieved)
	s.Equal(shop.ShopID, retrieved.ShopID)
}

// TestGetShop_NotFound tests retrieving a non-existent shop
func (s *StoreTestSuite) TestGetShop_NotFound() {
	ctx := context.Background()

	retrieved, err := s.store.GetShopByID(ctx, "non-existent-shop")
	s.Require().Error(err)
	s.Nil(retrieved)
	s.Contains(err.Error(), "not found")
}

// TestGetShop_EmptyShopID tests retrieving with empty shop ID
func (s *StoreTestSuite) TestGetShop_EmptyShopID() {
	ctx := context.Background()

	retrieved, err := s.store.GetShopByID(ctx, "")
	s.Require().Error(err)
	s.Nil(retrieved)
}

// TestSetShopActive_Success tests updating shop active state
func (s *StoreTestSuite) TestSetShopActive_Success() {
	ctx := context.Background()
	shop := createTestShop("test-shop-status")

	err := s.store.UpdateShop(ctx, shop)
	s.Require().NoError(err)

	// Update active state
	err = s.store.SetShopActive(ctx, shop.ShopID, false)
	s.Require().NoError(err)

	// Verify active state was updated
	retrieved, err := s.store.GetShopByID(ctx, shop.ShopID)
	s.Require().NoError(err)
	s.False(retrieved.Active)
}

// TestSetShopActive_NotFound tests updating active state of non-existent shop
func (s *StoreTestSuite) TestSetShopActive_NotFound() {
	ctx := context.Background()

	err := s.store.SetShopActive(ctx, "non-existent-shop", true)
	s.Require().Error(err)
	s.Contains(err.Error(), "not found")
}

// TestDeleteShop_Success tests deleting a shop
func (s *StoreTestSuite) TestDeleteShop_Success() {
	ctx := context.Background()
	shop := createTestShop("test-shop-delete")

	err := s.store.UpdateShop(ctx, shop)
	s.Require().NoError(err)

	// Delete shop
	err = s.store.DeleteShop(ctx, shop.ShopID)
	s.Require().NoError(err)

	// Verify shop was deleted
	retrieved, err := s.store.GetShopByID(ctx, shop.ShopID)
	s.Require().Error(err)
	s.Nil(retrieved)
}

// TestDeleteShop_NotFound tests deleting a non-existent shop
func (s *StoreTestSuite) TestDeleteShop_NotFound() {
	ctx := context.Background()

	err := s.store.DeleteShop(ctx, "non-existent-shop")
	s.Require().Error(err)
	s.Contains(err.Error(), "not found")
}

// TestHealthCheck_Success tests health check
func (s *StoreTestSuite) TestHealthCheck_Success() {
	ctx := context.Background()

	err := s.store.HealthCheck(ctx)
	s.Require().NoError(err)
}

// TestHealthCheck_AfterOperations tests health check after CRUD operations
func (s *StoreTestSuite) TestHealthCheck_AfterOperations() {
	ctx := context.Background()
	shop := createTestShop("test-shop-health")

	s.Require().NoError(s.store.UpdateShop(ctx, shop))
	_, err := s.store.GetShopByID(ctx, shop.ShopID)
	s.Require().NoError(err)
	s.Require().NoError(s.store.SetShopActive(ctx, shop.ShopID, false))
	s.Require().NoError(s.store.DeleteShop(ctx, shop.ShopID))

	// Health check should still pass
	err = s.store.HealthCheck(ctx)
	s.Require().NoError(err)
}

// TestConcurrentStoreAndGet tests concurrent store and get operations
func (s *StoreTestSuite) TestConcurrentStoreAndGet() {
	ctx := context.Background()
	var wg sync.WaitGroup
	numGoroutines := 10
	errs := make(chan error, numGoroutines*2)

	for i := range numGoroutines {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			shopID := fmt.Sprintf("concurrent-shop-%d", index)
			shop := createTestShop(shopID)

			if err := s.store.UpdateShop(ctx, shop); err != nil {
				errs <- fmt.Errorf("UpdateShop %s: %w", shopID, err)
				return
			}

			retrieved, err := s.store.GetShopByID(ctx, shopID)
			if err != nil {
				errs <- fmt.Errorf("GetShopByID %s: %w", shopID, err)
				return
			}
			if retrieved.ShopID != shopID {
				errs <- fmt.Errorf("expected ShopID %s, got %s", shopID, retrieved.ShopID)
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		s.Require().NoError(err)
	}
}

// TestConcurrentUpdates tests concurrent updates to the same shop
func (s *StoreTestSuite) TestConcurrentUpdates() {
	ctx := context.Background()
	shop := createTestShop("test-shop-concurrent-updates")

	err := s.store.UpdateShop(ctx, shop)
	s.Require().NoError(err)

	var wg sync.WaitGroup
	numGoroutines := 5

	errs := make(chan error, numGoroutines)

	for i := range numGoroutines {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			active := index%2 == 0
			if setErr := s.store.SetShopActive(ctx, shop.ShopID, active); setErr != nil {
				errs <- setErr
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		s.Require().NoError(err)
	}

	retrieved, err := s.store.GetShopByID(ctx, shop.ShopID)
	s.Require().NoError(err)
	s.NotNil(retrieved)
}

// TestMultipleShops tests storing and retrieving multiple shops
func (s *StoreTestSuite) TestMultipleShops() {
	ctx := context.Background()
	numShops := 5

	// Store multiple shops
	for i := range numShops {
		shop := createTestShop(fmt.Sprintf("multi-shop-%d", i))
		err := s.store.UpdateShop(ctx, shop)
		s.Require().NoError(err)
	}

	// Retrieve each shop
	for i := range numShops {
		shopID := fmt.Sprintf("multi-shop-%d", i)
		retrieved, err := s.store.GetShopByID(ctx, shopID)
		s.Require().NoError(err)
		s.Equal(shopID, retrieved.ShopID)
	}
}

// TestShopIsolation tests that shops are isolated from each other
func (s *StoreTestSuite) TestShopIsolation() {
	ctx := context.Background()
	shop1 := createTestShop("isolation-shop-1")
	shop2 := createTestShop("isolation-shop-2")

	// Store both shops
	err := s.store.UpdateShop(ctx, shop1)
	s.Require().NoError(err)
	err = s.store.UpdateShop(ctx, shop2)
	s.Require().NoError(err)

	// Update shop1
	err = s.store.SetShopActive(ctx, shop1.ShopID, false)
	s.Require().NoError(err)

	// Verify shop1 was updated
	retrieved1, err := s.store.GetShopByID(ctx, shop1.ShopID)
	s.Require().NoError(err)
	s.False(retrieved1.Active)

	// Verify shop2 was not affected
	retrieved2, err := s.store.GetShopByID(ctx, shop2.ShopID)
	s.Require().NoError(err)
	s.True(retrieved2.Active)
}

// TestStoreShop_PreservesAllFields tests that all shop fields are preserved
func (s *StoreTestSuite) TestStoreShop_PreservesAllFields() {
	ctx := context.Background()
	now := time.Now()
	rotatedAt := now.Add(-30 * time.Second)
	shop := &store.Shop{
		ShopID:                         "field-test-shop",
		ShopURL:                        "https://field-test.shopware.test",
		ShopSecret:                     "test-shop-secret-123",
		ClientID:                       "test-client-id-456",
		ClientSecret:                   "test-client-secret-789",
		Active:                         false,
		RegistrationConfirmed:          true,
		PendingShopSecret:              "pending-secret-abc",
		PendingShopURL:                 "https://pending.shopware.test",
		PreviousShopSecret:             "previous-secret-xyz",
		SecretsRotatedAt:               &rotatedAt,
		HasVerifiedWithDoubleSignature: true,
		CreatedAt:                      now,
		UpdatedAt:                      now,
	}

	err := s.store.UpdateShop(ctx, shop)
	s.Require().NoError(err)

	retrieved, err := s.store.GetShopByID(ctx, shop.ShopID)
	s.Require().NoError(err)
	s.Equal(shop.ShopID, retrieved.ShopID)
	s.Equal(shop.ShopURL, retrieved.ShopURL)
	s.Equal(shop.ShopSecret, retrieved.ShopSecret)
	s.Equal(shop.ClientID, retrieved.ClientID)
	s.Equal(shop.ClientSecret, retrieved.ClientSecret)
	s.Equal(shop.Active, retrieved.Active)
	s.Equal(shop.RegistrationConfirmed, retrieved.RegistrationConfirmed)
	s.Equal(shop.PendingShopSecret, retrieved.PendingShopSecret)
	s.Equal(shop.PendingShopURL, retrieved.PendingShopURL)
	s.Equal(shop.PreviousShopSecret, retrieved.PreviousShopSecret)
	s.Equal(shop.HasVerifiedWithDoubleSignature, retrieved.HasVerifiedWithDoubleSignature)
	s.Require().NotNil(retrieved.SecretsRotatedAt)
	s.WithinDuration(*shop.SecretsRotatedAt, *retrieved.SecretsRotatedAt, time.Second)
	s.False(retrieved.CreatedAt.IsZero())
	s.False(retrieved.UpdatedAt.IsZero())
}

func (s *StoreTestSuite) TestStoreShop_NilSecretsRotatedAt() {
	ctx := context.Background()
	shop := &store.Shop{
		ShopID:           "nil-rotated-shop",
		ShopURL:          "https://nil-rotated.test",
		ShopSecret:       "secret",
		SecretsRotatedAt: nil,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	err := s.store.UpdateShop(ctx, shop)
	s.Require().NoError(err)

	retrieved, err := s.store.GetShopByID(ctx, shop.ShopID)
	s.Require().NoError(err)
	s.Nil(retrieved.SecretsRotatedAt)
}

// TestDeleteShop_VerifyRemoval tests that deleted shop cannot be retrieved
func (s *StoreTestSuite) TestDeleteShop_VerifyRemoval() {
	ctx := context.Background()
	shop := createTestShop("test-shop-verify-deletion")

	// Store and verify
	err := s.store.UpdateShop(ctx, shop)
	s.Require().NoError(err)
	_, err = s.store.GetShopByID(ctx, shop.ShopID)
	s.Require().NoError(err)

	// Delete
	err = s.store.DeleteShop(ctx, shop.ShopID)
	s.Require().NoError(err)

	// Verify deletion
	_, err = s.store.GetShopByID(ctx, shop.ShopID)
	s.Require().Error(err)
}

// TestSetShopActive_UpdatesActiveState verifies active state transitions.
func (s *StoreTestSuite) TestSetShopActive_UpdatesActiveState() {
	ctx := context.Background()
	shop := createTestShop("test-shop-timestamp")

	// Store shop
	err := s.store.UpdateShop(ctx, shop)
	s.Require().NoError(err)

	// Update active state
	err = s.store.SetShopActive(ctx, shop.ShopID, false)
	s.Require().NoError(err)

	// Get updated state
	retrieved2, err := s.store.GetShopByID(ctx, shop.ShopID)
	s.Require().NoError(err)

	// Verify active state changed
	s.False(retrieved2.Active)
}

// TestContextCancellation tests that operations respect context cancellation.
// All store implementations must propagate context cancellation.
func (s *StoreTestSuite) TestContextCancellation() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	shop := createTestShop("test-shop-cancelled")

	s.Require().Error(s.store.UpdateShop(ctx, shop))
}
