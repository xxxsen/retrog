package app

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	kind     string
	filePath string
	dirPath  string
	exts     string
}

type romDefinition struct {
	Name   string
	Parent string
	Roms   []dat.Rom
}

func NewRomTestCommand() *RomTestCommand {
	return &RomTestCommand{}
}

func (c *RomTestCommand) Name() string { return "rom-test" }

func (c *RomTestCommand) Desc() string { return "检查压缩包中的 ROM 是否符合 DAT 定义" }

func (c *RomTestCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.datPath, "dat", "", "DAT 文件路径")
	f.StringVar(&c.kind, "kind", "fbneo", "DAT 类型，可选 fbneo, mame")
	f.StringVar(&c.filePath, "file", "", "待验证的压缩包文件路径")
	f.StringVar(&c.dirPath, "dir", "", "待验证的压缩包目录，递归扫描")
	f.StringVar(&c.exts, "ext", "zip,7z", "扫描扩展名，逗号分隔，例如 zip,7z")
}

func (c *RomTestCommand) PreRun(ctx context.Context) error {
	kind := strings.ToLower(strings.TrimSpace(c.kind))
	if kind == "" {
		return errors.New("rom-test requires --kind")
	}
	if kind != "fbneo" && kind != "mame" {
		return fmt.Errorf("unsupported kind: %s", kind)
	}
	if strings.TrimSpace(c.filePath) == "" && strings.TrimSpace(c.dirPath) == "" {
		return errors.New("rom-test requires --file or --dir")
	}
	if strings.TrimSpace(c.datPath) == "" {
		return errors.New("rom-test requires --dat")
	}
	if _, err := normalizeExts(c.exts); err != nil {
		return err
	}
	logutil.GetLogger(ctx).Info("starting rom-test",
		zap.String("dat", c.datPath),
		zap.String("kind", c.kind),
		zap.String("file", c.filePath),
		zap.String("dir", c.dirPath),
		zap.String("exts", c.exts),
	)
	return nil
}

func (c *RomTestCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)

	lookup, err := c.loadRomDefinitions()
	if err != nil {
		return err
	}

	targets, err := c.collectTargets()
	if err != nil {
		return err
	}

	nameToPath := make(map[string]string, len(targets))
	for _, t := range targets {
		nameToPath[strings.ToLower(deriveGameName(t))] = t
	}

	failCount := 0
	for _, target := range targets {
		issues, parentPath := c.validateFile(lookup, nameToPath, target)
		label := target
		if strings.TrimSpace(parentPath) != "" {
			label = fmt.Sprintf("%s(parent: %s)", target, parentPath)
		}
		if len(issues) == 0 {
			fmt.Printf("%s -- test succ\n", label)
			continue
		}
		failCount++
		fmt.Printf("%s -- test fail\n", label)
		for _, issue := range issues {
			fmt.Printf("- %s\n", issue)
		}
	}
	if failCount > 0 {
		return fmt.Errorf("rom check failed for %d file(s)", failCount)
	}

	logger.Info("rom check passed",
		zap.Int("file_count", len(targets)),
	)
	return nil
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
		displayName := romFileName(rom)
		keyFull := strings.ToLower(displayName)
		if f, ok := fullIndex[keyFull]; ok {
			issues = append(issues, checkRomFile(displayName, rom, f)...)
			continue
		}

		candidates := baseIndex[keyFull]
		if len(candidates) == 0 {
			issues = append(issues, fmt.Sprintf("missing rom: %s", displayName))
			continue
		}

		matched := false
		for _, f := range candidates {
			if len(checkRomFile(displayName, rom, f)) == 0 {
				matched = true
				break
			}
		}
		if !matched {
			issues = append(issues, fmt.Sprintf("no candidate matched rom %s (candidates: %d)", displayName, len(candidates)))
		}
	}
	return issues
}

func checkRomFile(displayName string, rom dat.Rom, f *zip.File) []string {
	var issues []string
	if rom.Size > 0 {
		size := int64(f.UncompressedSize64)
		if size != rom.Size {
			issues = append(issues, fmt.Sprintf("size mismatch for %s: expected %d, got %d", displayName, rom.Size, size))
		}
	}
	if rom.CRC != "" {
		crc := fmt.Sprintf("%08x", f.CRC32)
		if !strings.EqualFold(crc, rom.CRC) {
			issues = append(issues, fmt.Sprintf("crc mismatch for %s: expected %s, got %s", displayName, rom.CRC, crc))
		}
	}
	return issues
}

func (c *RomTestCommand) loadRomDefinitions() (map[string]romDefinition, error) {
	kind := strings.ToLower(strings.TrimSpace(c.kind))
	result := make(map[string]romDefinition)
	switch kind {
	case "fbneo":
		parser := dat.NewParser()
		df, err := parser.ParseFile(c.datPath)
		if err != nil {
			return nil, err
		}
		for _, game := range df.Games {
			result[game.Name] = romDefinition{Name: game.Name, Parent: strings.TrimSpace(game.CloneOf), Roms: game.Roms}
		}
	case "mame":
		parser := dat.NewMameParser()
		df, err := parser.ParseFile(c.datPath)
		if err != nil {
			return nil, err
		}
		for _, machine := range df.Machines {
			parent := strings.TrimSpace(machine.CloneOf)
			if parent == "" {
				parent = strings.TrimSpace(machine.RomOf)
			}
			result[machine.Name] = romDefinition{Name: machine.Name, Parent: parent, Roms: machine.Roms}
		}
	default:
		return nil, fmt.Errorf("unsupported kind: %s", c.kind)
	}
	return result, nil
}

func (c *RomTestCommand) collectTargets() ([]string, error) {
	allowed, err := normalizeExts(c.exts)
	if err != nil {
		return nil, err
	}
	var targets []string
	if strings.TrimSpace(c.filePath) != "" {
		path := filepath.Clean(c.filePath)
		if len(allowed) > 0 {
			ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
			if _, ok := allowed[ext]; !ok {
				return nil, fmt.Errorf("file %s skipped: extension not allowed", path)
			}
		}
		targets = append(targets, path)
	}
	if strings.TrimSpace(c.dirPath) != "" {
		err := filepath.WalkDir(c.dirPath, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if len(allowed) > 0 {
				ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
				if _, ok := allowed[ext]; !ok {
					return nil
				}
			}
			targets = append(targets, filepath.Clean(path))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if len(targets) == 0 {
		return nil, errors.New("no rom files found to test")
	}
	return targets, nil
}

func (c *RomTestCommand) validateFile(lookup map[string]romDefinition, nameToPath map[string]string, path string) ([]string, string) {
	gameName := deriveGameName(path)
	def, ok := lookup[gameName]
	if !ok {
		return []string{fmt.Sprintf("game %s not found in dat", gameName)}, ""
	}

	var closers []io.Closer
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()

	files, closer, err := openArchive(path)
	if err != nil {
		return []string{fmt.Sprintf("open archive %s: %v", path, err)}, ""
	}
	closers = append(closers, closer)
	allFiles := append([]*zip.File{}, files...)
	parentLabel := ""

	parent := strings.TrimSpace(def.Parent)
	if parent != "" {
		parentLabel = parent + ".zip"
		if parentPath, ok := nameToPath[strings.ToLower(parent)]; ok {
			parentLabel = parentPath
			pFiles, pCloser, err := openArchive(parentPath)
			if err != nil {
				return []string{fmt.Sprintf("open parent archive %s: %v", parent, err)}, parentLabel
			}
			closers = append(closers, pCloser)
			allFiles = append(allFiles, pFiles...)
		} else {
			return []string{fmt.Sprintf("parent rom missing: %s", parent)}, parentLabel
		}
	}

	return validateRomArchive(&dat.Game{Name: def.Name, Roms: def.Roms}, allFiles), parentLabel
}

func normalizeExts(exts string) (map[string]struct{}, error) {
	parts := strings.Split(exts, ",")
	result := make(map[string]struct{})
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		trimmed = strings.TrimPrefix(trimmed, ".")
		if trimmed == "" {
			continue
		}
		result[strings.ToLower(trimmed)] = struct{}{}
	}
	if len(result) == 0 {
		return nil, errors.New("invalid ext value")
	}
	return result, nil
}

func openArchive(path string) ([]*zip.File, io.Closer, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil, err
	}
	return zr.File, zr, nil
}

func romFileName(rom dat.Rom) string {
	if trimmed := strings.TrimSpace(rom.Merge); trimmed != "" {
		return trimmed
	}
	return rom.Name
}
