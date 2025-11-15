package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xxxsen/retrog/internal/constant"
	appdb "github.com/xxxsen/retrog/internal/db"
	"github.com/xxxsen/retrog/internal/metadata"
	"github.com/xxxsen/retrog/internal/model"
	"github.com/xxxsen/retrog/internal/storage"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

type MatchUnlinkCommand struct {
	unlinkPath string
	outputPath string
	fix        bool
	replace    bool
}

func NewMatchUnlinkCommand() *MatchUnlinkCommand {
	return &MatchUnlinkCommand{}
}

func (c *MatchUnlinkCommand) Name() string { return "match-unlink" }

func (c *MatchUnlinkCommand) Desc() string {
	return "统计 scan-unlink 输出中可匹配到 meta.db 元数据的 ROM 列表"
}

func (c *MatchUnlinkCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.unlinkPath, "unlink-meta", "", "scan-unlink 输出的 JSON 文件")
	f.StringVar(&c.outputPath, "output", "", "统计结果输出路径")
	f.BoolVar(&c.fix, "fix", false, "根据匹配结果补充 gamelist.xml")
	f.BoolVar(&c.replace, "replace", false, "与 --fix 搭配使用，允许直接覆盖 gamelist.xml；默认输出 gamelist.xml.fix")
}

func (c *MatchUnlinkCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.unlinkPath) == "" {
		return errors.New("match-unlink requires --unlink-meta")
	}
	if strings.TrimSpace(c.outputPath) == "" {
		return errors.New("match-unlink requires --output")
	}
	logutil.GetLogger(ctx).Info("starting match-unlink",
		zap.String("unlink_meta", c.unlinkPath),
		zap.String("output", c.outputPath),
		zap.Bool("fix", c.fix),
		zap.Bool("replace", c.replace),
	)
	return nil
}

func (c *MatchUnlinkCommand) Run(ctx context.Context) error {
	data, err := os.ReadFile(c.unlinkPath)
	if err != nil {
		return fmt.Errorf("read unlink meta %s: %w", c.unlinkPath, err)
	}
	var input model.UnlinkReport
	if err := json.Unmarshal(data, &input); err != nil {
		return fmt.Errorf("decode unlink meta %s: %w", c.unlinkPath, err)
	}

	hashSet := make(map[string]struct{})
	for _, entry := range input.Unlink {
		for _, file := range entry.Files {
			hash := c.normalizeHash(file.Hash)
			if hash == "" {
				continue
			}
			hashSet[hash] = struct{}{}
		}
	}

	matchedEntries, err := c.fetchMatchedEntries(ctx, appdb.MetaDao, hashSet)
	if err != nil {
		return err
	}

	results := make([]model.MatchResult, 0)
	for _, entry := range input.Unlink {
		matchedFiles := make([]model.UnlinkFile, 0)
		for _, file := range entry.Files {
			hash := c.normalizeHash(file.Hash)
			if hash == "" {
				continue
			}
			if _, ok := matchedEntries[hash]; ok {
				matchedFiles = append(matchedFiles, file)
			}
		}
		if len(matchedFiles) == 0 {
			continue
		}
		results = append(results, model.MatchResult{
			Location:   filepath.ToSlash(entry.Location),
			MissCount:  entry.Count,
			MatchCount: len(matchedFiles),
			Files:      matchedFiles,
		})
	}

	output := model.MatchOutput{Match: results}
	outData, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal match output: %w", err)
	}
	if err := os.WriteFile(c.outputPath, outData, 0o644); err != nil {
		return fmt.Errorf("write match output %s: %w", c.outputPath, err)
	}

	logutil.GetLogger(ctx).Info("match-unlink completed",
		zap.Int("locations", len(results)),
		zap.String("output", c.outputPath),
	)

	if c.fix && len(results) > 0 {
		if err := c.fixGamelists(ctx, results, matchedEntries); err != nil {
			return err
		}
	}

	return nil
}

func (c *MatchUnlinkCommand) PostRun(ctx context.Context) error { return nil }

func (c *MatchUnlinkCommand) fetchMatchedEntries(ctx context.Context, dao *appdb.MetaDAO, hashSet map[string]struct{}) (map[string]model.MatchedMeta, error) {
	result := make(map[string]model.MatchedMeta, len(hashSet))
	if len(hashSet) == 0 {
		return result, nil
	}

	hashes := make([]string, 0, len(hashSet))
	for hash := range hashSet {
		hashes = append(hashes, hash)
	}

	const chunkSize = 200
	for start := 0; start < len(hashes); start += chunkSize {
		end := start + chunkSize
		if end > len(hashes) {
			end = len(hashes)
		}
		chunk := hashes[start:end]
		entries, _, err := dao.FetchStoredByHashes(ctx, chunk)
		if err != nil {
			return nil, err
		}
		for hash, stored := range entries {
			result[c.normalizeHash(hash)] = model.MatchedMeta{
				Entry: stored.Entry,
				ID:    stored.ID,
			}
		}
	}

	return result, nil
}

func (c *MatchUnlinkCommand) normalizeHash(hash string) string {
	return strings.ToLower(strings.TrimSpace(hash))
}

func init() {
	RegisterRunner("match-unlink", func() IRunner { return NewMatchUnlinkCommand() })
}

func (c *MatchUnlinkCommand) fixGamelists(ctx context.Context, results []model.MatchResult, entries map[string]model.MatchedMeta) error {
	store := storage.DefaultClient()
	if store == nil {
		return errors.New("storage client not initialised")
	}

	logger := logutil.GetLogger(ctx)
	for _, result := range results {
		dir := filepath.Clean(result.Location)
		gamelistPath := filepath.Join(dir, constant.DefaultGamelistFile)
		if _, err := os.Stat(gamelistPath); err != nil {
			logger.Warn("gamelist missing, skip fix",
				zap.String("path", filepath.ToSlash(gamelistPath)),
				zap.Error(err),
			)
			continue
		}

		doc, err := metadata.ParseGamelistFile(gamelistPath)
		if err != nil {
			logger.Warn("parse gamelist failed, skip fix",
				zap.String("path", filepath.ToSlash(gamelistPath)),
				zap.Error(err),
			)
			continue
		}

		extraEntries := make([]metadata.GamelistEntry, 0, len(result.Files))
		for _, file := range result.Files {
			meta, ok := entries[c.normalizeHash(file.Hash)]
			if !ok {
				continue
			}
			gameEntry, err := c.buildGamelistEntry(ctx, store, dir, file, meta)
			if err != nil {
				logger.Error("build gamelist entry failed",
					zap.String("location", dir),
					zap.String("rom", file.Name),
					zap.Error(err),
				)
				continue
			}
			extraEntries = append(extraEntries, *gameEntry)
		}
		if len(extraEntries) == 0 {
			continue
		}

		destPath := gamelistPath
		if !c.replace {
			destPath = gamelistPath + ".fix"
		}

		updatedDoc := *doc
		updatedDoc.Games = append(updatedDoc.Games, extraEntries...)

		if err := metadata.WriteGamelistFile(destPath, &updatedDoc); err != nil {
			return err
		}
		logger.Info("gamelist updated",
			zap.String("path", filepath.ToSlash(destPath)),
			zap.Int("added", len(extraEntries)),
		)
	}
	return nil
}

func (c *MatchUnlinkCommand) buildGamelistEntry(ctx context.Context, store storage.Client, dir string, file model.UnlinkFile, meta model.MatchedMeta) (*metadata.GamelistEntry, error) {
	entry := meta.Entry
	name := entry.Name
	if strings.TrimSpace(name) == "" {
		name = file.Name
	}

	pathValue := "./" + file.Name
	desc := strings.TrimSpace(entry.Desc)

	imagePath, err := c.downloadMedia(ctx, store, dir, constant.ImageDir, entry.Media, []string{"boxart", "boxfront", "screenshot"})
	if err != nil {
		return nil, err
	}
	logoPath, err := c.downloadMedia(ctx, store, dir, constant.ImageDir, entry.Media, []string{"logo"})
	if err != nil {
		return nil, err
	}
	videoPath, err := c.downloadMedia(ctx, store, dir, constant.VideoDir, entry.Media, []string{"video"})
	if err != nil {
		return nil, err
	}

	var release string
	if entry.ReleaseAt > 0 {
		release = time.Unix(entry.ReleaseAt, 0).UTC().Format("20060102T150405")
	}

	genres := make([]string, 0, len(entry.Genres))
	for _, g := range entry.Genres {
		if trimmed := strings.TrimSpace(g); trimmed != "" {
			genres = append(genres, trimmed)
		}
	}

	return &metadata.GamelistEntry{
		ID:          fmt.Sprintf("%d", meta.ID),
		Source:      "retrog",
		Path:        pathValue,
		Name:        name,
		Description: desc,
		Image:       imagePath,
		Marquee:     logoPath,
		Video:       videoPath,
		ReleaseDate: release,
		Developer:   entry.Developer,
		Publisher:   entry.Publisher,
		Genres:      genres,
		MD5:         c.normalizeHash(file.Hash),
	}, nil
}

func (c *MatchUnlinkCommand) downloadMedia(ctx context.Context, store storage.Client, dir string, category string, mediaList []model.MediaEntry, preferred []string) (string, error) {
	target := pickMedia(mediaList, preferred)
	if target == nil {
		return "", nil
	}

	subdir := filepath.Join(category, constant.RetrogMediaSubdir)
	destDir := filepath.Join(dir, subdir)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create media dir %s: %w", destDir, err)
	}

	filename := target.Hash + target.Ext
	destPath := filepath.Join(destDir, filename)
	if _, err := os.Stat(destPath); errors.Is(err, os.ErrNotExist) {
		if err := store.DownloadToFile(ctx, target.Hash+target.Ext, destPath); err != nil {
			return "", fmt.Errorf("download media %s: %w", target.Hash+target.Ext, err)
		}
	} else if err != nil {
		return "", fmt.Errorf("stat media %s: %w", destPath, err)
	}

	rel := "./" + filepath.ToSlash(filepath.Join(subdir, filename))
	return rel, nil
}

func pickMedia(media []model.MediaEntry, preferred []string) *model.MediaEntry {
	for _, typ := range preferred {
		for _, item := range media {
			if strings.EqualFold(item.Type, typ) {
				copy := item
				return &copy
			}
		}
	}
	return nil
}
