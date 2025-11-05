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
	"github.com/xxxsen/retrog/internal/storage"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

type PatchRetromMetaCommand struct {
	dblink        string
	dryRun        bool
	allowUpdate   bool
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
	f.BoolVar(&c.allowUpdate, "allow-update", false, "允许更新已存在的元数据，默认只新增")
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
		zap.Bool("allow_update", c.allowUpdate),
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
	sqliteDAO := appdb.MetaDao
	retromDAO, err := appdb.NewRetromMetaDAO(c.dblink)
	if err != nil {
		return err
	}
	defer retromDAO.Close()

	logger := logutil.GetLogger(ctx)

	var processed, inserted, updated, skipped int

	err = retromDAO.ForEachActiveGameFile(ctx, func(record appdb.RetromGameFile) error {
		processed++

		hostPath, ok := c.resolveHostPath(record.Path)
		if !ok {
			logger.Warn("path not under container root",
				zap.Int("game_id", record.GameID),
				zap.String("path", record.Path),
			)
			skipped++
			return nil
		}

		hash, err := readFileMD5WithCache(hostPath)
		if err != nil {
			logger.Warn("failed to compute md5",
				zap.Int("game_id", record.GameID),
				zap.String("path", hostPath),
				zap.Error(err),
			)
			skipped++
			return nil
		}

		metaMap, missing, err := sqliteDAO.FetchByHashes(ctx, []string{hash})
		if err != nil {
			return fmt.Errorf("fetch meta for hash %s: %w", hash, err)
		}
		if len(missing) > 0 {
			logger.Warn("meta not found for hash",
				zap.Int("game_id", record.GameID),
				zap.String("hash", hash),
			)
			skipped++
			return nil
		}

		entry := metaMap[hash]
		payload := buildMetaPayload(ctx, entry)

		exists, err := retromDAO.GameMetadataExists(ctx, record.GameID)
		if err != nil {
			return err
		}

		if c.dryRun {
			action := "update"
			if !exists {
				action = "insert"
			} else if !c.allowUpdate {
				action = "skip"
			}
			logger.Info("dryrun patch metadata",
				zap.Int("game_id", record.GameID),
				zap.String("hash", hash),
				zap.String("action", action),
				zap.Any("meta", payload),
			)
			if action == "skip" {
				skipped++
			}
			return nil
		}

		if exists && !c.allowUpdate {
			logger.Info("skip existing metadata",
				zap.Int("game_id", record.GameID),
				zap.String("hash", hash),
			)
			skipped++
			return nil
		}

		if c.allowUpdate {
			isInsert, err := retromDAO.UpsertGameMetadata(ctx, record.GameID, payload)
			if err != nil {
				return err
			}
			if isInsert {
				inserted++
			} else {
				updated++
			}
			return nil
		}

		if err := retromDAO.InsertGameMetadata(ctx, record.GameID, payload); err != nil {
			return err
		}
		inserted++
		return nil
	})
	if err != nil {
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

func buildMetaPayload(ctx context.Context, entry model.Entry) appdb.RetromMetaPayload {
	var cover, background, icon sql.NullString
	var videos, screenshots, artworks []string

	for _, m := range entry.Media {
		url := mediaURL(ctx, m)
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

	return appdb.RetromMetaPayload{
		Name:           sql.NullString{String: entry.Name, Valid: entry.Name != ""},
		Description:    sql.NullString{String: entry.Desc, Valid: entry.Desc != ""},
		CoverURL:       cover,
		BackgroundURL:  background,
		IconURL:        icon,
		Links:          make([]string, 0),
		VideoURLs:      videos,
		ScreenshotURLs: screenshots,
		ArtworkURLs:    artworks,
	}
}

func mediaURL(ctx context.Context, m model.MediaEntry) string {
	key := m.Hash + m.Ext
	return storage.DefaultClient().GetDownloadLink(ctx, key)
}

func init() {
	RegisterRunner("patch-retrom-meta", func() IRunner { return NewPatchRetromMetaCommand() })
}
