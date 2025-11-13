package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"github.com/xxxsen/retrog/internal/constant"
	"github.com/xxxsen/retrog/internal/metadata"
	"github.com/xxxsen/retrog/internal/model"
	"go.uber.org/zap"
)

type ScanUnlinkMediaCommand struct {
	rootDir string
	output  string
}

func NewScanUnlinkMediaCommand() *ScanUnlinkMediaCommand {
	return &ScanUnlinkMediaCommand{}
}

func (c *ScanUnlinkMediaCommand) Name() string { return "scan-unlink-media" }

func (c *ScanUnlinkMediaCommand) Desc() string {
	return "扫描 gamelist.xml 中未引用的媒体文件"
}

func (c *ScanUnlinkMediaCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.rootDir, "dir", "", "ROM 根目录")
	f.StringVar(&c.output, "output", "", "输出 JSON 文件路径")
}

func (c *ScanUnlinkMediaCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.rootDir) == "" {
		return errors.New("scan-unlink-media requires --dir")
	}
	if strings.TrimSpace(c.output) == "" {
		return errors.New("scan-unlink-media requires --output")
	}
	logutil.GetLogger(ctx).Info("starting scan-unlink-media",
		zap.String("dir", c.rootDir),
		zap.String("output", c.output),
	)
	return nil
}

func (c *ScanUnlinkMediaCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)
	results := make([]model.MediaLocation, 0)
	processed := make(map[string]struct{})

	err := filepath.WalkDir(c.rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(d.Name(), constant.DefaultGamelistFile) {
			return nil
		}

		dir := filepath.Dir(path)
		cleanDir := filepath.Clean(dir)
		if _, ok := processed[cleanDir]; ok {
			return nil
		}

		logger.Info("processing gamelist for media",
			zap.String("path", filepath.ToSlash(path)),
		)
		res, err := c.collectMediaUnlinked(cleanDir, path)
		if err != nil {
			return err
		}
		if len(res.Dirs) > 0 {
			results = append(results, res)
		}
		processed[cleanDir] = struct{}{}
		return nil
	})
	if err != nil {
		return err
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Location < results[j].Location
	})

	output := model.MediaReport{UnlinkMedia: results}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal output: %w", err)
	}
	if err := os.WriteFile(c.output, data, 0o644); err != nil {
		return fmt.Errorf("write output %s: %w", c.output, err)
	}

	logger.Info("scan-unlink-media completed",
		zap.Int("locations", len(results)),
		zap.String("output", c.output),
	)
	return nil
}

func (c *ScanUnlinkMediaCommand) PostRun(ctx context.Context) error { return nil }

func (c *ScanUnlinkMediaCommand) collectMediaUnlinked(baseDir, gamelistPath string) (model.MediaLocation, error) {
	result := model.MediaLocation{
		Location: filepath.ToSlash(baseDir),
		Dirs:     []model.MediaDir{},
	}

	doc, err := metadata.ParseGamelistFile(gamelistPath)
	if err != nil {
		return result, fmt.Errorf("parse gamelist %s: %w", gamelistPath, err)
	}

	referenced := make(map[string]struct{})
	mediaDirs := make(map[string]struct{})

	for _, game := range doc.Games {
		paths := []string{game.Image, game.Thumbnail, game.Marquee, game.Video}
		for _, rel := range paths {
			full := resolveResourcePath(baseDir, rel)
			if full == "" {
				continue
			}
			referenced[filepath.Clean(full)] = struct{}{}

			dir := filepath.Dir(full)
			if filepath.HasPrefix(dir, baseDir) {
				relDir, err := filepath.Rel(baseDir, dir)
				if err == nil && relDir != "." {
					mediaDirs[filepath.Clean(filepath.Join(baseDir, relDir))] = struct{}{}
				}
			}
		}
	}

	if len(mediaDirs) == 0 {
		return result, nil
	}

	dirResults := make([]model.MediaDir, 0)
	for dir := range mediaDirs {
		if _, err := os.Stat(dir); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return result, fmt.Errorf("stat media dir %s: %w", dir, err)
		}

		files, err := listUnlinkedFiles(dir, referenced)
		if err != nil {
			return result, err
		}
		if len(files) == 0 {
			continue
		}

		relDir, err := filepath.Rel(baseDir, dir)
		if err != nil {
			relDir = dir
		}
		dirResults = append(dirResults, model.MediaDir{
			Dir:   "./" + filepath.ToSlash(relDir),
			Count: len(files),
			Files: files,
		})
	}

	sort.Slice(dirResults, func(i, j int) bool {
		return dirResults[i].Dir < dirResults[j].Dir
	})
	result.Dirs = dirResults
	return result, nil
}

func listUnlinkedFiles(dir string, referenced map[string]struct{}) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	files := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		full := filepath.Clean(filepath.Join(dir, entry.Name()))
		if _, ok := referenced[full]; ok {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)
	return files, nil
}

func init() {
	RegisterRunner("scan-unlink-media", func() IRunner { return NewScanUnlinkMediaCommand() })
}
