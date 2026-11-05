package store_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shopware/sdk-go/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// SQLiteStoreTestSuite runs the shared test suite for SQLiteStore
type SQLiteStoreTestSuite struct {
	StoreTestSuite

	dbPath      string
	sqliteStore *store.SQLiteStore
}

// SetupTest creates a new SQLiteStore for each test
func (suite *SQLiteStoreTestSuite) SetupTest() {
	// Create temporary database file
	tmpFile, err := os.CreateTemp(suite.T().TempDir(), "test-shops-*.db")
	suite.Require().NoError(err)
	suite.dbPath = tmpFile.Name()
	tmpFile.Close()

	// Create SQLite store
	config := store.SQLiteConfig{
		DatabasePath: suite.dbPath,
	}

	sqliteStore, err := store.NewSQLiteStore(config)
	suite.NoError(err)
	suite.sqliteStore = sqliteStore
	suite.store = sqliteStore
}

// TearDownTest cleans up the temporary database
func (suite *SQLiteStoreTestSuite) TearDownTest() {
	if suite.sqliteStore != nil {
		suite.sqliteStore.Close()
	}
	if suite.dbPath != "" {
		os.Remove(suite.dbPath)
	}
}

// TestSQLiteStoreTestSuite runs the test suite
func TestSQLiteStoreTestSuite(t *testing.T) {
	suite.Run(t, new(SQLiteStoreTestSuite))
}

func newSQLiteStoreWithCleanup(t *testing.T, config store.SQLiteConfig) *store.SQLiteStore {
	t.Helper()
	sqliteStore, err := store.NewSQLiteStore(config)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqliteStore.Close())
	})
	return sqliteStore
}

// TestSQLiteStore_FileCreation tests that database file is created
func TestSQLiteStore_FileCreation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := store.SQLiteConfig{
		DatabasePath: dbPath,
	}

	newSQLiteStoreWithCleanup(t, config)

	// Verify file exists
	_, err := os.Stat(dbPath)
	require.NoError(t, err)
}

// TestSQLiteStore_DirectoryCreation tests that parent directory is created
func TestSQLiteStore_DirectoryCreation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "subdir", "nested", "test.db")

	config := store.SQLiteConfig{
		DatabasePath: dbPath,
	}

	newSQLiteStoreWithCleanup(t, config)

	// Verify nested directories exist
	_, err := os.Stat(filepath.Dir(dbPath))
	require.NoError(t, err)

	// Verify database file exists
	_, err = os.Stat(dbPath)
	require.NoError(t, err)
}

// TestSQLiteStore_DefaultPath tests default database path
func TestSQLiteStore_DefaultPath(t *testing.T) {
	// Create temporary directory for default path
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	config := store.SQLiteConfig{
		DatabasePath: "", // Use default
	}

	newSQLiteStoreWithCleanup(t, config)

	// Verify default path was created
	_, err := os.Stat("./data/shops.db")
	require.NoError(t, err)

	// The ./data dir lives inside the t.TempDir() we chdir'd into, so the
	// testing framework removes it automatically.
}

// TestSQLiteStore_DatabasePathWithQuery ensures a DatabasePath that already
// carries a query string yields a valid DSN (params appended with '&', not a
// second '?').
func TestSQLiteStore_DatabasePathWithQuery(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "test-query-*.db")
	require.NoError(t, err)
	dbPath := tmpFile.Name()
	tmpFile.Close()

	config := store.SQLiteConfig{
		DatabasePath: dbPath + "?_pragma=foreign_keys(1)",
	}

	sqliteStore := newSQLiteStoreWithCleanup(t, config)

	ctx := context.Background()
	shop := createTestShop("query-path-shop")

	require.NoError(t, sqliteStore.UpdateShop(ctx, shop))

	retrieved, err := sqliteStore.GetShopByID(ctx, shop.ShopID)
	require.NoError(t, err)
	assert.Equal(t, shop.ShopID, retrieved.ShopID)
}

// TestSQLiteStore_TableCreation tests that tables are created with correct schema
func TestSQLiteStore_TableCreation(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "test-table-*.db")
	require.NoError(t, err)
	dbPath := tmpFile.Name()
	tmpFile.Close()

	config := store.SQLiteConfig{
		DatabasePath: dbPath,
	}

	sqliteStore := newSQLiteStoreWithCleanup(t, config)

	// Verify we can perform operations (table exists)
	ctx := context.Background()
	shop := createTestShop("table-test-shop")

	err = sqliteStore.UpdateShop(ctx, shop)
	require.NoError(t, err)

	// Verify shop was stored
	retrieved, err := sqliteStore.GetShopByID(ctx, shop.ShopID)
	require.NoError(t, err)
	assert.Equal(t, shop.ShopID, retrieved.ShopID)
}

func TestSQLiteStore_TableUsesPHPStylePendingColumns(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "test-schema-*.db")
	require.NoError(t, err)
	dbPath := tmpFile.Name()
	tmpFile.Close()

	sqliteStore, err := store.NewSQLiteStore(store.SQLiteConfig{DatabasePath: dbPath})
	require.NoError(t, err)
	require.NoError(t, sqliteStore.Close())

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()

	rows, err := db.QueryContext(context.Background(), "PRAGMA table_info(shops)")
	require.NoError(t, err)
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)

		require.NoError(t, rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk))
		columns[name] = true
	}
	require.NoError(t, rows.Err())

	assert.True(t, columns["pendingShopSecret"])
	assert.True(t, columns["pendingShopUrl"])
	assert.True(t, columns["previousShopSecret"])
	assert.False(t, columns["pendingSecret"])
	assert.False(t, columns["pendingUrl"])
	assert.False(t, columns["previousSecret"])
}

// TestSQLiteStore_ReopenDatabase tests data persistence across connections
func TestSQLiteStore_ReopenDatabase(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "test-reopen-*.db")
	require.NoError(t, err)
	dbPath := tmpFile.Name()
	tmpFile.Close()

	ctx := context.Background()
	config := store.SQLiteConfig{
		DatabasePath: dbPath,
	}

	// Create first store and add shop
	store1, err := store.NewSQLiteStore(config)
	require.NoError(t, err)

	shop := createTestShop("persistent-shop")
	err = store1.UpdateShop(ctx, shop)
	require.NoError(t, err)

	// Close first store
	err = store1.Close()
	require.NoError(t, err)

	// Reopen database with new store instance
	store2, err := store.NewSQLiteStore(config)
	require.NoError(t, err)
	defer store2.Close()

	// Verify shop still exists
	retrieved, err := store2.GetShopByID(ctx, shop.ShopID)
	require.NoError(t, err)
	assert.Equal(t, shop.ShopID, retrieved.ShopID)
	assert.Equal(t, shop.ShopURL, retrieved.ShopURL)
	assert.Equal(t, shop.ShopSecret, retrieved.ShopSecret)
}

// TestSQLiteStore_UniqueConstraints tests unique constraints on shop_url
func TestSQLiteStore_UniqueConstraints(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "test-unique-*.db")
	require.NoError(t, err)
	dbPath := tmpFile.Name()
	tmpFile.Close()

	ctx := context.Background()
	config := store.SQLiteConfig{
		DatabasePath: dbPath,
	}

	sqliteStore, err := store.NewSQLiteStore(config)
	require.NoError(t, err)
	defer sqliteStore.Close()

	// Store first shop
	shop1 := createTestShop("unique-shop-1")
	err = sqliteStore.UpdateShop(ctx, shop1)
	require.NoError(t, err)

	// Try to store second shop with same URL but different ID
	shop2 := createTestShop("unique-shop-2")
	shop2.ShopURL = shop1.ShopURL // Same URL

	err = sqliteStore.UpdateShop(ctx, shop2)
	// Should fail due to unique constraint on shop_url
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UNIQUE")
}

// TestSQLiteStore_Close tests closing the database
func TestSQLiteStore_Close(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "test-close-*.db")
	require.NoError(t, err)
	dbPath := tmpFile.Name()
	tmpFile.Close()

	config := store.SQLiteConfig{
		DatabasePath: dbPath,
	}

	sqliteStore, err := store.NewSQLiteStore(config)
	require.NoError(t, err)

	// Close should succeed
	err = sqliteStore.Close()
	require.NoError(t, err)

	// Second close should also succeed (or be a no-op)
	err = sqliteStore.Close()
	require.NoError(t, err)
}

// TestSQLiteStore_HealthCheck tests database health check
func TestSQLiteStore_HealthCheck(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "test-health-*.db")
	require.NoError(t, err)
	dbPath := tmpFile.Name()
	tmpFile.Close()

	ctx := context.Background()
	config := store.SQLiteConfig{
		DatabasePath: dbPath,
	}

	sqliteStore, err := store.NewSQLiteStore(config)
	require.NoError(t, err)
	defer sqliteStore.Close()

	// Health check should pass
	err = sqliteStore.HealthCheck(ctx)
	require.NoError(t, err)

	// Health check should pass after operations
	shop := createTestShop("health-shop")
	_ = sqliteStore.UpdateShop(ctx, shop)

	err = sqliteStore.HealthCheck(ctx)
	require.NoError(t, err)
}

// TestSQLiteStore_TimestampPrecision tests timestamp storage and retrieval
func TestSQLiteStore_TimestampPrecision(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "test-timestamp-*.db")
	require.NoError(t, err)
	dbPath := tmpFile.Name()
	tmpFile.Close()

	ctx := context.Background()
	config := store.SQLiteConfig{
		DatabasePath: dbPath,
	}

	sqliteStore, err := store.NewSQLiteStore(config)
	require.NoError(t, err)
	defer sqliteStore.Close()

	shop := createTestShop("timestamp-shop")
	err = sqliteStore.UpdateShop(ctx, shop)
	require.NoError(t, err)

	retrieved, err := sqliteStore.GetShopByID(ctx, shop.ShopID)
	require.NoError(t, err)

	// Timestamps should be preserved (within second precision due to Unix timestamp)
	assert.False(t, retrieved.CreatedAt.IsZero())
	assert.False(t, retrieved.UpdatedAt.IsZero())
	assert.Equal(t, shop.CreatedAt.Unix(), retrieved.CreatedAt.Unix())
	assert.Equal(t, shop.UpdatedAt.Unix(), retrieved.UpdatedAt.Unix())
}

// =============================================================================
// SQL Mock Tests - Testing uncovered error paths with sqlmock for 100% coverage
// =============================================================================

func TestSQLiteStore_StoreShop_ExecError(t *testing.T) {
	// Use QueryMatcherOption to allow regex matching for SQL queries
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer db.Close()

	// Mock successful table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS shops").WillReturnResult(sqlmock.NewResult(0, 0))

	sqliteStore, err := store.NewSQLiteStoreWithDB(db)
	require.NoError(t, err, "NewSQLiteStoreWithDB should succeed")

	shop := createTestShop("test-shop")

	mock.ExpectExec("INSERT INTO shops").
		WithArgs(shop.ShopID, shop.ShopURL, shop.ShopSecret, shop.ClientID, shop.ClientSecret, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(errors.New("database error"))

	err = sqliteStore.UpdateShop(context.Background(), shop)
	require.Error(t, err, "StoreShop should return error")
	assert.Contains(t, err.Error(), "failed to store shop")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err, "All mock expectations should be met")
}

func TestSQLiteStore_SetShopActive_ExecError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Mock successful table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS shops").WillReturnResult(sqlmock.NewResult(0, 0))

	sqliteStore, err := store.NewSQLiteStoreWithDB(db)
	require.NoError(t, err)

	// Mock ExecContext to return an error
	// Arguments: active (bool), updated_at, shopID
	mock.ExpectExec("UPDATE shops SET active").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(errors.New("database error"))

	err = sqliteStore.SetShopActive(context.Background(), "test-shop", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update shop active state")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSQLiteStore_SetShopActive_NoRowsAffected(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Mock successful table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS shops").WillReturnResult(sqlmock.NewResult(0, 0))

	sqliteStore, err := store.NewSQLiteStoreWithDB(db)
	require.NoError(t, err)

	// Mock successful exec but no rows affected
	// Arguments: active (bool), updated_at, shopID
	mock.ExpectExec("UPDATE shops SET active").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = sqliteStore.SetShopActive(context.Background(), "nonexistent", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shop not found")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSQLiteStore_SetShopActive_RowsAffectedError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Mock successful table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS shops").WillReturnResult(sqlmock.NewResult(0, 0))

	sqliteStore, err := store.NewSQLiteStoreWithDB(db)
	require.NoError(t, err)

	// Mock result that returns error on RowsAffected()
	// Arguments: active (bool), updated_at, shopID
	mock.ExpectExec("UPDATE shops SET active").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewErrorResult(errors.New("rows affected error")))

	err = sqliteStore.SetShopActive(context.Background(), "test-shop", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get affected rows")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSQLiteStore_DeleteShop_ExecError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Mock successful table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS shops").WillReturnResult(sqlmock.NewResult(0, 0))

	sqliteStore, err := store.NewSQLiteStoreWithDB(db)
	require.NoError(t, err)

	// Mock ExecContext to return an error
	mock.ExpectExec("DELETE FROM shops WHERE id").
		WithArgs(sqlmock.AnyArg()).
		WillReturnError(errors.New("database error"))

	err = sqliteStore.DeleteShop(context.Background(), "test-shop")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete shop")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSQLiteStore_DeleteShop_RowsAffectedError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Mock successful table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS shops").WillReturnResult(sqlmock.NewResult(0, 0))

	sqliteStore, err := store.NewSQLiteStoreWithDB(db)
	require.NoError(t, err)

	// Mock result that returns error on RowsAffected()
	mock.ExpectExec("DELETE FROM shops WHERE id").
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewErrorResult(errors.New("rows affected error")))

	err = sqliteStore.DeleteShop(context.Background(), "test-shop")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get affected rows")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSQLiteStore_Close_Error(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	// Mock successful table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS shops").WillReturnResult(sqlmock.NewResult(0, 0))

	sqliteStore, err := store.NewSQLiteStoreWithDB(db)
	require.NoError(t, err)

	// Mock Close to return an error
	mock.ExpectClose().WillReturnError(errors.New("close error"))

	err = sqliteStore.Close()
	require.Error(t, err)
	// The actual error message is just "close error" without the wrapper
	assert.Contains(t, err.Error(), "close error")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSQLiteStore_NewSQLiteStoreWithDB_CreateTableError(t *testing.T) {
	// Test that NewSQLiteStoreWithDB returns error if createTable fails
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Mock table creation to return an error
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS shops").WillReturnError(errors.New("table creation error"))

	_, err = store.NewSQLiteStoreWithDB(db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create table")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSQLiteStore_Close_NilDB(t *testing.T) {
	// This test is not directly achievable since we can't create SQLiteStore with nil db
	// from outside the package. The nil check in Close() is defensive programming.
	// It's covered indirectly - when Close is called after Close, db might be in an invalid state.
	t.Skip("Cannot test nil db from outside package - defensive code path")
}

func TestSQLiteStore_GetShop_ScanError(t *testing.T) {
	// Test GetShop when QueryRowContext succeeds but Scan fails
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Mock successful table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS shops").WillReturnResult(sqlmock.NewResult(0, 0))

	sqliteStore, err := store.NewSQLiteStoreWithDB(db)
	require.NoError(t, err)

	rows := sqlmock.NewRows([]string{"id", "url", "secret", "clientId", "clientSecret", "active", "confirmed", "pendingShopSecret", "pendingShopUrl", "previousShopSecret", "secretsRotatedAt", "hasVerifiedWithDoubleSignature", "created_at", "updated_at"}).
		AddRow("test", "https://test.com", "secret", "key", "key", true, true, "", "", "", nil, false, "invalid", "invalid")

	mock.ExpectQuery("SELECT (.+) FROM shops WHERE id").
		WithArgs("test-shop").
		WillReturnRows(rows)

	_, err = sqliteStore.GetShopByID(context.Background(), "test-shop")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get shop")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSQLiteStore_StoreShop_ZeroCreatedAt(t *testing.T) {
	// Test StoreShop when CreatedAt is zero - should set it automatically
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer db.Close()

	// Mock successful table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS shops").WillReturnResult(sqlmock.NewResult(0, 0))

	sqliteStore, err := store.NewSQLiteStoreWithDB(db)
	require.NoError(t, err)

	shop := createTestShop("test-shop")
	shop.CreatedAt = time.Time{} // Zero time

	mock.ExpectExec("INSERT INTO shops").
		WithArgs(shop.ShopID, shop.ShopURL, shop.ShopSecret, shop.ClientID, shop.ClientSecret, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = sqliteStore.UpdateShop(context.Background(), shop)
	require.NoError(t, err)
	// Verify CreatedAt was set
	assert.False(t, shop.CreatedAt.IsZero())

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSQLiteStore_MigratesMissingTimestampColumns(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "test-legacy-timestamps-*.db")
	require.NoError(t, err)
	dbPath := tmpFile.Name()
	tmpFile.Close()

	ctx := context.Background()

	// Create a table without timestamp columns (simulating other SDK behavior)
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		CREATE TABLE shops (
			id TEXT PRIMARY KEY,
			url TEXT NOT NULL UNIQUE,
			secret TEXT NOT NULL,
			clientId TEXT NOT NULL,
			clientSecret TEXT NOT NULL,
			active INTEGER NOT NULL DEFAULT 0,
			confirmed BOOLEAN DEFAULT 0,
			pendingShopSecret TEXT NOT NULL DEFAULT '',
			pendingShopUrl TEXT NOT NULL DEFAULT '',
			previousShopSecret TEXT NOT NULL DEFAULT '',
			secretsRotatedAt INTEGER,
			hasVerifiedWithDoubleSignature INTEGER NOT NULL DEFAULT 0
		)
	`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO shops (id, url, secret, clientId, clientSecret, active, confirmed)
		VALUES ('legacy-shop', 'https://legacy.shop', 'secret123', 'client-id', 'client-secret', 1, 1)
	`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	sqliteStore, err := store.NewSQLiteStore(store.SQLiteConfig{DatabasePath: dbPath})
	require.NoError(t, err)
	defer sqliteStore.Close()

	shop, err := sqliteStore.GetShopByID(ctx, "legacy-shop")
	require.NoError(t, err)

	assert.Equal(t, "legacy-shop", shop.ShopID)
	assert.False(t, shop.CreatedAt.IsZero())
	assert.False(t, shop.UpdatedAt.IsZero())
}

// TestSQLiteStore_GetShop_WithTimestamps tests that GetShopByID retrieves timestamps correctly.
func TestSQLiteStore_GetShop_WithTimestamps(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Mock successful table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS shops").WillReturnResult(sqlmock.NewResult(0, 0))

	sqliteStore, err := store.NewSQLiteStoreWithDB(db)
	require.NoError(t, err)

	now := time.Now().Unix()
	rows := sqlmock.NewRows([]string{"id", "url", "secret", "clientId", "clientSecret", "active", "confirmed", "pendingShopSecret", "pendingShopUrl", "previousShopSecret", "secretsRotatedAt", "hasVerifiedWithDoubleSignature", "created_at", "updated_at"}).
		AddRow("test-shop", "https://test.com", "secret", "client-id", "client-secret", true, true, "", "", "", nil, false, now, now)

	mock.ExpectQuery("SELECT (.+) FROM shops WHERE id").
		WithArgs("test-shop").
		WillReturnRows(rows)

	shop, err := sqliteStore.GetShopByID(context.Background(), "test-shop")
	require.NoError(t, err)

	assert.Equal(t, "test-shop", shop.ShopID)
	assert.Equal(t, "https://test.com", shop.ShopURL)
	assert.True(t, shop.Active)

	assert.False(t, shop.CreatedAt.IsZero())
	assert.False(t, shop.UpdatedAt.IsZero())
	assert.Equal(t, now, shop.CreatedAt.Unix())
	assert.Equal(t, now, shop.UpdatedAt.Unix())

	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// Legacy Active Field Tests
// =============================================================================

func TestSQLiteStore_GetShop_ActiveTrue(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS shops").WillReturnResult(sqlmock.NewResult(0, 0))

	sqliteStore, err := store.NewSQLiteStoreWithDB(db)
	require.NoError(t, err)

	now := time.Now().Unix()
	rows := sqlmock.NewRows([]string{"id", "url", "secret", "clientId", "clientSecret", "active", "confirmed", "pendingShopSecret", "pendingShopUrl", "previousShopSecret", "secretsRotatedAt", "hasVerifiedWithDoubleSignature", "created_at", "updated_at"}).
		AddRow("active-shop", "https://active.shop", "secret", "client-id", "client-secret", true, true, "", "", "", nil, false, now, now)

	mock.ExpectQuery("SELECT (.+) FROM shops WHERE id").
		WithArgs("active-shop").
		WillReturnRows(rows)

	shop, err := sqliteStore.GetShopByID(context.Background(), "active-shop")
	require.NoError(t, err)

	assert.Equal(t, "active-shop", shop.ShopID)
	assert.True(t, shop.Active)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSQLiteStore_GetShop_ActiveFalse(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS shops").WillReturnResult(sqlmock.NewResult(0, 0))

	sqliteStore, err := store.NewSQLiteStoreWithDB(db)
	require.NoError(t, err)

	now := time.Now().Unix()
	rows := sqlmock.NewRows([]string{"id", "url", "secret", "clientId", "clientSecret", "active", "confirmed", "pendingShopSecret", "pendingShopUrl", "previousShopSecret", "secretsRotatedAt", "hasVerifiedWithDoubleSignature", "created_at", "updated_at"}).
		AddRow("inactive-shop", "https://inactive.shop", "secret", "client-id", "client-secret", false, true, "", "", "", nil, false, now, now)

	mock.ExpectQuery("SELECT (.+) FROM shops WHERE id").
		WithArgs("inactive-shop").
		WillReturnRows(rows)

	shop, err := sqliteStore.GetShopByID(context.Background(), "inactive-shop")
	require.NoError(t, err)

	assert.Equal(t, "inactive-shop", shop.ShopID)
	assert.False(t, shop.Active)

	assert.NoError(t, mock.ExpectationsWereMet())
}
