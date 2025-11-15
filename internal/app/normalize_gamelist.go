package app

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"github.com/xxxsen/retrog/internal/constant"
	"github.com/xxxsen/retrog/internal/metadata"
	"go.uber.org/zap"
)

type NormalizeGamelistCommand struct {
	dir     string
	replace bool
	dryRun  bool
}

func NewNormalizeGamelistCommand() *NormalizeGamelistCommand {
	return &NormalizeGamelistCommand{}
}

func (c *NormalizeGamelistCommand) Name() string { return "normalize-gamelist" }

func (c *NormalizeGamelistCommand) Desc() string {
	return "扫描并标准化 gamelist.xml 文件"
}

func (c *NormalizeGamelistCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.dir, "dir", "", "ROM 根目录")
	f.BoolVar(&c.replace, "replace", false, "是否直接覆盖 gamelist.xml，默认写入 gamelist.xml.fix")
	f.BoolVar(&c.dryRun, "dryrun", false, "仅模拟执行，不写入任何文件")
}

func (c *NormalizeGamelistCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.dir) == "" {
		return errors.New("normalize-gamelist requires --dir")
	}
	logutil.GetLogger(ctx).Info("starting normalize-gamelist",
		zap.String("dir", c.dir),
		zap.Bool("replace", c.replace),
		zap.Bool("dryrun", c.dryRun),
	)
	return nil
}

func (c *NormalizeGamelistCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)
	processed := 0
	written := 0

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

		doc, err := metadata.ParseGamelistFile(path)
		if err != nil {
			return err
		}

		dest := path
		if !c.replace {
			dest = path + ".fix"
		}

		processed++
		if c.dryRun {
			logger.Info("gamelist normalize (dryrun)", zap.String("src", filepath.ToSlash(path)), zap.String("dest", filepath.ToSlash(dest)))
			return nil
		}

		if err := metadata.WriteGamelistFile(dest, doc); err != nil {
			return err
		}
		written++
		logger.Info("gamelist normalized",
			zap.String("src", filepath.ToSlash(path)),
			zap.String("dest", filepath.ToSlash(dest)),
			zap.Bool("replace", c.replace),
		)
		return nil
	})
	if err != nil {
		return err
	}

	logger.Info("normalize-gamelist completed",
		zap.Int("gamelist_found", processed),
		zap.Int("gamelist_written", written),
		zap.Bool("dry_run", c.dryRun),
	)
	return nil
}

func (c *NormalizeGamelistCommand) PostRun(ctx context.Context) error { return nil }

func init() {
	RegisterRunner("normalize-gamelist", func() IRunner { return NewNormalizeGamelistCommand() })
}
