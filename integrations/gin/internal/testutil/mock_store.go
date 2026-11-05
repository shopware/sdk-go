package testutil

import (
	"context"

	"github.com/shopware/app-sdk-go/pkg/store"
	"github.com/stretchr/testify/mock"
)

type MockShopStore struct {
	mock.Mock
}

// NewFailingStore returns a MockShopStore where every method returns err.
func NewFailingStore(err error) *MockShopStore {
	m := new(MockShopStore)
	m.On("GetShopByID", mock.Anything, mock.Anything).Return(nil, err).Maybe()
	m.On("UpdateShop", mock.Anything, mock.Anything).Return(err).Maybe()
	m.On("SetShopActive", mock.Anything, mock.Anything, mock.Anything).Return(err).Maybe()
	m.On("DeleteShop", mock.Anything, mock.Anything).Return(err).Maybe()
	m.On("HealthCheck", mock.Anything).Return(err).Maybe()
	return m
}

var _ store.ShopStore = (*MockShopStore)(nil)

func (m *MockShopStore) GetShopByID(ctx context.Context, shopID string) (*store.Shop, error) {
	args := m.Called(ctx, shopID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	shop, _ := args.Get(0).(*store.Shop)
	return shop, args.Error(1)
}

func (m *MockShopStore) UpdateShop(ctx context.Context, shop *store.Shop) error {
	args := m.Called(ctx, shop)
	return args.Error(0)
}

func (m *MockShopStore) SetShopActive(ctx context.Context, shopID string, active bool) error {
	args := m.Called(ctx, shopID, active)
	return args.Error(0)
}

func (m *MockShopStore) DeleteShop(ctx context.Context, shopID string) error {
	args := m.Called(ctx, shopID)
	return args.Error(0)
}

func (m *MockShopStore) HealthCheck(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}
