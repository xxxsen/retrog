package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"retrog/internal/metadata"
	"retrog/internal/storage"

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

type UploadCommand struct {
	romDir   string
	metaPath string
	cats     []string
}

func (c *UploadCommand) Name() string { return "upload" }

func (c *UploadCommand) Desc() string {
	return "上传媒体资源并输出以 ROM 哈希为键的元数据"
}

func NewUploadCommand() *UploadCommand { return &UploadCommand{} }

func (c *UploadCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.romDir, "dir", "", "ROM 根目录")
	f.StringVar(&c.metaPath, "meta", "", "生成的 meta.json 输出路径")
	f.StringSliceVar(&c.cats, "cat", nil, "要处理的子目录列表，逗号分隔；为空则全部上传")
}

func (c *UploadCommand) PreRun(ctx context.Context) error {
	if c.romDir == "" || c.metaPath == "" {
		return errors.New("upload requires --dir and --meta")
	}
	logutil.GetLogger(ctx).Info("starting upload",
		zap.String("dir", c.romDir),
		zap.String("meta", c.metaPath),
	)
	return nil
}

func (c *UploadCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)

	store := storage.DefaultClient()
	if store == nil {
		return errors.New("storage client not initialised")
	}

	meta, err := c.buildMeta(ctx, store)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta json: %w", err)
	}

	if err := os.WriteFile(c.metaPath, data, 0o644); err != nil {
		return fmt.Errorf("write meta file: %w", err)
	}

	logger.Info("upload run completed",
		zap.String("meta_path", c.metaPath),
		zap.Int("entries", len(meta)),
	)
	return nil
}

func (c *UploadCommand) PostRun(ctx context.Context) error { return nil }

func (c *UploadCommand) buildMeta(ctx context.Context, store storage.Client) (Meta, error) {
	logger := logutil.GetLogger(ctx)
	logger.Info("scanning rom root", zap.String("root", c.romDir))

	entries, err := os.ReadDir(c.romDir)
	if err != nil {
		return nil, fmt.Errorf("read rom root %s: %w", c.romDir, err)
	}

	allowed := make(map[string]struct{})
	if len(c.cats) > 0 {
		for _, name := range c.cats {
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				allowed[trimmed] = struct{}{}
			}
		}
	}

	meta := make(Meta)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[entry.Name()]; !ok {
				continue
			}
		}

		categoryPath := filepath.Join(c.romDir, entry.Name())
		records, err := c.processCategory(ctx, store, categoryPath)
		if err != nil {
			return nil, err
		}
		for hash, item := range records {
			meta[hash] = item
		}
		logger.Debug("category processed", zap.String("path", categoryPath), zap.Int("records", len(records)))
	}

	return meta, nil
}

func (c *UploadCommand) processCategory(ctx context.Context, store storage.Client, categoryPath string) (map[string]MetaEntry, error) {
	metaPath := filepath.Join(categoryPath, metadataFile)
	logger := logutil.GetLogger(ctx)
	logger.Debug("processing metadata", zap.String("path", metaPath))

	doc, err := metadata.Parse(metaPath)
	if err != nil {
		return nil, fmt.Errorf("parse metadata %s: %w", metaPath, err)
	}
	doc.Cat = filepath.Base(categoryPath)
	if len(doc.Games) == 0 {
		return map[string]MetaEntry{}, nil
	}

	result := make(map[string]MetaEntry)
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

func (c *UploadCommand) processGame(ctx context.Context, store storage.Client, categoryPath string, gameDef metadata.Game) (map[string]MetaEntry, error) {
	entries := make(map[string]MetaEntry)

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
		mediaCopy := make(map[string]string, len(mediaMap))
		for k, v := range mediaMap {
			mediaCopy[k] = v
		}
		entries[md5sum] = MetaEntry{
			Name:  cleanedName,
			Desc:  cleanedDesc,
			Media: mediaCopy,
		}
	}

	return entries, nil
}

func (c *UploadCommand) findMediaDir(categoryPath string, gameDef metadata.Game) string {
	if len(gameDef.Files) == 0 {
		return ""
	}
	first := gameDef.Files[0]
	base := strings.TrimSuffix(first, filepath.Ext(first))
	return filepath.Join(categoryPath, "media", base)
}

func (c *UploadCommand) collectMedia(ctx context.Context, store storage.Client, categoryPath, defaultDir string, gameDef metadata.Game) (map[string]string, error) {
	result := make(map[string]string)
	for mediaType, baseName := range mediaCandidates {
		path, err := c.pickMediaSource(categoryPath, defaultDir, gameDef, mediaType, baseName)
		if err != nil {
			return nil, err
		}
		if path == "" {
			continue
		}
		md5sum, err := fileMD5(path)
		if err != nil {
			return nil, err
		}
		ext := strings.ToLower(filepath.Ext(path))
		key := fmt.Sprintf("media/%s%s", md5sum, ext)
		contentType := mime.TypeByExtension(ext)
		if err := store.UploadFile(ctx, key, path, contentType); err != nil {
			return nil, err
		}
		result[mediaType] = strings.TrimPrefix(key, "media/")
	}
	return result, nil
}

func (c *UploadCommand) pickMediaSource(categoryPath, defaultDir string, gameDef metadata.Game, mediaType, baseName string) (string, error) {
	if candidate := c.assetPathFromMetadata(categoryPath, gameDef, mediaType); candidate != "" {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		logutil.GetLogger(context.Background()).Warn("asset path missing, fallback to prefix",
			zap.String("media_type", mediaType),
			zap.String("path", candidate),
		)
	}
	if defaultDir == "" {
		return "", nil
	}
	return firstFileWithPrefix(defaultDir, baseName)
}

func (c *UploadCommand) assetPathFromMetadata(categoryPath string, gameDef metadata.Game, mediaType string) string {
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
	RegisterRunner("upload", func() IRunner { return NewUploadCommand() })
}
