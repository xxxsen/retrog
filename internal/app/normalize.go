package app

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
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
	unzip       bool
}

func (c *NormalizeCommand) Name() string { return "normalize" }

func (c *NormalizeCommand) Desc() string {
	return "正规化平台目录结构，将游戏文件移动到同名目录中"
}

func NewNormalizeCommand() *NormalizeCommand { return &NormalizeCommand{} }

func (c *NormalizeCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.platformDir, "dir", "", "平台目录，例如 ./roms/gba")
	f.BoolVar(&c.unzip, "unzip", false, "若 zip 仅包含单个文件，则解压并替换为同名 ROM")
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
		if err := c.maybeUnzip(ctx, dst); err != nil {
			return err
		}
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

func (c *NormalizeCommand) maybeUnzip(ctx context.Context, filePath string) error {
	if !c.unzip || !strings.EqualFold(filepath.Ext(filePath), ".zip") {
		return nil
	}

	logger := logutil.GetLogger(ctx)
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return fmt.Errorf("open zip %s: %w", filePath, err)
	}
	defer r.Close()

	var target *zip.File
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if target != nil {
			return nil
		}
		target = f
	}
	if target == nil {
		return nil
	}

	parentDir := filepath.Dir(filePath)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return fmt.Errorf("ensure dir %s: %w", parentDir, err)
	}

	baseName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	dstName := baseName + filepath.Ext(target.Name)
	dstPath := filepath.Join(parentDir, dstName)

	src, err := target.Open()
	if err != nil {
		return fmt.Errorf("open entry %s: %w", target.Name, err)
	}
	defer src.Close()

	tempPath := dstPath + ".tmp"
	out, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("create temp file %s: %w", tempPath, err)
	}

	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		os.Remove(tempPath)
		return fmt.Errorf("extract entry %s: %w", target.Name, err)
	}
	out.Close()

	if _, err := os.Stat(dstPath); err == nil {
		if err := os.Remove(dstPath); err != nil {
			os.Remove(tempPath)
			return fmt.Errorf("remove existing %s: %w", dstPath, err)
		}
	}

	if err := os.Rename(tempPath, dstPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("rename %s -> %s: %w", tempPath, dstPath, err)
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("remove zip %s: %w", filePath, err)
	}

	logger.Info("unzipped rom",
		zap.String("zip", filepath.Base(filePath)),
		zap.String("target", filepath.ToSlash(dstPath)),
	)
	return nil
}
