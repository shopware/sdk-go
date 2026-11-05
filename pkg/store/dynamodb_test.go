package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/shopware/app-sdk-go/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockDynamoDBClient mocks the DynamoDB client
type MockDynamoDBClient struct {
	mock.Mock
}

func (m *MockDynamoDBClient) GetItem(
	ctx context.Context,
	params *dynamodb.GetItemInput,
	optFns ...func(*dynamodb.Options),
) (*dynamodb.GetItemOutput, error) {
	args := m.Called(ctx, params, optFns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dynamodb.GetItemOutput), args.Error(1)
}

func (m *MockDynamoDBClient) Query(
	ctx context.Context,
	params *dynamodb.QueryInput,
	optFns ...func(*dynamodb.Options),
) (*dynamodb.QueryOutput, error) {
	args := m.Called(ctx, params, optFns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dynamodb.QueryOutput), args.Error(1)
}

func (m *MockDynamoDBClient) PutItem(
	ctx context.Context,
	params *dynamodb.PutItemInput,
	optFns ...func(*dynamodb.Options),
) (*dynamodb.PutItemOutput, error) {
	args := m.Called(ctx, params, optFns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dynamodb.PutItemOutput), args.Error(1)
}

func (m *MockDynamoDBClient) UpdateItem(
	ctx context.Context,
	params *dynamodb.UpdateItemInput,
	optFns ...func(*dynamodb.Options),
) (*dynamodb.UpdateItemOutput, error) {
	args := m.Called(ctx, params, optFns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dynamodb.UpdateItemOutput), args.Error(1)
}

func (m *MockDynamoDBClient) DeleteItem(
	ctx context.Context,
	params *dynamodb.DeleteItemInput,
	optFns ...func(*dynamodb.Options),
) (*dynamodb.DeleteItemOutput, error) {
	args := m.Called(ctx, params, optFns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dynamodb.DeleteItemOutput), args.Error(1)
}

func (m *MockDynamoDBClient) DescribeTable(
	ctx context.Context,
	params *dynamodb.DescribeTableInput,
	optFns ...func(*dynamodb.Options),
) (*dynamodb.DescribeTableOutput, error) {
	args := m.Called(ctx, params, optFns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*dynamodb.DescribeTableOutput), args.Error(1)
}

// Helper to create a DynamoDB item from a Shop
func createDynamoDBItem(shop *store.Shop) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"id":           &types.AttributeValueMemberS{Value: shop.ShopID},
		"url":          &types.AttributeValueMemberS{Value: shop.ShopURL},
		"secret":       &types.AttributeValueMemberS{Value: shop.ShopSecret},
		"clientId":     &types.AttributeValueMemberS{Value: shop.ClientID},
		"clientSecret": &types.AttributeValueMemberS{Value: shop.ClientSecret},
		"active":       &types.AttributeValueMemberBOOL{Value: shop.Active},
		"confirmed":    &types.AttributeValueMemberBOOL{Value: shop.RegistrationConfirmed},
		"created_at":   &types.AttributeValueMemberS{Value: shop.CreatedAt.UTC().Format(time.RFC3339)},
		"updated_at":   &types.AttributeValueMemberS{Value: shop.UpdatedAt.UTC().Format(time.RFC3339)},
		"gsi_pk":       &types.AttributeValueMemberS{Value: shop.ShopURL},
	}
}

// Helper to create test store config
func createTestDynamoDBConfig() store.DynamoDBConfig {
	return store.DynamoDBConfig{
		TableName: "test-table",
		// Use defaults for retry config
	}
}

// Test DynamoDB store creation with default endpoint
func TestNewDynamoDBStore_DefaultEndpoint(t *testing.T) {
	config := store.DynamoDBConfig{
		TableName: "test-shops",
		Region:    "us-east-1",
	}

	dynStore, err := store.NewDynamoDBStore(config)

	// The function will either succeed (if AWS credentials are available)
	// or fail with a specific error (if credentials are not available)
	if err != nil {
		// Expected error when AWS credentials are not configured
		assert.Contains(t, err.Error(), "failed to load AWS config")
	} else {
		// If credentials are available, store should be created successfully
		assert.NotNil(t, dynStore)

		// Verify it's a DynamoDB store by checking HealthCheck behavior
		// (will fail if DynamoDB is not accessible, but that's expected)
		err = dynStore.HealthCheck(context.Background())
		// We don't assert on this error because it depends on DynamoDB availability
		_ = err
	}
}

// Test DynamoDB store creation with custom endpoint
func TestNewDynamoDBStore_CustomEndpoint(t *testing.T) {
	config := store.DynamoDBConfig{
		TableName: "test-shops",
		Region:    "us-east-1",
		Endpoint:  "http://localhost:8000",
	}

	dynStore, err := store.NewDynamoDBStore(config)

	// The function will either succeed (if AWS credentials are available)
	// or fail with a specific error (if credentials are not available)
	if err != nil {
		// Expected error when AWS credentials are not configured
		assert.Contains(t, err.Error(), "failed to load AWS config")
	} else {
		// If credentials are available, store should be created successfully
		assert.NotNil(t, dynStore)

		// The custom endpoint should be used (we can't verify this directly without
		// making a request, but we can at least verify the store was created)
		err = dynStore.HealthCheck(context.Background())
		// We don't assert on this error because it depends on DynamoDB Local availability
		_ = err
	}
}

// Test NewDynamoDBStoreWithClient directly
func TestNewDynamoDBStoreWithClient(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()

	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	require.NotNil(t, dynStore)

	// Verify the store is usable by calling a method
	mockClient.On("DescribeTable", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.DescribeTableOutput{
		Table: &types.TableDescription{
			TableName: aws.String("test-table"),
		},
	}, nil)

	err := dynStore.HealthCheck(context.Background())
	require.NoError(t, err)
	mockClient.AssertExpectations(t)
}

// =============================================================================
// GetShop Tests
// =============================================================================

func TestDynamoDBStore_GetShop_UsesConsistentRead(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	now := time.Now().UTC()
	shop := &store.Shop{
		ShopID:    "test-shop",
		ShopURL:   "https://test.com",
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	mockClient.On("GetItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.GetItemOutput{
		Item: createDynamoDBItem(shop),
	}, nil)

	_, err := dynStore.GetShopByID(context.Background(), "test-shop")
	require.NoError(t, err)

	input := mockClient.Calls[0].Arguments[1].(*dynamodb.GetItemInput)
	require.NotNil(t, input.ConsistentRead)
	assert.True(t, *input.ConsistentRead)
}

func TestDynamoDBStore_GetShop_NotFound_ReturnsSentinelError(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("GetItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.GetItemOutput{
		Item: nil,
	}, nil)

	_, err := dynStore.GetShopByID(context.Background(), "nonexistent")

	require.Error(t, err)
	assert.ErrorIs(t, err, store.ErrShopNotFound)
}

func TestDynamoDBStore_GetShop_Success(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	now := time.Now().UTC()
	shop := &store.Shop{
		ShopID:       "test-shop",
		ShopURL:      "https://test.com",
		ShopSecret:   "test-secret",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		Active:       true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	mockClient.On("GetItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.GetItemOutput{
		Item: createDynamoDBItem(shop),
	}, nil)

	result, err := dynStore.GetShopByID(context.Background(), "test-shop")

	require.NoError(t, err)
	assert.Equal(t, shop.ShopID, result.ShopID)
	assert.Equal(t, shop.ShopURL, result.ShopURL)
	assert.Equal(t, shop.Active, result.Active)
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_GetShop_NotFound(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("GetItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.GetItemOutput{
		Item: nil,
	}, nil)

	result, err := dynStore.GetShopByID(context.Background(), "nonexistent")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "shop not found")
	assert.Nil(t, result)
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_GetShop_DynamoDBError(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("GetItem", mock.Anything, mock.Anything, mock.Anything).Return(
		nil,
		errors.New("dynamodb connection error"),
	)

	result, err := dynStore.GetShopByID(context.Background(), "test-shop")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get shop")
	assert.Nil(t, result)
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_GetShop_MissingTimestamps(t *testing.T) {
	// Tests that shops created by other SDKs (without timestamp columns) can be loaded
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("GetItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.GetItemOutput{
		Item: map[string]types.AttributeValue{
			"id":           &types.AttributeValueMemberS{Value: "legacy-shop"},
			"url":          &types.AttributeValueMemberS{Value: "https://legacy.shop"},
			"secret":       &types.AttributeValueMemberS{Value: "secret"},
			"clientId":     &types.AttributeValueMemberS{Value: "client-id"},
			"clientSecret": &types.AttributeValueMemberS{Value: "client-secret"},
			"active":       &types.AttributeValueMemberBOOL{Value: true},
			// Note: no created_at or updated_at fields (simulating other SDK behavior)
		},
	}, nil)

	result, err := dynStore.GetShopByID(context.Background(), "legacy-shop")

	require.NoError(t, err)
	assert.Equal(t, "legacy-shop", result.ShopID)
	assert.Equal(t, "https://legacy.shop", result.ShopURL)
	assert.True(t, result.Active)
	// Timestamps should be zero time since they were not present
	assert.True(t, result.CreatedAt.IsZero())
	assert.True(t, result.UpdatedAt.IsZero())
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_GetShop_MissingConfirmedDefaultsToTrue(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("GetItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.GetItemOutput{
		Item: map[string]types.AttributeValue{
			"id":           &types.AttributeValueMemberS{Value: "legacy-shop"},
			"url":          &types.AttributeValueMemberS{Value: "https://legacy.shop"},
			"secret":       &types.AttributeValueMemberS{Value: "secret"},
			"clientId":     &types.AttributeValueMemberS{Value: "client-id"},
			"clientSecret": &types.AttributeValueMemberS{Value: "client-secret"},
			"active":       &types.AttributeValueMemberBOOL{Value: true},
		},
	}, nil)

	result, err := dynStore.GetShopByID(context.Background(), "legacy-shop")

	require.NoError(t, err)
	assert.True(t, result.RegistrationConfirmed)
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_GetShop_MissingConfirmedWithPendingSecretAndNoCredentialsDefaultsToFalse(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("GetItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.GetItemOutput{
		Item: map[string]types.AttributeValue{
			"id":                &types.AttributeValueMemberS{Value: "pending-shop"},
			"url":               &types.AttributeValueMemberS{Value: "https://pending.shop"},
			"secret":            &types.AttributeValueMemberS{Value: ""},
			"clientId":          &types.AttributeValueMemberS{Value: ""},
			"clientSecret":      &types.AttributeValueMemberS{Value: ""},
			"active":            &types.AttributeValueMemberBOOL{Value: false},
			"pendingShopSecret": &types.AttributeValueMemberS{Value: "pending-secret"},
			"pendingShopUrl":    &types.AttributeValueMemberS{Value: "https://pending.shop"},
		},
	}, nil)

	result, err := dynStore.GetShopByID(context.Background(), "pending-shop")

	require.NoError(t, err)
	assert.False(t, result.RegistrationConfirmed)
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_GetShop_LegacyGoPendingAttributeNames(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("GetItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.GetItemOutput{
		Item: map[string]types.AttributeValue{
			"id":             &types.AttributeValueMemberS{Value: "pending-shop"},
			"url":            &types.AttributeValueMemberS{Value: "https://pending.shop"},
			"secret":         &types.AttributeValueMemberS{Value: ""},
			"clientId":       &types.AttributeValueMemberS{Value: ""},
			"clientSecret":   &types.AttributeValueMemberS{Value: ""},
			"active":         &types.AttributeValueMemberBOOL{Value: false},
			"pendingSecret":  &types.AttributeValueMemberS{Value: "pending-secret"},
			"pendingUrl":     &types.AttributeValueMemberS{Value: "https://pending.shop"},
			"previousSecret": &types.AttributeValueMemberS{Value: "previous-secret"},
		},
	}, nil)

	result, err := dynStore.GetShopByID(context.Background(), "pending-shop")

	require.NoError(t, err)
	assert.False(t, result.RegistrationConfirmed)
	assert.Equal(t, "pending-secret", result.PendingShopSecret)
	assert.Equal(t, "https://pending.shop", result.PendingShopURL)
	assert.Equal(t, "previous-secret", result.PreviousShopSecret)
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_GetShop_ConfirmedFalse(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("GetItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.GetItemOutput{
		Item: map[string]types.AttributeValue{
			"id":           &types.AttributeValueMemberS{Value: "unconfirmed-shop"},
			"url":          &types.AttributeValueMemberS{Value: "https://unconfirmed.shop"},
			"secret":       &types.AttributeValueMemberS{Value: "secret"},
			"clientId":     &types.AttributeValueMemberS{Value: ""},
			"clientSecret": &types.AttributeValueMemberS{Value: ""},
			"active":       &types.AttributeValueMemberBOOL{Value: false},
			"confirmed":    &types.AttributeValueMemberBOOL{Value: false},
		},
	}, nil)

	result, err := dynStore.GetShopByID(context.Background(), "unconfirmed-shop")

	require.NoError(t, err)
	assert.False(t, result.RegistrationConfirmed)
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_GetShop_InvalidCreatedAtTimestamp(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("GetItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.GetItemOutput{
		Item: map[string]types.AttributeValue{
			"id":           &types.AttributeValueMemberS{Value: "test"},
			"url":          &types.AttributeValueMemberS{Value: "https://test.com"},
			"secret":       &types.AttributeValueMemberS{Value: "secret"},
			"clientId":     &types.AttributeValueMemberS{Value: "key"},
			"clientSecret": &types.AttributeValueMemberS{Value: "secret"},
			"active":       &types.AttributeValueMemberBOOL{Value: true},
			"created_at":   &types.AttributeValueMemberS{Value: "invalid-timestamp"},
			"updated_at":   &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
			"gsi_pk":       &types.AttributeValueMemberS{Value: "https://test.com"},
		},
	}, nil)

	result, err := dynStore.GetShopByID(context.Background(), "test-shop")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse created_at")
	assert.Nil(t, result)
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_GetShop_InvalidUpdatedAtTimestamp(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("GetItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.GetItemOutput{
		Item: map[string]types.AttributeValue{
			"id":           &types.AttributeValueMemberS{Value: "test"},
			"url":          &types.AttributeValueMemberS{Value: "https://test.com"},
			"secret":       &types.AttributeValueMemberS{Value: "secret"},
			"clientId":     &types.AttributeValueMemberS{Value: "key"},
			"clientSecret": &types.AttributeValueMemberS{Value: "secret"},
			"active":       &types.AttributeValueMemberBOOL{Value: true},
			"created_at":   &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
			"updated_at":   &types.AttributeValueMemberS{Value: "invalid-timestamp"},
			"gsi_pk":       &types.AttributeValueMemberS{Value: "https://test.com"},
		},
	}, nil)

	result, err := dynStore.GetShopByID(context.Background(), "test-shop")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse updated_at")
	assert.Nil(t, result)
	mockClient.AssertExpectations(t)
}

// =============================================================================
// StoreShop Tests
// =============================================================================

func TestDynamoDBStore_StoreShop_Success(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	shop := &store.Shop{
		ShopID:                "test-shop",
		ShopURL:               "https://test.com",
		ShopSecret:            "test-secret",
		ClientID:              "test-client-id",
		ClientSecret:          "test-client-secret",
		Active:                false,
		RegistrationConfirmed: true,
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}

	var capturedInput *dynamodb.UpdateItemInput
	mockClient.On("UpdateItem", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			capturedInput = args.Get(1).(*dynamodb.UpdateItemInput)
		}).
		Return(&dynamodb.UpdateItemOutput{}, nil)

	err := dynStore.UpdateShop(context.Background(), shop)

	require.NoError(t, err)

	input := capturedInput
	require.NotNil(t, input)
	require.NotNil(t, input.UpdateExpression)
	assert.Contains(t, *input.UpdateExpression, "SET #active = :active, confirmed = :confirmed, #u = :url")
	assert.NotContains(t, *input.UpdateExpression, "SET active = :active")
	assert.Contains(t, *input.UpdateExpression, "pendingShopSecret = :pendingShopSecret")
	assert.Contains(t, *input.UpdateExpression, "pendingShopUrl = :pendingShopUrl")
	assert.Contains(t, *input.UpdateExpression, "previousShopSecret = :previousShopSecret")
	assert.NotContains(t, *input.UpdateExpression, "pendingSecret = :pendingSecret")
	assert.NotContains(t, *input.UpdateExpression, "pendingUrl = :pendingUrl")
	assert.NotContains(t, *input.UpdateExpression, "previousSecret = :previousSecret")
	assert.Contains(t, *input.UpdateExpression, "created_at = if_not_exists(created_at, :created_at)")
	assert.Contains(t, *input.UpdateExpression, "updated_at = :updated_at")
	assert.NotContains(t, *input.UpdateExpression, "commercialLicense")
	assert.Equal(t, "active", input.ExpressionAttributeNames["#active"])
	assert.Equal(t, "url", input.ExpressionAttributeNames["#u"])
	assert.True(t, input.ExpressionAttributeValues[":confirmed"].(*types.AttributeValueMemberBOOL).Value)

	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_StoreShop_ConfirmedUsesShopState(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	shop := &store.Shop{
		ShopID:                "test-shop",
		ShopURL:               "https://test.com",
		Active:                false,
		RegistrationConfirmed: true,
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}

	mockClient.On("UpdateItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.UpdateItemOutput{}, nil)

	err := dynStore.UpdateShop(context.Background(), shop)

	require.NoError(t, err)

	input := mockClient.Calls[0].Arguments[1].(*dynamodb.UpdateItemInput)
	assert.True(t, input.ExpressionAttributeValues[":confirmed"].(*types.AttributeValueMemberBOOL).Value)
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_StoreShop_ConfirmedFalseUsesShopStateWithCredentials(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	shop := &store.Shop{
		ShopID:                "test-shop",
		ShopURL:               "https://test.com",
		ClientID:              "client-id",
		ClientSecret:          "client-secret",
		RegistrationConfirmed: false,
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}

	mockClient.On("UpdateItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.UpdateItemOutput{}, nil)

	err := dynStore.UpdateShop(context.Background(), shop)

	require.NoError(t, err)

	input := mockClient.Calls[0].Arguments[1].(*dynamodb.UpdateItemInput)
	assert.False(t, input.ExpressionAttributeValues[":confirmed"].(*types.AttributeValueMemberBOOL).Value)
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_StoreShop_ConfirmedFalseWithoutCredentials(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	shop := &store.Shop{
		ShopID:    "test-shop",
		ShopURL:   "https://test.com",
		Active:    false,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	mockClient.On("UpdateItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.UpdateItemOutput{}, nil)

	err := dynStore.UpdateShop(context.Background(), shop)

	require.NoError(t, err)

	input := mockClient.Calls[0].Arguments[1].(*dynamodb.UpdateItemInput)
	assert.False(t, input.ExpressionAttributeValues[":confirmed"].(*types.AttributeValueMemberBOOL).Value)
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_StoreShop_UpdateItemError(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	shop := &store.Shop{
		ShopID:       "test-shop",
		ShopURL:      "https://test.com",
		ShopSecret:   "test-secret",
		ClientID:     "",
		ClientSecret: "",
		Active:       false,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	mockClient.On("UpdateItem", mock.Anything, mock.Anything, mock.Anything).Return(
		nil,
		errors.New("update item failed"),
	)

	err := dynStore.UpdateShop(context.Background(), shop)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to store shop")
	mockClient.AssertExpectations(t)
}

// =============================================================================
// SetShopActive Tests
// =============================================================================

func TestDynamoDBStore_SetShopActive_Success(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("UpdateItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.UpdateItemOutput{}, nil)

	err := dynStore.SetShopActive(context.Background(), "test-shop", true)

	require.NoError(t, err)
	input := mockClient.Calls[0].Arguments[1].(*dynamodb.UpdateItemInput)
	assert.Equal(t, "SET #active = :active, updated_at = :updated_at", *input.UpdateExpression)
	assert.Contains(t, input.ExpressionAttributeValues, ":updated_at")
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_SetShopActive_NotFound(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	// DynamoDB returns ConditionalCheckFailedException for non-existent items
	var condErr *types.ConditionalCheckFailedException
	mockClient.On("UpdateItem", mock.Anything, mock.Anything, mock.Anything).Return(
		nil,
		condErr,
	)

	err := dynStore.SetShopActive(context.Background(), "nonexistent", true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update shop active state")
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_SetShopActive_UpdateError(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("UpdateItem", mock.Anything, mock.Anything, mock.Anything).Return(
		nil,
		errors.New("update failed"),
	)

	err := dynStore.SetShopActive(context.Background(), "test-shop", true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update shop active state")
	mockClient.AssertExpectations(t)
}

// =============================================================================
// DeleteShop Tests
// =============================================================================

func TestDynamoDBStore_DeleteShop_Success(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("DeleteItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.DeleteItemOutput{}, nil)

	err := dynStore.DeleteShop(context.Background(), "test-shop")

	require.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_DeleteShop_DeleteError(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("DeleteItem", mock.Anything, mock.Anything, mock.Anything).Return(
		nil,
		errors.New("delete failed"),
	)

	err := dynStore.DeleteShop(context.Background(), "test-shop")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete shop")
	mockClient.AssertExpectations(t)
}

// =============================================================================
// HealthCheck Tests
// =============================================================================

func TestDynamoDBStore_HealthCheck_Success(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("DescribeTable", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.DescribeTableOutput{
		Table: &types.TableDescription{
			TableName: aws.String("test-table"),
		},
	}, nil)

	err := dynStore.HealthCheck(context.Background())

	require.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_HealthCheck_TableNotFound(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	var resNotFound *types.ResourceNotFoundException
	mockClient.On("DescribeTable", mock.Anything, mock.Anything, mock.Anything).Return(
		nil,
		resNotFound,
	)

	err := dynStore.HealthCheck(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "dynamodb health check failed")
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_HealthCheck_DescribeError(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("DescribeTable", mock.Anything, mock.Anything, mock.Anything).Return(
		nil,
		errors.New("describe table failed"),
	)

	err := dynStore.HealthCheck(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "dynamodb health check failed")
	mockClient.AssertExpectations(t)
}

// =============================================================================
// Active Field Tests
// =============================================================================

func TestDynamoDBStore_GetShop_ActiveTrue(t *testing.T) {
	// Tests that shops with Active=true are loaded correctly
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("GetItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.GetItemOutput{
		Item: map[string]types.AttributeValue{
			"id":           &types.AttributeValueMemberS{Value: "active-shop"},
			"url":          &types.AttributeValueMemberS{Value: "https://active.shop"},
			"secret":       &types.AttributeValueMemberS{Value: "secret"},
			"clientId":     &types.AttributeValueMemberS{Value: "client-id"},
			"clientSecret": &types.AttributeValueMemberS{Value: "client-secret"},
			"active":       &types.AttributeValueMemberBOOL{Value: true},
		},
	}, nil)

	result, err := dynStore.GetShopByID(context.Background(), "active-shop")

	require.NoError(t, err)
	assert.Equal(t, "active-shop", result.ShopID)
	assert.True(t, result.Active)
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_GetShop_ActiveFalse(t *testing.T) {
	// Tests that shops with Active=false are loaded correctly
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("GetItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.GetItemOutput{
		Item: map[string]types.AttributeValue{
			"id":           &types.AttributeValueMemberS{Value: "inactive-shop"},
			"url":          &types.AttributeValueMemberS{Value: "https://inactive.shop"},
			"secret":       &types.AttributeValueMemberS{Value: "secret"},
			"clientId":     &types.AttributeValueMemberS{Value: "client-id"},
			"clientSecret": &types.AttributeValueMemberS{Value: "client-secret"},
			"active":       &types.AttributeValueMemberBOOL{Value: false},
		},
	}, nil)

	result, err := dynStore.GetShopByID(context.Background(), "inactive-shop")

	require.NoError(t, err)
	assert.Equal(t, "inactive-shop", result.ShopID)
	assert.False(t, result.Active)
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_UpdateShop_SecretRotationFields(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	now := time.Now().UTC()
	rotatedAt := time.Unix(123, 0).UTC()
	shop := &store.Shop{
		ShopID:                         "full-shop",
		ShopURL:                        "https://full.test",
		ShopSecret:                     "secret",
		ClientID:                       "cid",
		ClientSecret:                   "csecret",
		Active:                         true,
		PendingShopSecret:              "pending",
		PendingShopURL:                 "https://pending.test",
		PreviousShopSecret:             "previous",
		SecretsRotatedAt:               &rotatedAt,
		HasVerifiedWithDoubleSignature: true,
		CreatedAt:                      now,
		UpdatedAt:                      now,
	}

	var capturedInput *dynamodb.UpdateItemInput
	mockClient.On("UpdateItem", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			capturedInput = args.Get(1).(*dynamodb.UpdateItemInput)
		}).
		Return(&dynamodb.UpdateItemOutput{}, nil)

	err := dynStore.UpdateShop(context.Background(), shop)
	require.NoError(t, err)
	require.NotNil(t, capturedInput)

	values := capturedInput.ExpressionAttributeValues
	assert.Equal(t, shop.PendingShopSecret, values[":pendingShopSecret"].(*types.AttributeValueMemberS).Value)
	assert.Equal(t, shop.PendingShopURL, values[":pendingShopUrl"].(*types.AttributeValueMemberS).Value)
	assert.Equal(t, shop.PreviousShopSecret, values[":previousShopSecret"].(*types.AttributeValueMemberS).Value)
	assert.Equal(t, rotatedAt.UTC().Format(time.RFC3339), values[":secretsRotatedAt"].(*types.AttributeValueMemberS).Value)
	assert.True(t, values[":hasVerifiedWithDoubleSignature"].(*types.AttributeValueMemberBOOL).Value)
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_GetShop_SecretsRotatedAtRFC3339(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	rotatedAt := time.Unix(123, 0).UTC()
	mockClient.On("GetItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.GetItemOutput{
		Item: map[string]types.AttributeValue{
			"id":               &types.AttributeValueMemberS{Value: "test"},
			"url":              &types.AttributeValueMemberS{Value: "https://test.com"},
			"secret":           &types.AttributeValueMemberS{Value: "secret"},
			"clientId":         &types.AttributeValueMemberS{Value: "key"},
			"clientSecret":     &types.AttributeValueMemberS{Value: "secret"},
			"active":           &types.AttributeValueMemberBOOL{Value: true},
			"secretsRotatedAt": &types.AttributeValueMemberS{Value: rotatedAt.Format(time.RFC3339)},
		},
	}, nil)

	result, err := dynStore.GetShopByID(context.Background(), "test")

	require.NoError(t, err)
	require.NotNil(t, result.SecretsRotatedAt)
	assert.Equal(t, int64(123), result.SecretsRotatedAt.Unix())
	mockClient.AssertExpectations(t)
}

func TestDynamoDBStore_GetShop_InvalidSecretsRotatedAt(t *testing.T) {
	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("GetItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.GetItemOutput{
		Item: map[string]types.AttributeValue{
			"id":               &types.AttributeValueMemberS{Value: "test"},
			"url":              &types.AttributeValueMemberS{Value: "https://test.com"},
			"secret":           &types.AttributeValueMemberS{Value: "secret"},
			"clientId":         &types.AttributeValueMemberS{Value: "key"},
			"clientSecret":     &types.AttributeValueMemberS{Value: "secret"},
			"active":           &types.AttributeValueMemberBOOL{Value: true},
			"created_at":       &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
			"updated_at":       &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
			"secretsRotatedAt": &types.AttributeValueMemberS{Value: "not-a-timestamp"},
		},
	}, nil)

	result, err := dynStore.GetShopByID(context.Background(), "test")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse secretsRotatedAt")
	assert.Nil(t, result)
}
