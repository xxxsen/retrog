package db

import (
	"context"
	"fmt"
	"time"

	"github.com/didi/gendry/builder"
	"github.com/xxxsen/common/database"
)

const hashCacheTableName = "file_hash_cache_tab"

// FileHashCacheDAO provides helpers to read and write file hash cache entries.
type FileHashCacheDAO struct {
	db database.IDatabase
}

// NewFileHashCacheDAO builds a cache DAO using the default database.
func NewFileHashCacheDAO() *FileHashCacheDAO {
	return &FileHashCacheDAO{db: Default()}
}

// Lookup returns a cached hash for the location when the file modification time matches.
func (dao *FileHashCacheDAO) Lookup(ctx context.Context, location string, modTime int64) (string, bool, error) {
	if dao.db == nil {
		return "", false, nil
	}

	const query = `SELECT hash, file_modtime FROM file_hash_cache_tab WHERE location = ? LIMIT 1`
	rows, err := dao.db.QueryContext(ctx, query, location)
	if err != nil {
		return "", false, fmt.Errorf("query hash cache: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		var hash string
		var cachedModTime int64
		if err := rows.Scan(&hash, &cachedModTime); err != nil {
			return "", false, fmt.Errorf("scan hash cache: %w", err)
		}
		if cachedModTime == modTime {
			return hash, true, nil
		}
		return "", false, nil
	}
	if err := rows.Err(); err != nil {
		return "", false, err
	}
	return "", false, nil
}

// Upsert stores or updates the cached hash for the provided location.
func (dao *FileHashCacheDAO) Upsert(ctx context.Context, location string, modTime int64, hash string) error {
	if dao.db == nil {
		return fmt.Errorf("hash cache dao not initialised")
	}

	now := time.Now().Unix()
	payload := []map[string]interface{}{{
		"location":     location,
		"create_time":  now,
		"file_modtime": modTime,
		"hash":         hash,
	}}
	insertSQL, insertArgs, err := builder.BuildInsert(hashCacheTableName, payload)
	if err != nil {
		return err
	}
	if _, err := dao.db.ExecContext(ctx, insertSQL, insertArgs...); err != nil {
		if !isUniqueConstraintError(err) {
			return fmt.Errorf("insert hash cache: %w", err)
		}
		updateSQL, updateArgs, err := builder.BuildUpdate(hashCacheTableName,
			map[string]interface{}{"location": location},
			map[string]interface{}{
				"file_modtime": modTime,
				"hash":         hash,
			},
		)
		if err != nil {
			return err
		}
		if _, err := dao.db.ExecContext(ctx, updateSQL, updateArgs...); err != nil {
			return fmt.Errorf("update hash cache: %w", err)
		}
	}
	return nil
}
