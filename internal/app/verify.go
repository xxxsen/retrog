package app

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/xxxsen/common/logutil"
	"github.com/xxxsen/retrog/internal/constant"
	"github.com/xxxsen/retrog/internal/metadata"
	"github.com/xxxsen/retrog/internal/model"

	"github.com/spf13/pflag"
	"go.uber.org/zap"
)

type VerifyCommand struct {
	rootDir string
	output  string
	fix     bool
}

func NewVerifyCommand() *VerifyCommand {
	return &VerifyCommand{}
}

func (c *VerifyCommand) Name() string { return "verify" }

func (c *VerifyCommand) Desc() string {
	return "验证 ROM 与媒体文件是否与 gamelist 匹配"
}

func (c *VerifyCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.rootDir, "dir", "", "ROM 根目录")
	f.StringVar(&c.output, "output", "", "输出 JSON 文件路径")
	f.BoolVar(&c.fix, "fix", false, "自动移除缺失 ROM 的条目，并清空无效媒体字段")
}

func (c *VerifyCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.rootDir) == "" {
		return errors.New("verify requires --dir")
	}
	if strings.TrimSpace(c.output) == "" {
		return errors.New("verify requires --output")
	}
	logutil.GetLogger(ctx).Info("starting verify",
		zap.String("dir", c.rootDir),
		zap.String("output", c.output),
		zap.Bool("fix", c.fix),
	)
	return nil
}

func (c *VerifyCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)
	results := make([]model.VerifyLocation, 0)
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

		logger.Info("verifying gamelist",
			zap.String("path", filepath.ToSlash(path)),
		)
		locationResult, err := c.verifyGamelist(ctx, cleanDir, path)
		if err != nil {
			return err
		}
		if len(locationResult.List) > 0 {
			results = append(results, locationResult)
		}
		processed[cleanDir] = struct{}{}
		return nil
	})
	if err != nil {
		return err
	}

	output := model.VerifyOutput{CaseList: results}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal verify output: %w", err)
	}
	if err := os.WriteFile(c.output, data, 0o644); err != nil {
		return fmt.Errorf("write verify output %s: %w", c.output, err)
	}

	logger.Info("verify completed",
		zap.Int("locations", len(results)),
		zap.String("output", c.output),
	)
	return nil
}

func (c *VerifyCommand) PostRun(ctx context.Context) error { return nil }

func (c *VerifyCommand) verifyGamelist(ctx context.Context, baseDir, gamelistPath string) (model.VerifyLocation, error) {
	result := model.VerifyLocation{
		Location: filepath.ToSlash(baseDir),
		List:     []model.VerifyCase{},
	}

	doc, err := metadata.ParseGamelistFile(gamelistPath)
	if err != nil {
		return result, fmt.Errorf("parse gamelist %s: %w", gamelistPath, err)
	}

	newGames := make([]metadata.GamelistEntry, 0, len(doc.Games))
	changed := false

	for _, src := range doc.Games {
		game := src
		caseItem := model.VerifyCase{
			Rom:    strings.TrimSpace(game.Path),
			Reason: make([]string, 0),
		}

		romMissing := false
		gamePath := resolveResourcePath(baseDir, game.Path)
		if gamePath == "" || !fileExists(gamePath) {
			caseItem.Reason = append(caseItem.Reason, "rom missing")
			romMissing = true
		} else if isZipFile(gamePath) {
			if err := inspectZip(gamePath); err != nil {
				caseItem.Reason = append(caseItem.Reason, "zip read failed")
			}
		}

		if strings.TrimSpace(game.Name) == "" {
			caseItem.Reason = append(caseItem.Reason, "empty name")
		}
		if strings.TrimSpace(game.Description) == "" {
			caseItem.Reason = append(caseItem.Reason, "empty desc")
		}

		mediaFields := []struct {
			value *string
		}{
			{value: &game.Image},
			{value: &game.Video},
			{value: &game.Thumbnail},
			{value: &game.Marquee},
		}
		for _, field := range mediaFields {
			rel := strings.TrimSpace(*field.value)
			if rel == "" {
				continue
			}
			full := resolveResourcePath(baseDir, rel)
			if !fileExists(full) {
				caseItem.Reason = append(caseItem.Reason, "media missing:"+rel)
				if c.fix {
					if *field.value != "" {
						*field.value = ""
						changed = true
					}
				}
			}
		}

		if len(caseItem.Reason) > 0 {
			result.List = append(result.List, caseItem)
		}

		if romMissing && c.fix {
			changed = true
			continue
		}

		newGames = append(newGames, game)
	}

	if c.fix && changed {
		doc.Games = newGames
		if err := metadata.WriteGamelistFile(gamelistPath, doc); err != nil {
			return result, fmt.Errorf("update gamelist %s: %w", gamelistPath, err)
		}
		logutil.GetLogger(ctx).Info("verify fixed gamelist",
			zap.String("path", filepath.ToSlash(gamelistPath)),
		)
	}

	return result, nil
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	return true
}

func isZipFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".zip")
}

func inspectZip(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return err
	}
	if stat.IsDir() {
		return fmt.Errorf("zip path is directory")
	}

	_, err = zipReader(file)
	return err
}

func zipReader(f *os.File) (*zip.Reader, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return zip.NewReader(f, info.Size())
}

func init() {
	RegisterRunner("verify", func() IRunner { return NewVerifyCommand() })
}
