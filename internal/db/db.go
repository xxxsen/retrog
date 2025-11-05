package db

import (
	"context"

	"github.com/xxxsen/common/database"
)

var defaultDB database.IDatabase

const (
	createMetaTableSQL = `
CREATE TABLE IF NOT EXISTS retro_game_meta_tab (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	rom_hash VARCHAR(32) NOT NULL UNIQUE,
	rom_name VARCHAR(128) NOT NULL,
	rom_desc VARCHAR(1024) NOT NULL,
	rom_size INTEGER NOT NULL,
	create_time BIGINT NOT NULL,
	update_time BIGINT NOT NULL,
	ext_info VARCHAR(2048) NOT NULL
);`
	createHashCacheTableSQL = `
CREATE TABLE IF NOT EXISTS file_hash_cache_tab (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	location VARCHAR(1024) NOT NULL UNIQUE,
	create_time BIGINT NOT NULL,
	file_modtime BIGINT NOT NULL,
	hash VARCHAR(32) NOT NULL
);`
)

// SetDefault assigns the global database instance.
func SetDefault(db database.IDatabase) {
	defaultDB = db
}

// Default returns the configured global database instance.
func Default() database.IDatabase {
	return defaultDB
}

// EnsureSchema initialises required tables and indexes.
func EnsureSchema(ctx context.Context, db database.IDatabase) error {
	stmts := []string{
		createMetaTableSQL,
		createHashCacheTableSQL,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
