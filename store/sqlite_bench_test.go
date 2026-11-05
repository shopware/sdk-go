package store_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/shopware/sdk-go/internal/testutil"
	"github.com/shopware/sdk-go/store"
	"github.com/stretchr/testify/require"
)

func BenchmarkSQLiteStore_UpdateShop(b *testing.B) {
	b.ReportAllocs()

	tmpFile, err := os.CreateTemp(b.TempDir(), "bench-store-*.db")
	require.NoError(b, err)
	dbPath := tmpFile.Name()
	tmpFile.Close()

	ctx := context.Background()
	config := store.SQLiteConfig{
		DatabasePath: dbPath,
	}

	sqliteStore, err := store.NewSQLiteStore(config)
	require.NoError(b, err)
	defer sqliteStore.Close()

	shop := testutil.NewTestShop(testutil.WithShopID("benchmark-shop"))

	b.ResetTimer()
	for range b.N {
		_ = sqliteStore.UpdateShop(ctx, shop)
	}
}

func BenchmarkSQLiteStore_GetShopByID(b *testing.B) {
	b.ReportAllocs()

	tmpFile, err := os.CreateTemp(b.TempDir(), "bench-get-*.db")
	require.NoError(b, err)
	dbPath := tmpFile.Name()
	tmpFile.Close()

	ctx := context.Background()
	config := store.SQLiteConfig{
		DatabasePath: dbPath,
	}

	sqliteStore, err := store.NewSQLiteStore(config)
	require.NoError(b, err)
	defer sqliteStore.Close()

	shop := testutil.NewTestShop(testutil.WithShopID("benchmark-shop"))
	_ = sqliteStore.UpdateShop(ctx, shop)

	b.ResetTimer()
	for range b.N {
		_, _ = sqliteStore.GetShopByID(ctx, shop.ShopID)
	}
}

func BenchmarkSQLiteStore_SetShopActive(b *testing.B) {
	b.ReportAllocs()

	tmpFile, err := os.CreateTemp(b.TempDir(), "bench-update-*.db")
	require.NoError(b, err)
	dbPath := tmpFile.Name()
	tmpFile.Close()

	ctx := context.Background()
	config := store.SQLiteConfig{
		DatabasePath: dbPath,
	}

	sqliteStore, err := store.NewSQLiteStore(config)
	require.NoError(b, err)
	defer sqliteStore.Close()

	shop := testutil.NewTestShop(testutil.WithShopID("benchmark-shop"))
	_ = sqliteStore.UpdateShop(ctx, shop)

	b.ResetTimer()
	for i := range b.N {
		active := i%2 == 0
		_ = sqliteStore.SetShopActive(ctx, shop.ShopID, active)
	}
}

func BenchmarkSQLiteStore_DeleteShop(b *testing.B) {
	b.ReportAllocs()

	tmpFile, err := os.CreateTemp(b.TempDir(), "bench-delete-*.db")
	require.NoError(b, err)
	dbPath := tmpFile.Name()
	tmpFile.Close()

	ctx := context.Background()
	config := store.SQLiteConfig{
		DatabasePath: dbPath,
	}

	sqliteStore, err := store.NewSQLiteStore(config)
	require.NoError(b, err)
	defer sqliteStore.Close()

	b.ResetTimer()
	for i := range b.N {
		b.StopTimer()
		shopID := fmt.Sprintf("benchmark-shop-%d", i)
		shop := testutil.NewTestShop(testutil.WithShopID(shopID))
		_ = sqliteStore.UpdateShop(ctx, shop)
		b.StartTimer()

		_ = sqliteStore.DeleteShop(ctx, shopID)
	}
}
