package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

// NormalizeCommand ensures ROM files are wrapped in matching directories.
type NormalizeCommand struct {
	platformDir string
}

func (c *NormalizeCommand) Name() string { return "normalize" }

func (c *NormalizeCommand) Desc() string {
	return "正规化平台目录结构，将游戏文件移动到同名目录中"
}

func NewNormalizeCommand() *NormalizeCommand { return &NormalizeCommand{} }

func (c *NormalizeCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.platformDir, "dir", "", "平台目录，例如 ./roms/gba")
}

func (c *NormalizeCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.platformDir) == "" {
		return errors.New("normalize requires --dir")
	}
	logutil.GetLogger(ctx).Info("starting normalize",
		zap.String("dir", c.platformDir),
	)
	return nil
}

func (c *NormalizeCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)
	entries, err := os.ReadDir(c.platformDir)
	if err != nil {
		return fmt.Errorf("read platform dir %s: %w", c.platformDir, err)
	}

	processed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		base := strings.TrimSuffix(name, filepath.Ext(name))
		src := filepath.Join(c.platformDir, name)
		dstDir := filepath.Join(c.platformDir, base)
		dst := filepath.Join(dstDir, name)

		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", dstDir, err)
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("move %s -> %s: %w", src, dst, err)
		}
		logger.Info("normalized rom",
			zap.String("file", name),
			zap.String("target_dir", filepath.ToSlash(dstDir)),
		)
		processed++
	}

	logger.Info("normalize completed",
		zap.Int("moved", processed),
	)
	return nil
}

func (c *NormalizeCommand) PostRun(ctx context.Context) error { return nil }

func init() {
	RegisterRunner("normalize", func() IRunner { return NewNormalizeCommand() })
}
