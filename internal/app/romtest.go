package app

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"github.com/xxxsen/retrog/internal/dat"
	"go.uber.org/zap"
)

// RomTestCommand validates a ROM archive against an fbneo DAT.
type RomTestCommand struct {
	datPath  string
	filePath string
}

func NewRomTestCommand() *RomTestCommand {
	return &RomTestCommand{}
}

func (c *RomTestCommand) Name() string { return "rom-test" }

func (c *RomTestCommand) Desc() string {
	return "检查压缩包中的 ROM 是否符合 fbneo.dat"
}

func (c *RomTestCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.datPath, "dat", "", "fbneo.dat 文件路径")
	f.StringVar(&c.filePath, "file", "", "待验证的压缩包文件路径")
}

func (c *RomTestCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.datPath) == "" {
		return errors.New("rom-test requires --dat")
	}
	if strings.TrimSpace(c.filePath) == "" {
		return errors.New("rom-test requires --file")
	}
	logutil.GetLogger(ctx).Info("starting rom-test",
		zap.String("dat", c.datPath),
		zap.String("file", c.filePath),
	)
	return nil
}

func (c *RomTestCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)

	parser := dat.NewParser()
	df, err := parser.ParseFile(c.datPath)
	if err != nil {
		return err
	}

	gameName := deriveGameName(c.filePath)
	game := df.FindGame(gameName)
	if game == nil {
		return fmt.Errorf("game %s not found in dat", gameName)
	}

	zr, err := zip.OpenReader(c.filePath)
	if err != nil {
		return fmt.Errorf("open archive %s: %w", c.filePath, err)
	}
	defer zr.Close()

	issues := validateRomArchive(game, zr.File)
	if len(issues) == 0 {
		logger.Info("rom check passed",
			zap.String("game", gameName),
			zap.Int("rom_count", len(game.Roms)),
			zap.String("file", c.filePath),
		)
		return nil
	}

	for _, issue := range issues {
		logger.Error("rom check failed", zap.String("issue", issue))
	}
	return fmt.Errorf("rom check found %d issue(s) for %s", len(issues), gameName)
}

func (c *RomTestCommand) PostRun(ctx context.Context) error { return nil }

func init() {
	RegisterRunner("rom-test", func() IRunner { return NewRomTestCommand() })
}

// deriveGameName extracts the game name from the archive filename.
func deriveGameName(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

// validateRomArchive compares archive contents against rom definitions.
func validateRomArchive(game *dat.Game, files []*zip.File) []string {
	if game == nil {
		return []string{"nil game reference"}
	}
	fullIndex := make(map[string]*zip.File, len(files))
	baseIndex := make(map[string][]*zip.File, len(files))
	for _, f := range files {
		lowerFull := strings.ToLower(f.Name)
		fullIndex[lowerFull] = f
		base := strings.ToLower(filepath.Base(f.Name))
		baseIndex[base] = append(baseIndex[base], f)
	}

	var issues []string
	for _, rom := range game.Roms {
		keyFull := strings.ToLower(rom.Name)
		if f, ok := fullIndex[keyFull]; ok {
			issues = append(issues, checkRomFile(rom, f)...)
			continue
		}

		candidates := baseIndex[keyFull]
		if len(candidates) == 0 {
			issues = append(issues, fmt.Sprintf("missing rom: %s", rom.Name))
			continue
		}

		matched := false
		for _, f := range candidates {
			if len(checkRomFile(rom, f)) == 0 {
				matched = true
				break
			}
		}
		if !matched {
			issues = append(issues, fmt.Sprintf("no candidate matched rom %s (candidates: %d)", rom.Name, len(candidates)))
		}
	}
	return issues
}

func checkRomFile(rom dat.Rom, f *zip.File) []string {
	var issues []string
	if rom.Size > 0 {
		size := int64(f.UncompressedSize64)
		if size != rom.Size {
			issues = append(issues, fmt.Sprintf("size mismatch for %s: expected %d, got %d", rom.Name, rom.Size, size))
		}
	}
	if rom.CRC != "" {
		crc := fmt.Sprintf("%08x", f.CRC32)
		if !strings.EqualFold(crc, rom.CRC) {
			issues = append(issues, fmt.Sprintf("crc mismatch for %s: expected %s, got %s", rom.Name, rom.CRC, crc))
		}
	}
	return issues
}
