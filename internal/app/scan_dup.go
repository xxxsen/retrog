package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"github.com/xxxsen/retrog/internal/constant"
	"github.com/xxxsen/retrog/internal/metadata"
	"go.uber.org/zap"
)

type ScanDupCommand struct {
	dir string
	fix bool
}

func NewScanDupCommand() *ScanDupCommand { return &ScanDupCommand{} }

func (c *ScanDupCommand) Name() string { return "scan-dup" }

func (c *ScanDupCommand) Desc() string {
	return "扫描 gamelist.xml，检测并去重重复的 ROM 配置"
}

func (c *ScanDupCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.dir, "dir", "", "ROM 根目录")
	f.BoolVar(&c.fix, "fix", false, "是否自动去重")
}

func (c *ScanDupCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.dir) == "" {
		return errors.New("scan-dup requires --dir")
	}
	logutil.GetLogger(ctx).Info("starting scan-dup",
		zap.String("dir", c.dir),
		zap.Bool("fix", c.fix),
	)
	return nil
}

func (c *ScanDupCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)
	err := filepath.WalkDir(c.dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(d.Name(), constant.DefaultGamelistFile) {
			return nil
		}

		if err := c.processGamelist(ctx, path); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	logger.Info("scan-dup completed", zap.Bool("fix", c.fix))
	return nil
}

func (c *ScanDupCommand) PostRun(ctx context.Context) error { return nil }

func (c *ScanDupCommand) processGamelist(ctx context.Context, gamelistPath string) error {
	logger := logutil.GetLogger(ctx)
	doc, err := metadata.ParseGamelistFile(gamelistPath)
	if err != nil {
		return fmt.Errorf("parse gamelist %s: %w", gamelistPath, err)
	}

	seen := make(map[string]bool)
	duplicates := make([]metadata.GamelistEntry, 0)
	cleaned := make([]metadata.GamelistEntry, 0, len(doc.Games))
	for _, game := range doc.Games {
		pathValue := normalizeGamePath(game.Path)
		if pathValue == "" {
			cleaned = append(cleaned, game)
			continue
		}
		if seen[pathValue] {
			duplicates = append(duplicates, game)
			continue
		}
		seen[pathValue] = true
		cleaned = append(cleaned, game)
	}

	if len(duplicates) == 0 {
		return nil
	}

	fmt.Printf("location: %s\n", filepath.ToSlash(filepath.Dir(gamelistPath)))
	for _, dup := range duplicates {
		fmt.Printf("- %s already exists in gamelist.xml\n", strings.TrimSpace(dup.Path))
	}
	fmt.Println()

	if !c.fix {
		return nil
	}

	doc.Games = cleaned
	if err := metadata.WriteGamelistFile(gamelistPath, doc); err != nil {
		return fmt.Errorf("rewrite gamelist %s: %w", gamelistPath, err)
	}
	logger.Info("duplicate entries removed", zap.String("path", filepath.ToSlash(gamelistPath)), zap.Int("removed", len(duplicates)))
	return nil
}

func normalizeGamePath(path string) string {
	val := strings.TrimSpace(path)
	if val == "" {
		return ""
	}
	return filepath.Clean(val)
}

func init() {
	RegisterRunner("scan-dup", func() IRunner { return NewScanDupCommand() })
}
