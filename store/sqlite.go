package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	// SQLite driver (pure-Go, CGo-free)
	_ "modernc.org/sqlite"
)

// SQLiteConfig holds configuration for SQLite store
type SQLiteConfig struct {
	// DatabasePath is the path to the SQLite database file
	// Default: "./data/shops.db"
	DatabasePath string
}

type SQLDB interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	PingContext(ctx context.Context) error
	Close() error
}

// SQLiteStore is a ShopStore backed by a SQLite database. Call Close when done.
type SQLiteStore struct {
	db SQLDB
}

var _ ShopStore = (*SQLiteStore)(nil)

// NewSQLiteStore opens (or creates) the SQLite database at config.DatabasePath,
// defaulting to ./data/shops.db, and creates the schema if needed.
func NewSQLiteStore(config SQLiteConfig) (*SQLiteStore, error) {
	if config.DatabasePath == "" {
		config.DatabasePath = "./data/shops.db"
	}

	// Create directory if it doesn't exist
	dbDir := filepath.Dir(config.DatabasePath)
	if err := createDirIfNotExists(dbDir); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Pragmas ride on the DSN so they apply to every pooled connection:
	// busy_timeout avoids immediate SQLITE_BUSY, WAL improves concurrency.
	sep := "?"
	if strings.Contains(config.DatabasePath, "?") {
		sep = "&" // path already carries a query
	}
	dsn := config.DatabasePath + sep + "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	store := &SQLiteStore{
		db: db,
	}

	if tableErr := store.createTable(); tableErr != nil {
		return nil, fmt.Errorf("failed to create table: %w", tableErr)
	}

	return store, nil
}

func NewSQLiteStoreWithDB(db SQLDB) (*SQLiteStore, error) {
	store := &SQLiteStore{
		db: db,
	}

	if err := store.createTable(); err != nil {
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	return store, nil
}

func (s *SQLiteStore) createTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS shops (
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
		hasVerifiedWithDoubleSignature INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL DEFAULT (unixepoch('now')),
		updated_at INTEGER NOT NULL DEFAULT (unixepoch('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_shops_url ON shops(url);
	CREATE INDEX IF NOT EXISTS idx_shops_active ON shops(active);
	`

	_, err := s.db.ExecContext(context.Background(), query)
	if err != nil {
		return err
	}

	// Migrate: add created_at and updated_at columns if they don't exist (for tables created by other SDKs)
	s.migrateAddTimestampColumns()
	s.migrateAddRegistrationColumns()

	return nil
}

// migrateAddTimestampColumns adds created_at and updated_at columns if they don't exist.
// This ensures compatibility with tables created by other SDKs that don't include these columns.
func (s *SQLiteStore) migrateAddTimestampColumns() {
	ctx := context.Background()
	now := time.Now().Unix()

	// SQLite doesn't support IF NOT EXISTS for ALTER TABLE ADD COLUMN,
	// so we try to add the columns and ignore errors (which occur if columns already exist)
	// Note: SQLite's ALTER TABLE ADD COLUMN has restrictions on NOT NULL with expression defaults,
	// so we add columns with a literal default value instead.

	// Try to add created_at column (will fail silently if it already exists)
	_, _ = s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE shops ADD COLUMN created_at INTEGER NOT NULL DEFAULT %d", now))

	// Try to add updated_at column (will fail silently if it already exists)
	_, _ = s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE shops ADD COLUMN updated_at INTEGER NOT NULL DEFAULT %d", now))

	// Ensure any existing NULL values (from previous migrations without NOT NULL) are set to current timestamp
	_, _ = s.db.ExecContext(ctx, "UPDATE shops SET created_at = unixepoch('now') WHERE created_at IS NULL")
	_, _ = s.db.ExecContext(ctx, "UPDATE shops SET updated_at = unixepoch('now') WHERE updated_at IS NULL")
}

// migrateAddRegistrationColumns adds registration-confirmation and secret-rotation columns if they don't exist.
func (s *SQLiteStore) migrateAddRegistrationColumns() {
	ctx := context.Background()

	_, _ = s.db.ExecContext(ctx, "ALTER TABLE shops ADD COLUMN pendingShopSecret TEXT NOT NULL DEFAULT ''")
	_, _ = s.db.ExecContext(ctx, "ALTER TABLE shops ADD COLUMN pendingShopUrl TEXT NOT NULL DEFAULT ''")
	_, _ = s.db.ExecContext(ctx, "ALTER TABLE shops ADD COLUMN previousShopSecret TEXT NOT NULL DEFAULT ''")
	_, _ = s.db.ExecContext(ctx, "ALTER TABLE shops ADD COLUMN secretsRotatedAt INTEGER")
	_, _ = s.db.ExecContext(ctx, "ALTER TABLE shops ADD COLUMN hasVerifiedWithDoubleSignature INTEGER NOT NULL DEFAULT 0")
	_, _ = s.db.ExecContext(ctx, "ALTER TABLE shops ADD COLUMN confirmed BOOLEAN DEFAULT 0")
}

// GetShopByID retrieves a shop by ID.
func (s *SQLiteStore) GetShopByID(ctx context.Context, shopID string) (*Shop, error) {
	query := `
		SELECT id, url, secret, clientId, clientSecret, active,
			confirmed, pendingShopSecret, pendingShopUrl, previousShopSecret, secretsRotatedAt,
			hasVerifiedWithDoubleSignature,
			created_at, updated_at
		FROM shops
		WHERE id = ?
	`

	var shop Shop
	var createdAt, updatedAt int64
	var secretsRotatedAt sql.NullInt64

	err := s.db.QueryRowContext(ctx, query, shopID).Scan(
		&shop.ShopID,
		&shop.ShopURL,
		&shop.ShopSecret,
		&shop.ClientID,
		&shop.ClientSecret,
		&shop.Active,
		&shop.RegistrationConfirmed,
		&shop.PendingShopSecret,
		&shop.PendingShopURL,
		&shop.PreviousShopSecret,
		&secretsRotatedAt,
		&shop.HasVerifiedWithDoubleSignature,
		&createdAt,
		&updatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrShopNotFound, shopID)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get shop: %w", err)
	}

	shop.CreatedAt = time.Unix(createdAt, 0)
	shop.UpdatedAt = time.Unix(updatedAt, 0)
	if secretsRotatedAt.Valid {
		t := time.Unix(secretsRotatedAt.Int64, 0)
		shop.SecretsRotatedAt = &t
	}

	return &shop, nil
}

// UpdateShop saves or updates a shop.
func (s *SQLiteStore) UpdateShop(ctx context.Context, shop *Shop) error {
	if shop.CreatedAt.IsZero() {
		shop.CreatedAt = time.Now()
	}
	shop.UpdatedAt = time.Now()

	var secretsRotatedAt interface{}
	if shop.SecretsRotatedAt != nil {
		secretsRotatedAt = shop.SecretsRotatedAt.Unix()
	}

	query := `
		INSERT INTO shops (id, url, secret, clientId, clientSecret, active, confirmed,
			pendingShopSecret, pendingShopUrl, previousShopSecret, secretsRotatedAt,
			hasVerifiedWithDoubleSignature,
			created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			url = excluded.url,
			secret = excluded.secret,
			clientId = excluded.clientId,
			clientSecret = excluded.clientSecret,
			active = excluded.active,
			confirmed = excluded.confirmed,
			pendingShopSecret = excluded.pendingShopSecret,
			pendingShopUrl = excluded.pendingShopUrl,
			previousShopSecret = excluded.previousShopSecret,
			secretsRotatedAt = excluded.secretsRotatedAt,
			hasVerifiedWithDoubleSignature = excluded.hasVerifiedWithDoubleSignature,
			updated_at = excluded.updated_at
	`

	_, err := s.db.ExecContext(
		ctx,
		query,
		shop.ShopID,
		shop.ShopURL,
		shop.ShopSecret,
		shop.ClientID,
		shop.ClientSecret,
		shop.Active,
		shop.RegistrationConfirmed,
		shop.PendingShopSecret,
		shop.PendingShopURL,
		shop.PreviousShopSecret,
		secretsRotatedAt,
		shop.HasVerifiedWithDoubleSignature,
		shop.CreatedAt.Unix(),
		shop.UpdatedAt.Unix(),
	)

	if err != nil {
		return fmt.Errorf("failed to store shop: %w", err)
	}

	return nil
}

// SetShopActive updates a shop's active state.
func (s *SQLiteStore) SetShopActive(ctx context.Context, shopID string, active bool) error {
	query := "UPDATE shops SET active = ?, updated_at = ? WHERE id = ?"

	result, err := s.db.ExecContext(ctx, query, active, time.Now().Unix(), shopID)
	if err != nil {
		return fmt.Errorf("failed to update shop active state: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("%w: %s", ErrShopNotFound, shopID)
	}

	return nil
}

// DeleteShop removes a shop by ID.
func (s *SQLiteStore) DeleteShop(ctx context.Context, shopID string) error {
	query := "DELETE FROM shops WHERE id = ?"

	result, err := s.db.ExecContext(ctx, query, shopID)
	if err != nil {
		return fmt.Errorf("failed to delete shop: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("%w: %s", ErrShopNotFound, shopID)
	}

	return nil
}

// HealthCheck verifies the SQLite connection is working.
func (s *SQLiteStore) HealthCheck(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// createDirIfNotExists creates a directory if it doesn't exist.
func createDirIfNotExists(dirPath string) error {
	if dirPath == "" || dirPath == "." {
		return nil
	}

	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
		if mkdirErr := os.MkdirAll(absPath, 0750); mkdirErr != nil {
			return fmt.Errorf("failed to create directory: %w", mkdirErr)
		}
	}

	return nil
}
