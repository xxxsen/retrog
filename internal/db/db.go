package db

import (
	"context"

	"github.com/xxxsen/common/database"
)

var defaultDB database.IDatabase

const (
	createTableSQL = `
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
	_, err := db.ExecContext(ctx, createTableSQL)
	return err
}
