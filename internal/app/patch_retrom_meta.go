package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	appdb "github.com/xxxsen/retrog/internal/db"
	"github.com/xxxsen/retrog/internal/model"

	"github.com/lib/pq"
	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

type PatchRetromMetaCommand struct {
	dblink        string
	dryRun        bool
	rootMapping   string
	hostRoot      string
	containerRoot string
	useMapping    bool
}

func (c *PatchRetromMetaCommand) Name() string { return "patch-retrom-meta" }

func (c *PatchRetromMetaCommand) Desc() string {
	return "根据 meta.db 补齐 retrom 库中的 game_metadata 数据"
}

func NewPatchRetromMetaCommand() *PatchRetromMetaCommand {
	return &PatchRetromMetaCommand{}
}

func (c *PatchRetromMetaCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.dblink, "dblink", "", "PostgreSQL 连接字符串")
	f.BoolVar(&c.dryRun, "dryrun", false, "测试模式，只打印操作不写入数据库")
	f.StringVar(&c.rootMapping, "root-mapping", "", "路径映射，格式为 \"{host-root}:{container-root}\"，留空则使用原始路径")
}

func (c *PatchRetromMetaCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.dblink) == "" {
		return errors.New("patch-retrom-meta requires --dblink")
	}
	mapping := strings.TrimSpace(c.rootMapping)
	c.useMapping = mapping != ""
	if c.useMapping {
		parts := strings.SplitN(mapping, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid root-mapping format: %s", mapping)
		}
		c.hostRoot = filepath.Clean(parts[0])
		c.containerRoot = filepath.Clean(parts[1])
		if c.containerRoot == "." || !filepath.IsAbs(c.containerRoot) {
			return fmt.Errorf("container part must be absolute path: %s", c.containerRoot)
		}
	}

	fields := []zap.Field{
		zap.String("dblink", c.dblink),
		zap.Bool("dry_run", c.dryRun),
	}
	if c.useMapping {
		fields = append(fields,
			zap.String("host_root", c.hostRoot),
			zap.String("container_root", c.containerRoot),
		)
	} else {
		fields = append(fields, zap.String("root_mapping", ""))
	}
	logutil.GetLogger(ctx).Info("starting patch-retrom-meta", fields...)
	return nil
}

func (c *PatchRetromMetaCommand) Run(ctx context.Context) error {
	dao, err := appdb.NewMetaDAO()
	if err != nil {
		return err
	}

	db, err := sql.Open("postgres", c.dblink)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer db.Close()

	const query = `
SELECT gf.game_id, gf.path
FROM game_files gf
JOIN games g ON g.id = gf.game_id
WHERE NOT gf.is_deleted AND NOT g.is_deleted`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("query game files: %w", err)
	}
	defer rows.Close()

	logger := logutil.GetLogger(ctx)
	type result struct {
		gameID int
		path   string
	}

	var processed, inserted, updated, skipped int

	for rows.Next() {
		var r result
		if err := rows.Scan(&r.gameID, &r.path); err != nil {
			return fmt.Errorf("scan game files: %w", err)
		}
		processed++

		hostPath, ok := c.resolveHostPath(r.path)
		if !ok {
			logger.Warn("path not under container root",
				zap.Int("game_id", r.gameID),
				zap.String("path", r.path),
			)
			skipped++
			continue
		}

		hash, err := fileMD5(hostPath)
		if err != nil {
			logger.Warn("failed to compute md5",
				zap.Int("game_id", r.gameID),
				zap.String("path", hostPath),
				zap.Error(err),
			)
			skipped++
			continue
		}

		metaMap, missing, err := dao.FetchByHashes(ctx, []string{hash})
		if err != nil {
			return fmt.Errorf("fetch meta for hash %s: %w", hash, err)
		}
		if len(missing) > 0 {
			logger.Warn("meta not found for hash",
				zap.Int("game_id", r.gameID),
				zap.String("hash", hash),
			)
			skipped++
			continue
		}

		entry := metaMap[hash]
		payload := buildMetaPayload(entry)

		if c.dryRun {
			logger.Info("dryrun patch metadata",
				zap.Int("game_id", r.gameID),
				zap.String("hash", hash),
				zap.Any("meta", payload),
			)
			continue
		}

		isInsert, err := upsertGameMetadata(ctx, db, r.gameID, payload)
		if err != nil {
			return fmt.Errorf("upsert metadata for game %d: %w", r.gameID, err)
		}

		if isInsert {
			inserted++
		} else {
			updated++
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	logger.Info("patch-retrom-meta finished",
		zap.Int("processed", processed),
		zap.Int("inserted", inserted),
		zap.Int("updated", updated),
		zap.Int("skipped", skipped),
	)
	return nil
}

func (c *PatchRetromMetaCommand) PostRun(ctx context.Context) error { return nil }

func (c *PatchRetromMetaCommand) resolveHostPath(containerPath string) (string, bool) {
	if !c.useMapping {
		return filepath.Clean(containerPath), true
	}
	normalizedRoot := filepath.ToSlash(c.containerRoot)
	clean := filepath.ToSlash(filepath.Clean(containerPath))
	if !strings.HasPrefix(clean, normalizedRoot) {
		return "", false
	}
	rel := strings.TrimPrefix(clean, normalizedRoot)
	rel = strings.TrimPrefix(rel, "/")
	return filepath.Join(c.hostRoot, filepath.FromSlash(rel)), true
}

type metaPayload struct {
	name           sql.NullString
	description    sql.NullString
	coverURL       sql.NullString
	backgroundURL  sql.NullString
	iconURL        sql.NullString
	links          []string
	videoURLs      []string
	screenshotURLs []string
	artworkURLs    []string
}

func buildMetaPayload(entry model.Entry) metaPayload {
	var cover, background, icon sql.NullString
	var videos, screenshots, artworks []string

	for _, m := range entry.Media {
		url := mediaURL(m)
		switch m.Type {
		case "boxart", "boxfront":
			if !cover.Valid {
				cover = sql.NullString{String: url, Valid: true}
			}
			artworks = append(artworks, url)
		case "screenshot":
			if !background.Valid {
				background = sql.NullString{String: url, Valid: true}
			}
			screenshots = append(screenshots, url)
		case "logo":
			if !icon.Valid {
				icon = sql.NullString{String: url, Valid: true}
			}
		case "video":
			videos = append(videos, url)
		}
	}

	if videos == nil {
		videos = make([]string, 0)
	}
	if screenshots == nil {
		screenshots = make([]string, 0)
	}
	if artworks == nil {
		artworks = make([]string, 0)
	}

	return metaPayload{
		name:           sql.NullString{String: entry.Name, Valid: entry.Name != ""},
		description:    sql.NullString{String: entry.Desc, Valid: entry.Desc != ""},
		coverURL:       cover,
		backgroundURL:  background,
		iconURL:        icon,
		links:          make([]string, 0),
		videoURLs:      videos,
		screenshotURLs: screenshots,
		artworkURLs:    artworks,
	}
}

func mediaURL(m model.MediaEntry) string {
	return fmt.Sprintf("media/%s%s", m.Hash, m.Ext)
}

func upsertGameMetadata(ctx context.Context, db *sql.DB, gameID int, payload metaPayload) (bool, error) {
	const stmt = `
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

	var inserted bool
	err := db.QueryRowContext(ctx, stmt,
		gameID,
		payload.name,
		payload.description,
		payload.coverURL,
		payload.backgroundURL,
		payload.iconURL,
		pq.Array(payload.links),
		pq.Array(payload.videoURLs),
		pq.Array(payload.screenshotURLs),
		pq.Array(payload.artworkURLs),
	).Scan(&inserted)
	if err != nil {
		return false, err
	}
	return inserted, nil
}

func init() {
	RegisterRunner("patch-retrom-meta", func() IRunner { return NewPatchRetromMetaCommand() })
}
