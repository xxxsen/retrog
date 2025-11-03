package db

import (
	"context"
	"errors"
	"time"

	"retrog/internal/model"

	"github.com/xxxsen/common/database"
)

const (
	selectMetaForUpsertSQL = `SELECT id FROM retro_game_meta_tab WHERE rom_hash = ?`
	insertMetaSQL          = `INSERT INTO retro_game_meta_tab (rom_hash, rom_name, rom_desc, rom_size, create_time, update_time, ext_info) VALUES (?, ?, ?, ?, ?, ?, ?)`
	updateMetaSQL          = `UPDATE retro_game_meta_tab SET rom_name = ?, rom_desc = ?, rom_size = ?, update_time = ?, ext_info = ? WHERE rom_hash = ?`
	selectMetaByHashSQL    = `SELECT rom_name, rom_desc, rom_size, ext_info FROM retro_game_meta_tab WHERE rom_hash = ?`
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
	err = dao.db.OnTransation(ctx, func(ctx context.Context, tx database.IQueryExecer) error {
		now := time.Now().Unix()
		for hash, record := range records {
			extJSON, err := record.MarshalExtInfo()
			if err != nil {
				return err
			}

			rows, err := tx.QueryContext(ctx, selectMetaForUpsertSQL, hash)
			if err != nil {
				return err
			}
			var existingID int64
			if rows.Next() {
				if err := rows.Scan(&existingID); err != nil {
					rows.Close()
					return err
				}
			}
			rows.Close()

			if existingID == 0 {
				if _, err := tx.ExecContext(ctx, insertMetaSQL, hash, record.Name, record.Desc, record.Size, now, now, extJSON); err != nil {
					return err
				}
				inserted++
			} else {
				if _, err := tx.ExecContext(ctx, updateMetaSQL, record.Name, record.Desc, record.Size, now, extJSON, hash); err != nil {
					return err
				}
				updated++
			}
		}
		return nil
	})
	return inserted, updated, err
}

// FetchByHashes returns metadata entries for the requested ROM hashes.
func (dao *MetaDAO) FetchByHashes(ctx context.Context, hashes []string) (map[string]model.Entry, []string, error) {
	result := make(map[string]model.Entry, len(hashes))
	missing := make([]string, 0)
	for _, hash := range hashes {
		rows, err := dao.db.QueryContext(ctx, selectMetaByHashSQL, hash)
		if err != nil {
			return nil, nil, err
		}
		var (
			name    string
			desc    string
			size    int64
			extInfo string
		)
		found := false
		if rows.Next() {
			found = true
			if err := rows.Scan(&name, &desc, &size, &extInfo); err != nil {
				rows.Close()
				return nil, nil, err
			}
		}
		rows.Close()

		if !found {
			missing = append(missing, hash)
			continue
		}

		entry, err := model.FromRecord(name, desc, size, extInfo)
		if err != nil {
			return nil, nil, err
		}
		result[hash] = entry
	}
	return result, missing, nil
}
