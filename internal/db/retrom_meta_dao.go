package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lib/pq"
)

// RetromGameFile represents a file entry in the retrom database.
type RetromGameFile struct {
	GameID int
	Path   string
}

// RetromMetaPayload holds the metadata values to persist into retrom.
type RetromMetaPayload struct {
	Name           sql.NullString
	Description    sql.NullString
	CoverURL       sql.NullString
	BackgroundURL  sql.NullString
	IconURL        sql.NullString
	Links          []string
	VideoURLs      []string
	ScreenshotURLs []string
	ArtworkURLs    []string
}

// RetromMetaDAO encapsulates database access for the retrom metadata tables.
type RetromMetaDAO struct {
	db *sql.DB
}

// NewRetromMetaDAO opens a PostgreSQL connection and returns a DAO instance.
func NewRetromMetaDAO(dsn string) (*RetromMetaDAO, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	return &RetromMetaDAO{db: db}, nil
}

// Close releases the underlying database connection.
func (dao *RetromMetaDAO) Close() error {
	if dao.db == nil {
		return nil
	}
	return dao.db.Close()
}

const listActiveGameFilesSQL = `
SELECT gf.game_id, gf.path
FROM game_files gf
JOIN games g ON g.id = gf.game_id
WHERE NOT gf.is_deleted AND NOT g.is_deleted`

// ForEachActiveGameFile iterates over non-deleted game files and calls fn for each record.
func (dao *RetromMetaDAO) ForEachActiveGameFile(ctx context.Context, fn func(RetromGameFile) error) error {
	rows, err := dao.db.QueryContext(ctx, listActiveGameFilesSQL)
	if err != nil {
		return fmt.Errorf("query game files: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var rec RetromGameFile
		if err := rows.Scan(&rec.GameID, &rec.Path); err != nil {
			return fmt.Errorf("scan game files: %w", err)
		}
		if err := fn(rec); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

const gameMetadataExistsSQL = `SELECT 1 FROM game_metadata WHERE game_id = $1 LIMIT 1`

// GameMetadataExists checks whether metadata already exists for the given game.
func (dao *RetromMetaDAO) GameMetadataExists(ctx context.Context, gameID int) (bool, error) {
	var dummy int
	err := dao.db.QueryRowContext(ctx, gameMetadataExistsSQL, gameID).Scan(&dummy)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, err
}

const insertGameMetadataSQL = `
INSERT INTO game_metadata (
	game_id, name, description, cover_url, background_url, icon_url,
	links, video_urls, screenshot_urls, artwork_urls, created_at, updated_at
) VALUES (
	$1, $2, $3, $4, $5, $6,
	$7, $8, $9, $10, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
)`

// InsertGameMetadata inserts a new metadata record for the given game.
func (dao *RetromMetaDAO) InsertGameMetadata(ctx context.Context, gameID int, payload RetromMetaPayload) error {
	_, err := dao.db.ExecContext(ctx, insertGameMetadataSQL,
		gameID,
		payload.Name,
		payload.Description,
		payload.CoverURL,
		payload.BackgroundURL,
		payload.IconURL,
		pq.Array(payload.Links),
		pq.Array(payload.VideoURLs),
		pq.Array(payload.ScreenshotURLs),
		pq.Array(payload.ArtworkURLs),
	)
	if err != nil {
		return fmt.Errorf("insert metadata for game %d: %w", gameID, err)
	}
	return nil
}

const upsertGameMetadataSQL = `
INSERT INTO game_metadata (
	game_id, name, description, cover_url, background_url, icon_url,
	links, video_urls, screenshot_urls, artwork_urls, created_at, updated_at
) VALUES (
	$1, $2, $3, $4, $5, $6,
	$7, $8, $9, $10, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
) ON CONFLICT (game_id) DO UPDATE SET
	name = EXCLUDED.name,
	description = EXCLUDED.description,
	cover_url = EXCLUDED.cover_url,
	background_url = EXCLUDED.background_url,
	icon_url = EXCLUDED.icon_url,
	links = EXCLUDED.links,
	video_urls = EXCLUDED.video_urls,
	screenshot_urls = EXCLUDED.screenshot_urls,
	artwork_urls = EXCLUDED.artwork_urls,
	updated_at = CURRENT_TIMESTAMP
RETURNING (xmax = 0)`

// UpsertGameMetadata inserts or updates metadata, returning true when an insert was performed.
func (dao *RetromMetaDAO) UpsertGameMetadata(ctx context.Context, gameID int, payload RetromMetaPayload) (bool, error) {
	var inserted bool
	err := dao.db.QueryRowContext(ctx, upsertGameMetadataSQL,
		gameID,
		payload.Name,
		payload.Description,
		payload.CoverURL,
		payload.BackgroundURL,
		payload.IconURL,
		pq.Array(payload.Links),
		pq.Array(payload.VideoURLs),
		pq.Array(payload.ScreenshotURLs),
		pq.Array(payload.ArtworkURLs),
	).Scan(&inserted)
	if err != nil {
		return false, fmt.Errorf("upsert metadata for game %d: %w", gameID, err)
	}
	return inserted, nil
}
