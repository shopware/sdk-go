package store_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/shopware/app-sdk-go/pkg/internal/testutil"
	"github.com/shopware/app-sdk-go/pkg/store"
)

func BenchmarkInMemoryStore_UpdateShop(b *testing.B) {
	b.ReportAllocs()

	ctx := context.Background()
	inMemStore := store.NewInMemoryStore()
	shop := testutil.NewTestShop(testutil.WithShopID("benchmark-shop"))

	b.ResetTimer()
	for range b.N {
		_ = inMemStore.UpdateShop(ctx, shop)
	}
}

func BenchmarkInMemoryStore_GetShopByID(b *testing.B) {
	b.ReportAllocs()

	ctx := context.Background()
	inMemStore := store.NewInMemoryStore()
	shop := testutil.NewTestShop(testutil.WithShopID("benchmark-shop"))
	_ = inMemStore.UpdateShop(ctx, shop)

	b.ResetTimer()
	for range b.N {
		_, _ = inMemStore.GetShopByID(ctx, shop.ShopID)
	}
}

func BenchmarkInMemoryStore_SetShopActive(b *testing.B) {
	b.ReportAllocs()

	ctx := context.Background()
	inMemStore := store.NewInMemoryStore()
	shop := testutil.NewTestShop(testutil.WithShopID("benchmark-shop"))
	_ = inMemStore.UpdateShop(ctx, shop)

	b.ResetTimer()
	for i := range b.N {
		active := i%2 == 0
		_ = inMemStore.SetShopActive(ctx, shop.ShopID, active)
	}
}

func BenchmarkInMemoryStore_DeleteShop(b *testing.B) {
	b.ReportAllocs()

	ctx := context.Background()
	inMemStore := store.NewInMemoryStore()

	b.ResetTimer()
	for i := range b.N {
		b.StopTimer()
		shopID := fmt.Sprintf("benchmark-shop-%d", i)
		shop := testutil.NewTestShop(testutil.WithShopID(shopID))
		_ = inMemStore.UpdateShop(ctx, shop)
		b.StartTimer()

		_ = inMemStore.DeleteShop(ctx, shopID)
	}
}

func BenchmarkInMemoryStore_ConcurrentOperations(b *testing.B) {
	b.ReportAllocs()

	ctx := context.Background()
	inMemStore := store.NewInMemoryStore()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			shopID := fmt.Sprintf("concurrent-shop-%d", i)
			shop := testutil.NewTestShop(testutil.WithShopID(shopID))

			_ = inMemStore.UpdateShop(ctx, shop)
			_, _ = inMemStore.GetShopByID(ctx, shopID)
			_ = inMemStore.SetShopActive(ctx, shopID, true)
			_ = inMemStore.DeleteShop(ctx, shopID)

			i++
		}
	})
}
