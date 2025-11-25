package app

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
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
	biosDir  string
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
	f.StringVar(&c.biosDir, "bios", "", "BIOS 目录，用于补全 romof/clone 依赖")
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
		zap.String("bios_dir", c.biosDir),
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

	allowed, err := normalizeExts(c.exts)
	if err != nil {
		return err
	}

	targets, err := c.collectTargets(allowed)
	if err != nil {
		return err
	}

	nameToPath := make(map[string]string, len(targets))
	for _, t := range targets {
		nameToPath[strings.ToLower(deriveGameName(t))] = t
	}
	if biosMap, err := c.collectBiosPaths(allowed); err == nil {
		for name, path := range biosMap {
			if _, exists := nameToPath[name]; !exists {
				nameToPath[name] = path
			}
		}
	} else {
		logger.Warn("collect bios paths failed", zap.Error(err))
	}

	failCount := 0
	for _, target := range targets {
		issues, parentPath, parentChain, parentIsBios, skippedOptional, nameMismatch, parentMissing, biosCrcMismatch := c.validateFile(lookup, nameToPath, target)
		label := target
		if strings.TrimSpace(parentPath) != "" {
			var names []string
			for _, p := range parentChain {
				names = append(names, filepath.Base(p))
			}
			parentBase := strings.Join(names, " <- ")
			labelType := "parent"
			if parentIsBios {
				labelType = "bios"
			}
			label = fmt.Sprintf("%s(%s: %s)", target, labelType, parentBase)
		}
		if len(issues) == 0 {
			var tags []string
			if skippedOptional {
				tags = append(tags, "skip optional")
			}
			if nameMismatch {
				tags = append(tags, "name mismatch")
			}
			if parentMissing {
				tags = append(tags, "parent rom missing")
			}
			if biosCrcMismatch {
				tags = append(tags, "bios crc mismatch")
			}
			if len(tags) > 0 {
				fmt.Printf("%s -- test succ(%s)\n", label, strings.Join(tags, ", "))
			} else {
				fmt.Printf("%s -- test succ\n", label)
			}
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
func validateRomArchive(game *dat.Game, files []*zip.File) ([]string, bool, bool) {
	if game == nil {
		return []string{"nil game reference"}, false, false
	}
	fullIndex := make(map[string]*zip.File, len(files))
	baseIndex := make(map[string][]*zip.File, len(files))
	crcIndex := make(map[string][]*zip.File, len(files))
	for _, f := range files {
		lowerFull := strings.ToLower(f.Name)
		fullIndex[lowerFull] = f
		base := strings.ToLower(filepath.Base(f.Name))
		baseIndex[base] = append(baseIndex[base], f)
		crc := fmt.Sprintf("%08x", f.CRC32)
		crcIndex[strings.ToLower(crc)] = append(crcIndex[strings.ToLower(crc)], f)
	}

	var issues []string
	skippedOptional := false
	nameMismatch := false
	for _, rom := range game.Roms {
		if isNoDump(rom) {
			continue
		}
		displayName := romFileName(rom)
		optional := isOptionalRom(displayName) || (rom.Size > 0 && rom.Size < 512)
		keyFull := strings.ToLower(displayName)
		if f, ok := fullIndex[keyFull]; ok {
			mismatches := checkRomFile(displayName, rom, f)
			if len(mismatches) > 0 && optional {
				skippedOptional = true
				continue
			}
			issues = append(issues, mismatches...)
			continue
		}

		candidates := baseIndex[keyFull]
		if len(candidates) == 0 {
			handled := false
			if rom.CRC != "" {
				for _, f := range crcIndex[strings.ToLower(rom.CRC)] {
					if rom.Size > 0 && int64(f.UncompressedSize64) != rom.Size {
						continue
					}
					mismatches := checkRomFile(displayName, rom, f)
					if len(mismatches) == 0 {
						nameMismatch = true
						handled = true
						break
					}
				}
			}
			if handled {
				continue
			}
			if optional {
				skippedOptional = true
				continue
			}
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
			if optional {
				skippedOptional = true
				continue
			}
			issues = append(issues, fmt.Sprintf("no candidate matched rom %s (candidates: %d)", displayName, len(candidates)))
		}
	}
	return issues, skippedOptional, nameMismatch
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
			parent := strings.TrimSpace(game.RomOf)
			result[game.Name] = romDefinition{Name: game.Name, Parent: parent, Roms: game.Roms}
		}
	case "mame":
		parser := dat.NewMameParser()
		df, err := parser.ParseFile(c.datPath)
		if err != nil {
			return nil, err
		}
		for _, machine := range df.Machines {
			parent := strings.TrimSpace(machine.RomOf)
			result[machine.Name] = romDefinition{Name: machine.Name, Parent: parent, Roms: machine.Roms}
		}
	default:
		return nil, fmt.Errorf("unsupported kind: %s", c.kind)
	}
	return result, nil
}

func (c *RomTestCommand) collectTargets(allowed map[string]struct{}) ([]string, error) {
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

func (c *RomTestCommand) validateFile(lookup map[string]romDefinition, nameToPath map[string]string, path string) ([]string, string, []string, bool, bool, bool, bool, bool) {
	gameName := deriveGameName(path)
	def, ok := lookup[gameName]
	if !ok {
		return []string{fmt.Sprintf("game %s not found in dat", gameName)}, "", nil, false, false, false, false, false
	}

	var closers []io.Closer
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()

	files, closer, err := openArchive(path)
	if err != nil {
		return []string{fmt.Sprintf("open archive %s: %v", path, err)}, "", nil, false, false, false, false, false
	}
	closers = append(closers, closer)
	allFiles := append([]*zip.File{}, files...)
	parentLabel := ""
	parentChainPaths := []string{}
	parentIsBios := false
	skippedOptional := false
	nameMismatch := false
	parentMissing := false
	parentBiosCrcMismatch := false

	parentChain := c.parentChain(def, lookup)
	if len(parentChain) > 0 {
		parentLabel = parentChain[0] + ".zip"
	}
	for idx, parent := range parentChain {
		parentPath := parent + ".zip"
		if actualPath, ok := nameToPath[strings.ToLower(parent)]; ok {
			parentPath = actualPath
		} else {
			parentMissing = true
		}
		parentChainPaths = append(parentChainPaths, parentPath)
		if idx == 0 {
			parentLabel = parentPath
		}
		if c.isBiosPath(parentPath) {
			parentIsBios = true
		}
		if !parentMissing {
			pFiles, pCloser, err := openArchive(parentPath)
			if err != nil {
				return []string{fmt.Sprintf("open parent archive %s: %v", parent, err)}, parentLabel, parentChainPaths, parentIsBios, false, false, false, false
			}
			closers = append(closers, pCloser)
			allFiles = append(allFiles, pFiles...)
		}
	}

	issues, skipped, mismatched := validateRomArchive(&dat.Game{Name: def.Name, Roms: def.Roms}, allFiles)
	skippedOptional = skippedOptional || skipped
	nameMismatch = nameMismatch || mismatched
	biosCrcMismatch := false
	if parentIsBios && len(issues) > 0 && allCrcMismatch(issues) {
		biosCrcMismatch = true
		issues = nil
	}
	parentBiosCrcMismatch = biosCrcMismatch && parentIsBios
	return issues, parentLabel, parentChainPaths, parentIsBios, skippedOptional, nameMismatch, parentMissing, parentBiosCrcMismatch
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

func (c *RomTestCommand) collectBiosPaths(allowed map[string]struct{}) (map[string]string, error) {
	if strings.TrimSpace(c.biosDir) == "" {
		return map[string]string{}, nil
	}
	result := make(map[string]string)
	err := filepath.WalkDir(c.biosDir, func(path string, d fs.DirEntry, walkErr error) error {
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
		name := strings.ToLower(deriveGameName(path))
		if name != "" {
			result[name] = filepath.Clean(path)
		}
		return nil
	})
	return result, err
}

func isNoDump(rom dat.Rom) bool {
	return strings.EqualFold(strings.TrimSpace(rom.Status), "nodump")
}

func isOptionalRom(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasSuffix(lower, ".mcu"):
		return true
	case strings.HasPrefix(lower, "pal"):
		return true
	case strings.HasPrefix(lower, "gal"):
		return true
	case strings.HasSuffix(lower, ".pld"):
		return true
	case strings.HasSuffix(lower, ".prom"):
		return true
	case strings.HasPrefix(lower, "i8751"):
		return true
	case strings.HasPrefix(lower, "68705"):
		return true
	case strings.HasPrefix(lower, "6805"):
		return true
	case strings.HasPrefix(lower, "i80c51"):
		return true
	case strings.HasPrefix(lower, "pic"):
		return true
	case strings.HasPrefix(lower, "mcs51"):
		return true
	default:
		return false
	}
}

func (c *RomTestCommand) isBiosPath(p string) bool {
	if strings.TrimSpace(c.biosDir) == "" || strings.TrimSpace(p) == "" {
		return false
	}
	biosRoot, err := filepath.Abs(c.biosDir)
	if err != nil {
		return false
	}
	target, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	biosRoot = filepath.Clean(biosRoot)
	target = filepath.Clean(target)
	prefix := biosRoot + string(os.PathSeparator)
	return target == biosRoot || strings.HasPrefix(target, prefix)
}

func allCrcMismatch(issues []string) bool {
	for _, issue := range issues {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(issue)), "crc mismatch for ") {
			return false
		}
	}
	return len(issues) > 0
}

func (c *RomTestCommand) parentChain(def romDefinition, lookup map[string]romDefinition) []string {
	seen := make(map[string]struct{})
	var chain []string
	parent := strings.TrimSpace(def.Parent)
	for parent != "" {
		lower := strings.ToLower(parent)
		if _, exists := seen[lower]; exists {
			break
		}
		seen[lower] = struct{}{}
		chain = append(chain, parent)
		next, ok := lookup[parent]
		if !ok {
			break
		}
		parent = strings.TrimSpace(next.Parent)
	}
	return chain
}
