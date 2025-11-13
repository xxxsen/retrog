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

type ScanUnlinkCommand struct {
	rootDir string
	output  string
	ignore  string

	ignoreExt map[string]struct{}
}

func NewScanUnlinkCommand() *ScanUnlinkCommand {
	return &ScanUnlinkCommand{}
}

func (c *ScanUnlinkCommand) Name() string { return "scan-unlink" }

func (c *ScanUnlinkCommand) Desc() string {
	return "扫描 gamelist.xml 并输出未引用的 ROM 文件列表"
}

func (c *ScanUnlinkCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.rootDir, "dir", "", "ROM 根目录")
	f.StringVar(&c.output, "output", "", "输出 JSON 文件路径")
	f.StringVar(&c.ignore, "ignore-ext", "", "忽略的 ROM 扩展名，逗号分隔，例如 .txt,.nfo")
}

func (c *ScanUnlinkCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.rootDir) == "" {
		return errors.New("scan-unlink requires --dir")
	}
	if strings.TrimSpace(c.output) == "" {
		return errors.New("scan-unlink requires --output")
	}
	c.ignoreExt = parseIgnoreExt(c.ignore)
	logutil.GetLogger(ctx).Info("starting scan-unlink",
		zap.String("dir", c.rootDir),
		zap.String("output", c.output),
		zap.Strings("ignore_ext", mapKeys(c.ignoreExt)),
	)
	return nil
}

func (c *ScanUnlinkCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)
	results := make([]model.UnlinkLocation, 0)
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
		if _, seen := processed[cleanDir]; seen {
			return nil
		}

		logger.Info("processing gamelist", zap.String("path", filepath.ToSlash(path)))
		res, err := c.collectUnlinked(ctx, cleanDir, path)
		if err != nil {
			return err
		}
		if len(res.Files) > 0 {
			results = append(results, res)
		}
		processed[cleanDir] = struct{}{}
		return nil
	})
	if err != nil {
		return err
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Count == results[j].Count {
			return results[i].Location < results[j].Location
		}
		return results[i].Count > results[j].Count
	})

	output := model.UnlinkReport{
		Count:  len(results),
		Unlink: results,
	}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal output: %w", err)
	}
	if err := os.WriteFile(c.output, data, 0o644); err != nil {
		return fmt.Errorf("write output %s: %w", c.output, err)
	}

	logger.Info("scan-unlink completed",
		zap.Int("locations", len(results)),
		zap.String("output", c.output),
	)
	return nil
}

func (c *ScanUnlinkCommand) PostRun(ctx context.Context) error { return nil }

func (c *ScanUnlinkCommand) collectUnlinked(ctx context.Context, dir, gamelistPath string) (model.UnlinkLocation, error) {
	result := model.UnlinkLocation{Location: filepath.ToSlash(dir)}

	doc, err := metadata.ParseGamelistFile(gamelistPath)
	if err != nil {
		return result, fmt.Errorf("parse gamelist %s: %w", gamelistPath, err)
	}

	linked := make(map[string]struct{})
	for _, game := range doc.Games {
		if resolved := resolveResourcePath(dir, game.Path); resolved != "" {
			linked[filepath.Clean(resolved)] = struct{}{}
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return result, fmt.Errorf("read dir %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(entry.Name(), constant.DefaultGamelistFile) {
			continue
		}
		full := filepath.Join(dir, entry.Name())
		if c.shouldIgnore(entry.Name()) {
			continue
		}
		if _, ok := linked[filepath.Clean(full)]; ok {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return result, fmt.Errorf("stat file %s: %w", full, err)
		}
		hash, err := readFileMD5WithCache(ctx, full)
		if err != nil {
			return result, fmt.Errorf("hash file %s: %w", full, err)
		}

		result.Files = append(result.Files, model.UnlinkFile{
			Name: entry.Name(),
			Size: info.Size(),
			Hash: hash,
		})
	}

	result.Count = len(result.Files)
	result.Total = len(entries) - 1
	if result.Total < 0 {
		result.Total = 0
	}

	return result, nil
}

func resolveResourcePath(baseDir, value string) string {
	val := strings.TrimSpace(value)
	if val == "" {
		return ""
	}
	if filepath.IsAbs(val) {
		return filepath.Clean(val)
	}
	val = strings.TrimPrefix(val, "./")
	val = strings.TrimPrefix(val, ".\\")
	val = filepath.Clean(filepath.FromSlash(val))
	return filepath.Join(baseDir, val)
}

func init() {
	RegisterRunner("scan-unlink", func() IRunner { return NewScanUnlinkCommand() })
}

func parseIgnoreExt(raw string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, item := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		trimmed = strings.ToLower(trimmed)
		if !strings.HasPrefix(trimmed, ".") {
			trimmed = "." + trimmed
		}
		result[trimmed] = struct{}{}
	}
	return result
}

func mapKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (c *ScanUnlinkCommand) shouldIgnore(name string) bool {
	if len(c.ignoreExt) == 0 {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return false
	}
	_, ok := c.ignoreExt[ext]
	return ok
}
