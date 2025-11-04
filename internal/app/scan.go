package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"

	appdb "github.com/xxxsen/retrog/internal/db"
	"github.com/xxxsen/retrog/internal/metadata"
	"github.com/xxxsen/retrog/internal/model"
	"github.com/xxxsen/retrog/internal/storage"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

const metadataFile = "metadata.pegasus.txt"

var mediaCandidates = map[string]string{
	"boxart":     "boxart",
	"boxfront":   "boxfront",
	"screenshot": "screenshot",
	"video":      "video",
	"logo":       "logo",
}

var mediaAssetAliases = map[string][]string{
	"boxart":     {"boxart", "box_front", "boxfront"},
	"boxfront":   {"box_front", "boxfront", "boxart"},
	"screenshot": {"screenshot"},
	"video":      {"video"},
	"logo":       {"logo"},
}

type ScanCommand struct {
	romDir string
}

func (c *ScanCommand) Name() string { return "scan" }

func (c *ScanCommand) Desc() string {
	return "扫描 ROM 目录，上传媒体并写入元数据"
}

func NewScanCommand() *ScanCommand { return &ScanCommand{} }

func (c *ScanCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.romDir, "dir", "", "ROM 根目录")
}

func (c *ScanCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.romDir) == "" {
		return errors.New("scan requires --dir")
	}

	logutil.GetLogger(ctx).Info("starting scan",
		zap.String("dir", c.romDir),
	)
	return nil
}

func (c *ScanCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)

	store := storage.DefaultClient()
	if store == nil {
		return errors.New("storage client not initialised")
	}

	meta, err := c.buildMeta(ctx, store)
	if err != nil {
		return err
	}

	inserted, updated, err := c.persistMeta(ctx, meta)
	if err != nil {
		return err
	}

	logger.Info("scan completed",
		zap.Int("entries", len(meta)),
		zap.Int("inserted", inserted),
		zap.Int("updated", updated),
	)
	return nil
}

func (c *ScanCommand) PostRun(ctx context.Context) error { return nil }

func (c *ScanCommand) buildMeta(ctx context.Context, store storage.Client) (map[string]model.Entry, error) {
	logger := logutil.GetLogger(ctx)
	logger.Info("scanning rom tree", zap.String("root", c.romDir))

	meta := make(map[string]model.Entry)
	processed := make(map[string]struct{})

	err := filepath.WalkDir(c.romDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || d.Name() != metadataFile {
			return nil
		}

		dir := filepath.Dir(path)
		if _, ok := processed[dir]; ok {
			return nil
		}

		records, err := c.processCategory(ctx, store, dir)
		if err != nil {
			return err
		}
		for hash, item := range records {
			meta[hash] = item
		}
		processed[dir] = struct{}{}

		rel, _ := filepath.Rel(c.romDir, dir)
		logger.Debug("metadata processed",
			zap.String("dir", filepath.ToSlash(rel)),
			zap.Int("records", len(records)),
		)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(meta) == 0 {
		logger.Warn("no metadata discovered", zap.String("root", c.romDir))
	}
	return meta, nil
}

func (c *ScanCommand) persistMeta(ctx context.Context, meta map[string]model.Entry) (int, int, error) {
	dao, err := appdb.NewMetaDAO()
	if err != nil {
		return 0, 0, err
	}
	return dao.Upsert(ctx, meta)
}

func (c *ScanCommand) processCategory(ctx context.Context, store storage.Client, categoryPath string) (map[string]model.Entry, error) {
	metaPath := filepath.Join(categoryPath, metadataFile)
	logger := logutil.GetLogger(ctx)
	logger.Debug("processing metadata", zap.String("path", metaPath))

	doc, err := metadata.Parse(metaPath)
	if err != nil {
		return nil, fmt.Errorf("parse metadata %s: %w", metaPath, err)
	}
	if rel, err := filepath.Rel(c.romDir, categoryPath); err == nil {
		doc.Cat = filepath.ToSlash(rel)
	}
	if len(doc.Games) == 0 {
		return map[string]model.Entry{}, nil
	}

	result := make(map[string]model.Entry)
	for _, gameDef := range doc.Games {
		entries, err := c.processGame(ctx, store, categoryPath, gameDef)
		if err != nil {
			return nil, err
		}
		for hash, item := range entries {
			result[hash] = item
		}
	}
	return result, nil
}

func (c *ScanCommand) processGame(ctx context.Context, store storage.Client, categoryPath string, gameDef metadata.Game) (map[string]model.Entry, error) {
	entries := make(map[string]model.Entry)

	cleanedName := cleanGameName(gameDef.Name)
	cleanedDesc := cleanDescription(gameDef.Description)

	mediaDir := c.findMediaDir(categoryPath, gameDef)
	mediaMap, err := c.collectMedia(ctx, store, categoryPath, mediaDir, gameDef)
	if err != nil {
		return nil, err
	}

	for _, rel := range gameDef.Files {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		full := filepath.Join(categoryPath, rel)
		info, err := os.Stat(full)
		if err != nil {
			return nil, fmt.Errorf("stat rom file %s: %w", full, err)
		}
		if info.IsDir() {
			continue
		}
		md5sum, err := fileMD5(full)
		if err != nil {
			return nil, err
		}
		mediaCopy := make([]model.MediaEntry, 0, len(mediaMap))
		if len(mediaMap) > 0 {
			keys := make([]string, 0, len(mediaMap))
			for mediaType := range mediaMap {
				keys = append(keys, mediaType)
			}
			sort.Strings(keys)
			for _, mediaType := range keys {
				asset := mediaMap[mediaType]
				asset.Type = strings.ToLower(mediaType)
				mediaCopy = append(mediaCopy, asset)
			}
		}
		entries[md5sum] = model.Entry{
			Name:  cleanedName,
			Desc:  cleanedDesc,
			Size:  info.Size(),
			Media: mediaCopy,
		}
	}

	return entries, nil
}

func (c *ScanCommand) findMediaDir(categoryPath string, gameDef metadata.Game) string {
	if len(gameDef.Files) == 0 {
		return ""
	}
	first := gameDef.Files[0]
	base := strings.TrimSuffix(first, filepath.Ext(first))
	return filepath.Join(categoryPath, "media", base)
}

func (c *ScanCommand) collectMedia(ctx context.Context, store storage.Client, categoryPath, defaultDir string, gameDef metadata.Game) (map[string]model.MediaEntry, error) {
	result := make(map[string]model.MediaEntry)
	for mediaType, baseName := range mediaCandidates {
		path, err := c.pickMediaSource(ctx, categoryPath, defaultDir, gameDef, mediaType, baseName)
		if err != nil {
			return nil, err
		}
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat media file %s: %w", path, err)
		}
		md5sum, err := fileMD5(path)
		if err != nil {
			return nil, err
		}
		ext := strings.ToLower(filepath.Ext(path))
		key := fmt.Sprintf("%s%s", md5sum, ext)
		contentType := mime.TypeByExtension(ext)
		if err := store.UploadFile(ctx, key, path, contentType); err != nil {
			return nil, err
		}
		result[mediaType] = model.MediaEntry{
			Hash: md5sum,
			Ext:  ext,
			Size: info.Size(),
		}
	}
	return result, nil
}

func (c *ScanCommand) pickMediaSource(ctx context.Context, categoryPath, defaultDir string, gameDef metadata.Game, mediaType, baseName string) (string, error) {
	if candidate := c.assetPathFromMetadata(categoryPath, gameDef, mediaType); candidate != "" {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		logutil.GetLogger(ctx).Warn("asset path missing, fallback to prefix",
			zap.String("media_type", mediaType),
			zap.String("path", candidate),
		)
	}
	if defaultDir == "" {
		return "", nil
	}
	return firstFileWithPrefix(defaultDir, baseName)
}

func (c *ScanCommand) assetPathFromMetadata(categoryPath string, gameDef metadata.Game, mediaType string) string {
	if len(gameDef.Assets) == 0 {
		return ""
	}
	aliases := mediaAssetAliases[mediaType]
	for _, alias := range aliases {
		if val, ok := gameDef.Assets[alias]; ok {
			trimmed := strings.TrimSpace(val)
			if trimmed == "" {
				continue
			}
			candidate := filepath.FromSlash(trimmed)
			if filepath.IsAbs(candidate) {
				return candidate
			}
			return filepath.Join(categoryPath, candidate)
		}
	}
	return ""
}

func firstFileWithPrefix(dir, prefix string) (string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read media dir %s: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			return filepath.Join(dir, name), nil
		}
	}
	return "", nil
}

func init() {
	RegisterRunner("scan", func() IRunner { return NewScanCommand() })
}
