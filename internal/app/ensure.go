package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"retrog/internal/storage"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

type EnsureCommand struct {
	metaPath  string
	targetDir string
	hashList  string
	mediaList string

	hashes      []string
	mediaFilter map[string]struct{}
}

func (c *EnsureCommand) Name() string { return "ensure" }

func (c *EnsureCommand) Desc() string {
	return "根据 ROM 哈希下载对应的媒体资源"
}

func NewEnsureCommand() *EnsureCommand { return &EnsureCommand{} }

func (c *EnsureCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.metaPath, "meta", "", "upload 命令生成的 meta.json")
	f.StringVar(&c.targetDir, "dir", "", "下载媒体写入的目标目录（必须为空或不存在）")
	f.StringVar(&c.hashList, "hash", "", "逗号分隔的 ROM 哈希列表")
	f.StringVar(&c.mediaList, "media", "", "限定下载的媒体类型，逗号分隔；为空表示全部")
}

func (c *EnsureCommand) PreRun(ctx context.Context) error {
	if c.metaPath == "" || c.targetDir == "" || c.hashList == "" {
		return errors.New("ensure requires --meta, --dir, and --hash")
	}

	hashes := strings.Split(c.hashList, ",")
	for _, h := range hashes {
		trimmed := strings.TrimSpace(h)
		if trimmed != "" {
			c.hashes = append(c.hashes, trimmed)
		}
	}
	if len(c.hashes) == 0 {
		return errors.New("no valid hashes provided")
	}

	if strings.TrimSpace(c.mediaList) != "" {
		c.mediaFilter = make(map[string]struct{})
		medias := strings.Split(c.mediaList, ",")
		for _, m := range medias {
			trimmed := strings.ToLower(strings.TrimSpace(m))
			if trimmed != "" {
				c.mediaFilter[trimmed] = struct{}{}
			}
		}
	}

	logutil.GetLogger(ctx).Info("starting ensure",
		zap.String("meta", c.metaPath),
		zap.String("dir", c.targetDir),
		zap.Strings("hashes", c.hashes),
	)

	return nil
}

func (c *EnsureCommand) Run(ctx context.Context) error {
	if err := ensureCleanDir(c.targetDir); err != nil {
		return err
	}

	store := storage.DefaultClient()
	if store == nil {
		return errors.New("storage client not initialised")
	}

	meta, err := loadMeta(c.metaPath)
	if err != nil {
		return err
	}

	logger := logutil.GetLogger(ctx)

	for _, hash := range c.hashes {
		entry, ok := meta[hash]
		if !ok {
			logger.Warn("hash not found in meta", zap.String("hash", hash))
			continue
		}

		mediaSet := entry.Media
		if len(mediaSet) == 0 {
			logger.Warn("no media recorded", zap.String("hash", hash))
			continue
		}

		entryDir := filepath.Join(c.targetDir, hash)
		if err := os.MkdirAll(entryDir, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", entryDir, err)
		}

		downloaded := 0
		for _, asset := range mediaSet {
			mediaType := strings.ToLower(strings.TrimSpace(asset.Type))
			if len(c.mediaFilter) > 0 {
				if _, ok := c.mediaFilter[mediaType]; !ok {
					continue
				}
			}
			if asset.Hash == "" {
				continue
			}
			ext := asset.Ext
			if ext == "" {
				ext = ""
			}
			key := asset.Hash + ext
			destName := mediaType
			if destName == "" {
				destName = asset.Hash
			}
			dest := filepath.Join(entryDir, destName+ext)
			if err := store.DownloadToFile(ctx, key, dest); err != nil {
				return err
			}
			downloaded++
		}

		if downloaded == 0 {
			logger.Warn("no media downloaded for hash", zap.String("hash", hash))
		}
	}

	return nil
}

func (c *EnsureCommand) PostRun(ctx context.Context) error {
	logutil.GetLogger(ctx).Info("ensure completed", zap.String("dir", c.targetDir))
	return nil
}

func ensureCleanDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.MkdirAll(path, 0o755)
		}
		return fmt.Errorf("stat dir %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("target %s is not a directory", path)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", path, err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("target dir %s is not empty", path)
	}
	return nil
}

func loadMeta(path string) (Meta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read meta file %s: %w", path, err)
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse meta json %s: %w", path, err)
	}
	return meta, nil
}

func init() {
	RegisterRunner("ensure", func() IRunner { return NewEnsureCommand() })
}
