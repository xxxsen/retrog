package app

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/mozillazg/go-pinyin"
	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"github.com/xxxsen/retrog/internal/constant"
	"github.com/xxxsen/retrog/internal/metadata"
	"go.uber.org/zap"
)

var pinyinFirstLetterArgs = func() pinyin.Args {
	args := pinyin.NewArgs()
	args.Style = pinyin.FirstLetter
	return args
}()

type NormalizeCommand struct {
	dir     string
	replace bool
	dryRun  bool
}

func NewNormalizeCommand() *NormalizeCommand {
	return &NormalizeCommand{}
}

func (c *NormalizeCommand) Name() string { return "normalize" }

func (c *NormalizeCommand) Desc() string {
	return "扫描并标准化 gamelist.xml 文件"
}

func (c *NormalizeCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.dir, "dir", "", "ROM 根目录")
	f.BoolVar(&c.replace, "replace", false, "是否直接覆盖 gamelist.xml，默认写入 gamelist.xml.fix")
	f.BoolVar(&c.dryRun, "dryrun", false, "仅模拟执行，不写入任何文件")
}

func (c *NormalizeCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.dir) == "" {
		return errors.New("normalize requires --dir")
	}
	logutil.GetLogger(ctx).Info("starting normalize",
		zap.String("dir", c.dir),
		zap.Bool("replace", c.replace),
		zap.Bool("dryrun", c.dryRun),
	)
	return nil
}

func (c *NormalizeCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)
	processed := 0
	written := 0
	changedCount := 0

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

		changed := normalizeGameEntries(doc)
		if changed {
			changedCount++
		}

		dest := path
		if !c.replace {
			dest = path + ".fix"
		}

		processed++
		if c.dryRun {
			logger.Info("gamelist normalize (dryrun)",
				zap.String("src", filepath.ToSlash(path)),
				zap.String("dest", filepath.ToSlash(dest)),
				zap.Bool("changed", changed),
			)
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
			zap.Bool("changed", changed),
		)
		return nil
	})
	if err != nil {
		return err
	}

	logger.Info("normalize completed",
		zap.Int("gamelist_found", processed),
		zap.Int("gamelist_written", written),
		zap.Int("gamelist_changed", changedCount),
		zap.Bool("dry_run", c.dryRun),
	)
	return nil
}

func (c *NormalizeCommand) PostRun(ctx context.Context) error { return nil }

func init() {
	RegisterRunner("normalize", func() IRunner { return NewNormalizeCommand() })
}

func normalizeGameEntries(doc *metadata.GamelistDocument) bool {
	if doc == nil {
		return false
	}
	changed := false
	for i := range doc.Games {
		name, updated := normalizeGameName(doc.Games[i].Name)
		if updated && doc.Games[i].Name != name {
			doc.Games[i].Name = name
			changed = true
		}
	}
	return changed
}

func normalizeGameName(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		if value == trimmed {
			return value, false
		}
		return trimmed, true
	}
	if hasGameNamePrefix(trimmed) {
		return value, false
	}
	prefix := determineNamePrefix(trimmed)
	if prefix == "" {
		return trimmed, trimmed != value
	}
	normalized := prefix + " " + trimmed
	return normalized, normalized != value
}

func determineNamePrefix(name string) string {
	for _, r := range name {
		if unicode.IsSpace(r) {
			continue
		}
		if unicode.IsDigit(r) {
			return string(r)
		}
		if unicode.Is(unicode.Han, r) {
			if letter := hanPrefix(r); letter != "" {
				return letter
			}
			continue
		}
		if unicode.IsLetter(r) {
			return strings.ToUpper(string(r))
		}
		return strings.ToUpper(string(r))
	}
	return ""
}

func hanPrefix(r rune) string {
	result := pinyin.LazyPinyin(string(r), pinyinFirstLetterArgs)
	if len(result) == 0 {
		return ""
	}
	letter := result[0]
	if letter == "" {
		return ""
	}
	return strings.ToUpper(letter[:1])
}
