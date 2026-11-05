package middleware_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/shopware/app-sdk-go/pkg/store"
	"github.com/shopware/app-sdk-go/pkg/store/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockStore is a mock implementation of store.ShopStore for testing
type mockStore struct {
	getShopByIDFunc   func(_ context.Context, _ string) (*store.Shop, error)
	updateShopFunc    func(_ context.Context, _ *store.Shop) error
	setShopActiveFunc func(ctx context.Context, shopID string, active bool) error
	deleteShopFunc    func(_ context.Context, _ string) error
	healthCheckFunc   func(ctx context.Context) error
}

func (m *mockStore) GetShopByID(ctx context.Context, shopID string) (*store.Shop, error) {
	if m.getShopByIDFunc != nil {
		return m.getShopByIDFunc(ctx, shopID)
	}
	return nil, errors.New("not implemented")
}

func (m *mockStore) UpdateShop(ctx context.Context, shop *store.Shop) error {
	if m.updateShopFunc != nil {
		return m.updateShopFunc(ctx, shop)
	}
	return errors.New("not implemented")
}

func (m *mockStore) SetShopActive(ctx context.Context, shopID string, active bool) error {
	if m.setShopActiveFunc != nil {
		return m.setShopActiveFunc(ctx, shopID, active)
	}
	return errors.New("not implemented")
}

func (m *mockStore) DeleteShop(ctx context.Context, shopID string) error {
	if m.deleteShopFunc != nil {
		return m.deleteShopFunc(ctx, shopID)
	}
	return errors.New("not implemented")
}

func (m *mockStore) HealthCheck(ctx context.Context) error {
	if m.healthCheckFunc != nil {
		return m.healthCheckFunc(ctx)
	}
	return errors.New("not implemented")
}

func TestRetryMiddleware_Success(t *testing.T) {
	testShop := &store.Shop{
		ShopID:  "test-shop",
		ShopURL: "https://test.com",
	}

	mock := &mockStore{
		getShopByIDFunc: func(_ context.Context, _ string) (*store.Shop, error) {
			return testShop, nil
		},
	}

	retryStore := middleware.NewRetryMiddleware(mock, middleware.RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
	})

	shop, err := retryStore.GetShopByID(context.Background(), "test-shop")
	require.NoError(t, err)
	assert.Equal(t, testShop.ShopID, shop.ShopID)
}

func TestRetryMiddleware_RetryableError_Success(t *testing.T) {
	testShop := &store.Shop{
		ShopID:  "test-shop",
		ShopURL: "https://test.com",
	}

	attemptCount := 0
	mock := &mockStore{
		getShopByIDFunc: func(_ context.Context, _ string) (*store.Shop, error) {
			attemptCount++
			if attemptCount < 3 {
				// Return retryable error for first 2 attempts
				return nil, &types.ProvisionedThroughputExceededException{
					Message: aws.String("throughput exceeded"),
				}
			}
			// Succeed on 3rd attempt
			return testShop, nil
		},
	}

	retryStore := middleware.NewRetryMiddleware(mock, middleware.RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
	})

	shop, err := retryStore.GetShopByID(context.Background(), "test-shop")
	require.NoError(t, err)
	assert.Equal(t, testShop.ShopID, shop.ShopID)
	assert.Equal(t, 3, attemptCount, "Should have retried 2 times before succeeding")
}

func TestRetryMiddleware_NonRetryableError_NoRetry(t *testing.T) {
	nonRetryableErr := errors.New("shop not found")
	attemptCount := 0

	mock := &mockStore{
		getShopByIDFunc: func(_ context.Context, _ string) (*store.Shop, error) {
			attemptCount++
			return nil, nonRetryableErr
		},
	}

	retryStore := middleware.NewRetryMiddleware(mock, middleware.RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
	})

	_, err := retryStore.GetShopByID(context.Background(), "test-shop")
	require.Error(t, err)
	assert.Equal(t, 1, attemptCount, "Should not retry non-retryable errors")
	assert.Equal(t, nonRetryableErr, err)
}

func TestRetryMiddleware_MaxRetriesExceeded(t *testing.T) {
	attemptCount := 0
	mock := &mockStore{
		getShopByIDFunc: func(_ context.Context, _ string) (*store.Shop, error) {
			attemptCount++
			return nil, &types.ProvisionedThroughputExceededException{
				Message: aws.String("throughput exceeded"),
			}
		},
	}

	retryStore := middleware.NewRetryMiddleware(mock, middleware.RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
	})

	_, err := retryStore.GetShopByID(context.Background(), "test-shop")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max retries exceeded")
	assert.Equal(t, 4, attemptCount, "Should attempt initial + 3 retries")
}

func TestRetryMiddleware_ContextCancellation(t *testing.T) {
	attemptCount := 0
	mock := &mockStore{
		getShopByIDFunc: func(_ context.Context, _ string) (*store.Shop, error) {
			attemptCount++
			return nil, &types.ProvisionedThroughputExceededException{
				Message: aws.String("throughput exceeded"),
			}
		},
	}

	retryStore := middleware.NewRetryMiddleware(mock, middleware.RetryConfig{
		MaxRetries: 10,
		BaseDelay:  100 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := retryStore.GetShopByID(ctx, "test-shop")
	elapsed := time.Since(start)

	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, elapsed, 500*time.Millisecond, "Should cancel quickly, not wait for all retries")
}

func TestRetryMiddleware_UpdateShop(t *testing.T) {
	testShop := &store.Shop{
		ShopID:  "test-shop",
		ShopURL: "https://test.com",
	}

	attemptCount := 0
	mock := &mockStore{
		updateShopFunc: func(_ context.Context, _ *store.Shop) error {
			attemptCount++
			if attemptCount < 2 {
				return &types.RequestLimitExceeded{
					Message: aws.String("request limit exceeded"),
				}
			}
			return nil
		},
	}

	retryStore := middleware.NewRetryMiddleware(mock, middleware.RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
	})

	err := retryStore.UpdateShop(context.Background(), testShop)
	require.NoError(t, err)
	assert.Equal(t, 2, attemptCount)
}

func TestRetryMiddleware_SetShopActive(t *testing.T) {
	attemptCount := 0
	mock := &mockStore{
		setShopActiveFunc: func(_ context.Context, _ string, _ bool) error {
			attemptCount++
			if attemptCount < 2 {
				return &types.InternalServerError{
					Message: aws.String("internal server error"),
				}
			}
			return nil
		},
	}

	retryStore := middleware.NewRetryMiddleware(mock, middleware.RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
	})

	err := retryStore.SetShopActive(context.Background(), "test-shop", true)
	require.NoError(t, err)
	assert.Equal(t, 2, attemptCount)
}

func TestRetryMiddleware_DeleteShop(t *testing.T) {
	attemptCount := 0
	mock := &mockStore{
		deleteShopFunc: func(_ context.Context, _ string) error {
			attemptCount++
			if attemptCount < 2 {
				return errors.New("connection timeout")
			}
			return nil
		},
	}

	retryStore := middleware.NewRetryMiddleware(mock, middleware.RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
	})

	err := retryStore.DeleteShop(context.Background(), "test-shop")
	require.NoError(t, err)
	assert.Equal(t, 2, attemptCount)
}

func TestRetryMiddleware_HealthCheck(t *testing.T) {
	attemptCount := 0
	mock := &mockStore{
		healthCheckFunc: func(_ context.Context) error {
			attemptCount++
			if attemptCount < 2 {
				return errors.New("ServiceUnavailable")
			}
			return nil
		},
	}

	retryStore := middleware.NewRetryMiddleware(mock, middleware.RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
	})

	err := retryStore.HealthCheck(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, attemptCount)
}

func TestRetryMiddleware_DefaultConfig(t *testing.T) {
	mock := &mockStore{
		getShopByIDFunc: func(_ context.Context, shopID string) (*store.Shop, error) {
			return &store.Shop{ShopID: shopID}, nil
		},
	}

	// Test with empty config - should use defaults
	retryStore := middleware.NewRetryMiddleware(mock, middleware.RetryConfig{})

	shop, err := retryStore.GetShopByID(context.Background(), "test-shop")
	require.NoError(t, err)
	assert.Equal(t, "test-shop", shop.ShopID)
}

func TestRetryMiddleware_RetryableErrorPatterns(t *testing.T) {
	tests := []struct {
		name             string
		err              error
		shouldRetry      bool
		expectedAttempts int
	}{
		{
			name:             "DynamoDB ProvisionedThroughputExceededException",
			err:              &types.ProvisionedThroughputExceededException{},
			shouldRetry:      true,
			expectedAttempts: 4, // initial + 3 retries
		},
		{
			name:             "DynamoDB RequestLimitExceeded",
			err:              &types.RequestLimitExceeded{},
			shouldRetry:      true,
			expectedAttempts: 4,
		},
		{
			name:             "DynamoDB InternalServerError",
			err:              &types.InternalServerError{},
			shouldRetry:      true,
			expectedAttempts: 4,
		},
		{
			name:             "ServiceUnavailable in message",
			err:              errors.New("ServiceUnavailable"),
			shouldRetry:      true,
			expectedAttempts: 4,
		},
		{
			name:             "timeout in message",
			err:              errors.New("request timeout"),
			shouldRetry:      true,
			expectedAttempts: 4,
		},
		{
			name:             "connection error",
			err:              errors.New("connection refused"),
			shouldRetry:      true,
			expectedAttempts: 4,
		},
		{
			name:             "Non-retryable error",
			err:              errors.New("shop not found"),
			shouldRetry:      false,
			expectedAttempts: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attemptCount := 0
			mock := &mockStore{
				getShopByIDFunc: func(_ context.Context, _ string) (*store.Shop, error) {
					attemptCount++
					return nil, tt.err
				},
			}

			retryStore := middleware.NewRetryMiddleware(mock, middleware.RetryConfig{
				MaxRetries: 3,
				BaseDelay:  10 * time.Millisecond,
			})

			_, err := retryStore.GetShopByID(context.Background(), "test-shop")
			require.Error(t, err)
			assert.Equal(t, tt.expectedAttempts, attemptCount)
		})
	}
}
