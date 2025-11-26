package sdk

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bodgit/sevenzip"
	"github.com/xxxsen/retrog/internal/dat"
)

type archiveFile struct {
	Name  string
	Size  uint64
	CRC32 uint32
}

type romDefinition struct {
	Name   string
	Parent string
	Roms   []SubRomFile
}

type tester struct {
	defs map[string]romDefinition
}

// NewFBNeoTestSDK creates an SDK using an fbneo DAT file.
func NewFBNeoTestSDK(datfile string) (IRomTestSDK, error) {
	parser := dat.NewParser()
	df, err := parser.ParseFile(datfile)
	if err != nil {
		return nil, err
	}
	defs := make(map[string]romDefinition)
	for _, game := range df.Games {
		defs[game.Name] = romDefinition{
			Name:   game.Name,
			Parent: strings.TrimSpace(game.RomOf),
			Roms:   convertRoms(game.Roms),
		}
	}
	return &tester{defs: defs}, nil
}

// NewMameTestSDK creates an SDK using a mame DAT file.
func NewMameTestSDK(datfile string) (IRomTestSDK, error) {
	parser := dat.NewMameParser()
	df, err := parser.ParseFile(datfile)
	if err != nil {
		return nil, err
	}
	defs := make(map[string]romDefinition)
	for _, m := range df.Machines {
		parent := strings.TrimSpace(m.RomOf)
		defs[m.Name] = romDefinition{
			Name:   m.Name,
			Parent: parent,
			Roms:   convertRoms(m.Roms),
		}
	}
	return &tester{defs: defs}, nil
}

func convertRoms(roms []dat.Rom) []SubRomFile {
	var out []SubRomFile
	for _, r := range roms {
		out = append(out, SubRomFile{
			Name:      r.Name,
			MergeName: r.Merge,
			Size:      r.Size,
			CRC:       r.CRC,
			Optional:  isOptionalRomEntry(r),
		})
	}
	return out
}

// TestDir scans a directory and validates matching archives.
func (t *tester) TestDir(ctx Context, romdir string, biosdir string, exts []string) (*RomTestResult, error) {
	if romdir == "" {
		return nil, errors.New("romdir is required")
	}
	allowed := normalizeExts(exts)
	paths, err := collectPaths(romdir, allowed)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, errors.New("no rom files provided")
	}
	nameToPath := indexPaths(paths)
	// include bios directory files
	if biosdir != "" {
		biosPaths, _ := collectPaths(biosdir, allowed)
		for k, v := range indexPaths(biosPaths) {
			if _, ok := nameToPath[k]; !ok {
				nameToPath[k] = v
			}
		}
	}
	var results []*RomFileTestResult
	for _, p := range paths {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		result, err := t.testOne(p, biosdir, nameToPath)
		if err != nil {
			return nil, err
		}
		result.FilePath = p
		results = append(results, result)
	}
	return &RomTestResult{List: results}, nil
}

func (t *tester) testOne(path string, biosdir string, nameToPath map[string]string) (*RomFileTestResult, error) {
	romName := deriveGameName(path)
	def, ok := t.defs[romName]
	if !ok {
		return &RomFileTestResult{
			FilePath:   path,
			RomName:    romName,
			ParentList: nil,
			RedSubRomResultList: []*SubRomFileTestResult{
				{TestState: SubRomStateRed, TestMessage: fmt.Sprintf("game %s not found in dat", romName)},
			},
		}, nil
	}

	files, closer, err := openArchive(path)
	if err != nil {
		return nil, fmt.Errorf("open archive %s: %w", path, err)
	}
	defer closer.Close()

	aggregate := append([]archiveFile{}, files...)
	parentChain := buildParentChain(def, t.defs)
	parentInfos := make([]ParentInfo, 0, len(parentChain))
	for _, parent := range parentChain {
		parentPath := parent + ".zip"
		exist := true
		if actual, ok := nameToPath[strings.ToLower(parent)]; ok {
			parentPath = actual
		} else {
			exist = false
		}
		isBios := exist && isPathInDir(parentPath, biosdir)
		parentInfos = append(parentInfos, ParentInfo{Name: filepath.Base(parentPath), Exist: exist, IsBios: isBios})
		if exist {
			pFiles, pCloser, err := openArchive(parentPath)
			if err != nil {
				return nil, fmt.Errorf("open parent archive %s: %w", parentPath, err)
			}
			defer pCloser.Close()
			aggregate = append(aggregate, pFiles...)
		}
	}

	green, yellow, red := validateDefinition(def, aggregate)

	return &RomFileTestResult{
		FilePath:               path,
		RomName:                romName,
		ParentList:             parentInfos,
		GreenSubRomResultList:  green,
		YellowSubRomResultList: yellow,
		RedSubRomResultList:    red,
	}, nil
}

func normalizeExts(exts []string) map[string]struct{} {
	if len(exts) == 0 {
		return nil
	}
	out := make(map[string]struct{})
	for _, ext := range exts {
		trim := strings.TrimSpace(strings.TrimPrefix(ext, "."))
		if trim == "" {
			continue
		}
		out[strings.ToLower(trim)] = struct{}{}
	}
	return out
}

func collectPaths(root string, allowed map[string]struct{}) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if len(allowed) > 0 {
			ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(p)), ".")
			if _, ok := allowed[ext]; !ok {
				return nil
			}
		}
		paths = append(paths, filepath.Clean(p))
		return nil
	})
	return paths, err
}

func indexPaths(paths []string) map[string]string {
	out := make(map[string]string)
	for _, p := range paths {
		name := strings.ToLower(deriveGameName(p))
		if name != "" {
			out[name] = p
		}
	}
	return out
}

func deriveGameName(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

func openArchive(path string) ([]archiveFile, io.Closer, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".zip":
		zr, err := zip.OpenReader(path)
		if err != nil {
			return nil, nil, err
		}
		files := make([]archiveFile, 0, len(zr.File))
		for _, f := range zr.File {
			files = append(files, archiveFile{
				Name:  f.Name,
				Size:  f.UncompressedSize64,
				CRC32: f.CRC32,
			})
		}
		return files, zr, nil
	case ".7z":
		sr, err := sevenzip.OpenReader(path)
		if err != nil {
			return nil, nil, err
		}
		files := make([]archiveFile, 0, len(sr.File))
		for _, f := range sr.File {
			files = append(files, archiveFile{
				Name:  f.Name,
				Size:  f.UncompressedSize,
				CRC32: f.CRC32,
			})
		}
		return files, sr, nil
	default:
		return nil, nil, fmt.Errorf("unsupported archive format: %s", ext)
	}
}

func buildParentChain(def romDefinition, all map[string]romDefinition) []string {
	var chain []string
	seen := make(map[string]struct{})
	parent := strings.TrimSpace(def.Parent)
	for parent != "" {
		lower := strings.ToLower(parent)
		if _, ok := seen[lower]; ok {
			break
		}
		seen[lower] = struct{}{}
		chain = append(chain, parent)
		next, ok := all[parent]
		if !ok {
			break
		}
		parent = strings.TrimSpace(next.Parent)
	}
	return chain
}

func validateDefinition(def romDefinition, files []archiveFile) (greens, yellows, reds []*SubRomFileTestResult) {
	indexFull := make(map[string]archiveFile)
	indexBase := make(map[string][]archiveFile)
	indexCRC := make(map[string][]archiveFile)
	for _, f := range files {
		lower := strings.ToLower(f.Name)
		indexFull[lower] = f
		base := strings.ToLower(filepath.Base(f.Name))
		indexBase[base] = append(indexBase[base], f)
		crc := fmt.Sprintf("%08x", f.CRC32)
		indexCRC[strings.ToLower(crc)] = append(indexCRC[strings.ToLower(crc)], f)
	}

	for _, rom := range def.Roms {
		name := strings.ToLower(rom.NormalizedName())
		var result SubRomFileTestResult
		romCopy := rom
		result.SubRom = &romCopy

		if rom.Optional {
			// optional missing defaults to yellow unless matched
		}

		checkMatch := func(f archiveFile) (bool, bool) {
			sizeMatch := rom.Size == 0 || int64(f.Size) == rom.Size
			crcMatch := rom.CRC == "" || strings.EqualFold(fmt.Sprintf("%08x", f.CRC32), rom.CRC)
			if sizeMatch && crcMatch {
				result.TestState = SubRomStateGreen
				return true, false
			}
			if crcMatch || sizeMatch {
				result.TestState = SubRomStateYellow
				if !sizeMatch {
					result.TestMessage = fmt.Sprintf("size mismatch need %d got %d", rom.Size, f.Size)
				} else {
					result.TestMessage = fmt.Sprintf("crc mismatch need %s got %08x", rom.CRC, f.CRC32)
				}
				return true, crcMatch && !sizeMatch
			}
			return false, false
		}

		// full name match
		if f, ok := indexFull[name]; ok {
			matched, _ := checkMatch(f)
			if matched {
				if result.TestState == SubRomStateGreen {
					greens = append(greens, &result)
				} else {
					yellows = append(yellows, &result)
				}
				continue
			}
		}

		// base name match
		if candidates := indexBase[name]; len(candidates) > 0 {
			found := false
			for _, f := range candidates {
				if matched, _ := checkMatch(f); matched {
					found = true
					if result.TestState == SubRomStateGreen {
						greens = append(greens, &result)
					} else {
						yellows = append(yellows, &result)
					}
					break
				}
			}
			if found {
				continue
			}
		}

		// crc match
		if rom.CRC != "" {
			if candidates := indexCRC[strings.ToLower(rom.CRC)]; len(candidates) > 0 {
				for _, f := range candidates {
					if rom.Size > 0 && int64(f.Size) != rom.Size {
						continue
					}
					result.TestState = SubRomStateYellow
					result.TestMessage = fmt.Sprintf("name mismatch expected %s found %s", rom.NormalizedName(), f.Name)
					yellows = append(yellows, &result)
					goto nextRom
				}
			}
		}

		if rom.Optional {
			result.TestState = SubRomStateYellow
			result.TestMessage = "optional missing"
			yellows = append(yellows, &result)
		} else {
			result.TestState = SubRomStateRed
			result.TestMessage = fmt.Sprintf("missing rom: %s", rom.NormalizedName())
			reds = append(reds, &result)
		}
	nextRom:
	}
	return
}

func isOptionalRomEntry(r dat.Rom) bool {
	if strings.EqualFold(strings.TrimSpace(r.Status), "nodump") {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(r.Name))
	switch {
	case strings.HasSuffix(name, ".mcu"):
		return true
	case strings.HasPrefix(name, "pal"):
		return true
	case strings.HasPrefix(name, "gal"):
		return true
	case strings.HasSuffix(name, ".pld"):
		return true
	case strings.HasSuffix(name, ".prom"):
		return true
	case strings.HasPrefix(name, "i8751"):
		return true
	case strings.HasPrefix(name, "68705"):
		return true
	case strings.HasPrefix(name, "6805"):
		return true
	case strings.HasPrefix(name, "i80c51"):
		return true
	case strings.HasPrefix(name, "pic"):
		return true
	case strings.HasPrefix(name, "mcs51"):
		return true
	}
	if r.Size > 0 && r.Size < 512 {
		return true
	}
	return false
}

func isPathInDir(p, dir string) bool {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(p) == "" {
		return false
	}
	base, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	target, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	base = filepath.Clean(base)
	target = filepath.Clean(target)
	if target == base {
		return true
	}
	prefix := base + string(os.PathSeparator)
	return strings.HasPrefix(target, prefix)
}

// Ensure tester implements IRomTestSDK.
var _ IRomTestSDK = (*tester)(nil)

// Adapt standard context.Context to sdk.Context when needed.
type stdContextAdapter struct{ context.Context }

func (a stdContextAdapter) Done() <-chan struct{} { return a.Context.Done() }
func (a stdContextAdapter) Err() error            { return a.Context.Err() }
