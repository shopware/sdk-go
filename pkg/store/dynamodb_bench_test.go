package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/shopware/app-sdk-go/pkg/store"
	"github.com/stretchr/testify/mock"
)

func BenchmarkDynamoDBStore_GetShopByID(b *testing.B) {
	b.ReportAllocs()

	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	now := time.Now().UTC()
	shop := &store.Shop{
		ShopID:       "bench-shop",
		ShopURL:      "https://bench.com",
		ShopSecret:   "secret",
		ClientID:     "key",
		ClientSecret: "secret",
		Active:       true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	mockClient.On("GetItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.GetItemOutput{
		Item: createDynamoDBItem(shop),
	}, nil)

	b.ResetTimer()
	for range b.N {
		_, _ = dynStore.GetShopByID(context.Background(), "bench-shop")
	}
}

func BenchmarkDynamoDBStore_UpdateShop(b *testing.B) {
	b.ReportAllocs()

	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	shop := &store.Shop{
		ShopID:       "bench-shop",
		ShopURL:      "https://bench.com",
		ShopSecret:   "secret",
		ClientID:     "key",
		ClientSecret: "secret",
		Active:       true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	mockClient.On("UpdateItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.UpdateItemOutput{}, nil)

	b.ResetTimer()
	for range b.N {
		_ = dynStore.UpdateShop(context.Background(), shop)
	}
}

func BenchmarkDynamoDBStore_SetShopActive(b *testing.B) {
	b.ReportAllocs()

	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("UpdateItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.UpdateItemOutput{}, nil)

	b.ResetTimer()
	for range b.N {
		_ = dynStore.SetShopActive(context.Background(), "bench-shop", true)
	}
}

func BenchmarkDynamoDBStore_DeleteShop(b *testing.B) {
	b.ReportAllocs()

	mockClient := new(MockDynamoDBClient)
	config := createTestDynamoDBConfig()
	dynStore := store.NewDynamoDBStoreWithClient(mockClient, config)

	mockClient.On("DeleteItem", mock.Anything, mock.Anything, mock.Anything).Return(&dynamodb.DeleteItemOutput{}, nil)

	b.ResetTimer()
	for range b.N {
		_ = dynStore.DeleteShop(context.Background(), "bench-shop")
	}
}
