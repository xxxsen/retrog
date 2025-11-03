package db

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/xxxsen/retrog/internal/model"

	"github.com/didi/gendry/builder"
	"github.com/xxxsen/common/database"
)

const (
	metaTableName   = "retro_game_meta_tab"
	upsertBatchSize = 50
)

// MetaDAO exposes helpers for reading and writing ROM metadata records.
type MetaDAO struct {
	db database.IDatabase
}

// NewMetaDAO builds a DAO using the globally configured database.
func NewMetaDAO() (*MetaDAO, error) {
	db := Default()
	if db == nil {
		return nil, errors.New("database not initialised")
	}
	return &MetaDAO{db: db}, nil
}

// Upsert inserts or updates metadata records, returning the number of inserted and updated rows.
func (dao *MetaDAO) Upsert(ctx context.Context, records map[string]model.Entry) (inserted int, updated int, err error) {
	if len(records) == 0 {
		return 0, 0, nil
	}

	keys := make([]string, 0, len(records))
	for hash := range records {
		keys = append(keys, hash)
	}
	sort.Strings(keys)

	for start := 0; start < len(keys); start += upsertBatchSize {
		end := start + upsertBatchSize
		if end > len(keys) {
			end = len(keys)
		}
		batchKeys := keys[start:end]

		err = dao.db.OnTransation(ctx, func(ctx context.Context, tx database.IQueryExecer) error {
			for _, hash := range batchKeys {
				record := records[hash]
				extJSON, err := record.MarshalExtInfo()
				if err != nil {
					return err
				}
				now := time.Now().Unix()
				insertPayload := []map[string]interface{}{{
					"rom_hash":    hash,
					"rom_name":    record.Name,
					"rom_desc":    record.Desc,
					"rom_size":    record.Size,
					"create_time": now,
					"update_time": now,
					"ext_info":    extJSON,
				}}
				insertSQL, insertArgs, err := builder.BuildInsert(metaTableName, insertPayload)
				if err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, insertSQL, insertArgs...); err != nil {
					if !isUniqueConstraintError(err) {
						return err
					}
					updateSQL, updateArgs, err := builder.BuildUpdate(metaTableName, map[string]interface{}{"rom_hash": hash}, map[string]interface{}{
						"rom_name":    record.Name,
						"rom_desc":    record.Desc,
						"rom_size":    record.Size,
						"update_time": now,
						"ext_info":    extJSON,
					})
					if err != nil {
						return err
					}
					if _, err := tx.ExecContext(ctx, updateSQL, updateArgs...); err != nil {
						return err
					}
					updated++
					continue
				}

				inserted++
			}
			return nil
		})
		if err != nil {
			return inserted, updated, err
		}
	}

	return inserted, updated, nil
}

// FetchByHashes returns metadata entries for the requested ROM hashes.
func (dao *MetaDAO) FetchByHashes(ctx context.Context, hashes []string) (map[string]model.Entry, []string, error) {
	result := make(map[string]model.Entry, len(hashes))
	missing := make([]string, 0)
	if len(hashes) == 0 {
		return result, missing, nil
	}

	where := map[string]interface{}{"rom_hash in": hashes}
	selectSQL, args, err := builder.BuildSelect(metaTableName, where, []string{"rom_hash", "rom_name", "rom_desc", "rom_size", "ext_info"})
	if err != nil {
		return nil, nil, err
	}
	rows, err := dao.db.QueryContext(ctx, selectSQL, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	found := make(map[string]struct{})
	for rows.Next() {
		var (
			hash    string
			name    string
			desc    string
			size    int64
			extInfo string
		)
		if err := rows.Scan(&hash, &name, &desc, &size, &extInfo); err != nil {
			return nil, nil, err
		}
		entry, err := model.FromRecord(name, desc, size, extInfo)
		if err != nil {
			return nil, nil, err
		}
		result[hash] = entry
		found[hash] = struct{}{}
	}

	for _, hash := range hashes {
		if _, ok := found[hash]; !ok {
			missing = append(missing, hash)
		}
	}

	return result, missing, rows.Err()
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}
