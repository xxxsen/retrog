package db

import (
	"context"
	"fmt"
	"time"

	"github.com/didi/gendry/builder"
)

const hashCacheTableName = "file_hash_cache_tab"

var FileHashCacheDao = newFileHashCacheDao()

type fileHashCacheDao struct {
	dbGetter DatabaseGetter
}

type HashCacheEntry struct {
	Location string
}

func newFileHashCacheDao() *fileHashCacheDao {
	return &fileHashCacheDao{
		dbGetter: Default,
	}
}

// Lookup returns a cached hash for the location when the file modification time matches.
func (dao *fileHashCacheDao) Lookup(ctx context.Context, location string, modTime int64) (string, bool, error) {
	db := dao.dbGetter()
	if db == nil {
		return "", false, nil
	}

	const query = `SELECT hash, file_modtime FROM file_hash_cache_tab WHERE location = ? LIMIT 1`
	rows, err := db.QueryContext(ctx, query, location)
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
func (dao *fileHashCacheDao) Upsert(ctx context.Context, location string, modTime int64, hash string) error {
	db := dao.dbGetter()
	if db == nil {
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
	if _, err := db.ExecContext(ctx, insertSQL, insertArgs...); err != nil {
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
		if _, err := db.ExecContext(ctx, updateSQL, updateArgs...); err != nil {
			return fmt.Errorf("update hash cache: %w", err)
		}
	}
	return nil
}

func (dao *fileHashCacheDao) ListAll(ctx context.Context) ([]HashCacheEntry, error) {
	db := dao.dbGetter()
	if db == nil {
		return nil, fmt.Errorf("hash cache dao not initialised")
	}
	const query = `SELECT location FROM file_hash_cache_tab`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list hash cache: %w", err)
	}
	defer rows.Close()

	var result []HashCacheEntry
	for rows.Next() {
		var entry HashCacheEntry
		if err := rows.Scan(&entry.Location); err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (dao *fileHashCacheDao) DeleteByLocations(ctx context.Context, locations []string) error {
	if len(locations) == 0 {
		return nil
	}
	db := dao.dbGetter()
	if db == nil {
		return fmt.Errorf("hash cache dao not initialised")
	}
	where := map[string]interface{}{"location in": locations}
	deleteSQL, args, err := builder.BuildDelete(hashCacheTableName, where)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, deleteSQL, args...)
	if err != nil {
		return fmt.Errorf("delete hash cache entries: %w", err)
	}
	return nil
}
