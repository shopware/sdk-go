package middleware

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/shopware/sdk-go/store"
)

const (
	defaultMaxRetries      = 3
	defaultBaseDelay       = 100 * time.Millisecond
	defaultMaxDelay        = 20 * time.Second
	defaultRequestTimeout  = 30 * time.Second
	exponentialBackoffBase = 2   // Base for exponential backoff calculation (2^attempt)
	jitterPercentage       = 0.1 // Jitter percentage for retry backoff (±10%)
)

// RetryConfig holds configuration for retry middleware
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts for failed requests
	// Default: 3
	MaxRetries int

	// BaseDelay is the initial retry delay with exponential backoff
	// Default: 100ms
	BaseDelay time.Duration

	// MaxDelay is the maximum retry delay (cap for exponential backoff)
	// Default: 20s
	MaxDelay time.Duration

	// RequestTimeout is the timeout for each individual request
	// Default: 30s
	RequestTimeout time.Duration
}

// RetryMiddleware wraps a ShopStore with retry logic
type RetryMiddleware struct {
	store          store.ShopStore
	maxRetries     int
	baseDelay      time.Duration
	maxDelay       time.Duration
	requestTimeout time.Duration
}

// NewRetryMiddleware creates a new retry middleware that wraps the given store
func NewRetryMiddleware(s store.ShopStore, cfg RetryConfig) store.ShopStore {
	// Apply defaults
	maxRetries := cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = defaultMaxRetries
	}

	baseDelay := cfg.BaseDelay
	if baseDelay == 0 {
		baseDelay = defaultBaseDelay
	}

	maxDelay := cfg.MaxDelay
	if maxDelay == 0 {
		maxDelay = defaultMaxDelay
	}

	requestTimeout := cfg.RequestTimeout
	if requestTimeout == 0 {
		requestTimeout = defaultRequestTimeout
	}

	return &RetryMiddleware{
		store:          s,
		maxRetries:     maxRetries,
		baseDelay:      baseDelay,
		maxDelay:       maxDelay,
		requestTimeout: requestTimeout,
	}
}

// isRetryableError determines if an error should trigger a retry
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific DynamoDB error types (if using DynamoDB)
	var provisionedThroughputExceeded *types.ProvisionedThroughputExceededException
	var requestLimitExceeded *types.RequestLimitExceeded
	var internalServerError *types.InternalServerError

	// Retryable errors
	if errors.As(err, &provisionedThroughputExceeded) {
		return true
	}
	if errors.As(err, &requestLimitExceeded) {
		return true
	}
	if errors.As(err, &internalServerError) {
		return true
	}

	// Check error message for common retryable patterns
	errMsg := err.Error()
	if strings.Contains(errMsg, "ServiceUnavailable") ||
		strings.Contains(errMsg, "RequestTimeout") ||
		strings.Contains(errMsg, "connection") ||
		strings.Contains(errMsg, "timeout") {
		return true
	}

	return false
}

// calculateBackoff calculates the delay for a retry attempt with exponential backoff and jitter
func (m *RetryMiddleware) calculateBackoff(attempt int) time.Duration {
	// Exponential backoff: baseDelay * 2^attempt
	delay := float64(m.baseDelay) * math.Pow(exponentialBackoffBase, float64(attempt))

	// Cap at maxDelay
	if delay > float64(m.maxDelay) {
		delay = float64(m.maxDelay)
	}

	// Add jitter (±10%) - using math/rand/v2 is acceptable for non-cryptographic jitter
	//nolint:gosec // G404: Jitter for retry backoff doesn't require cryptographic randomness
	jitter := delay * jitterPercentage * (rand.Float64()*2 - 1)
	finalDelay := time.Duration(delay + jitter)

	return finalDelay
}

// withRetry wraps an operation with retry logic
func (m *RetryMiddleware) withRetry(ctx context.Context, operation string, fn func(context.Context) error) error {
	var lastErr error

	for attempt := 0; attempt <= m.maxRetries; attempt++ {
		// Create context with timeout for this attempt
		attemptCtx, cancel := context.WithTimeout(ctx, m.requestTimeout)

		// Execute the operation
		lastErr = fn(attemptCtx)
		cancel()

		// Success
		if lastErr == nil {
			return nil
		}

		// Check if error is retryable
		if !isRetryableError(lastErr) {
			return lastErr
		}

		// Check if we should retry
		if attempt < m.maxRetries {
			delay := m.calculateBackoff(attempt)

			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
				timer.Stop()
				continue
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		}
	}

	return fmt.Errorf("max retries exceeded for %s: %w", operation, lastErr)
}

// GetShopByID implements store.ShopStore with retry logic
func (m *RetryMiddleware) GetShopByID(ctx context.Context, shopID string) (*store.Shop, error) {
	var shop *store.Shop

	err := m.withRetry(ctx, "GetShopByID", func(attemptCtx context.Context) error {
		var getErr error
		shop, getErr = m.store.GetShopByID(attemptCtx, shopID)
		return getErr
	})

	return shop, err
}

// UpdateShop implements store.ShopStore with retry logic
func (m *RetryMiddleware) UpdateShop(ctx context.Context, shop *store.Shop) error {
	return m.withRetry(ctx, "UpdateShop", func(attemptCtx context.Context) error {
		return m.store.UpdateShop(attemptCtx, shop)
	})
}

// SetShopActive implements store.ShopStore with retry logic
func (m *RetryMiddleware) SetShopActive(ctx context.Context, shopID string, active bool) error {
	return m.withRetry(ctx, "SetShopActive", func(attemptCtx context.Context) error {
		return m.store.SetShopActive(attemptCtx, shopID, active)
	})
}

// DeleteShop implements store.ShopStore with retry logic
func (m *RetryMiddleware) DeleteShop(ctx context.Context, shopID string) error {
	return m.withRetry(ctx, "DeleteShop", func(attemptCtx context.Context) error {
		return m.store.DeleteShop(attemptCtx, shopID)
	})
}

// HealthCheck implements store.ShopStore with retry logic
func (m *RetryMiddleware) HealthCheck(ctx context.Context) error {
	return m.withRetry(ctx, "HealthCheck", func(attemptCtx context.Context) error {
		return m.store.HealthCheck(attemptCtx)
	})
}
