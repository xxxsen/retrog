package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	constant "github.com/xxxsen/retrog/internal/constant"
	appdb "github.com/xxxsen/retrog/internal/db"
	"github.com/xxxsen/retrog/internal/metadata"
	"github.com/xxxsen/retrog/internal/model"
	"github.com/xxxsen/retrog/internal/storage"
	"go.uber.org/zap"
)

type ScanUnlinkCommand struct {
	rootDir string
	ignore  string
	fix     bool
	replace bool

	ignoreExt map[string]struct{}
}

func NewScanUnlinkCommand() *ScanUnlinkCommand {
	return &ScanUnlinkCommand{}
}

func (c *ScanUnlinkCommand) Name() string { return "scan-unlink" }

func (c *ScanUnlinkCommand) Desc() string {
	return "扫描 gamelist.xml 并输出未引用的 ROM 文件列表"
}

func (c *ScanUnlinkCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.rootDir, "dir", "", "ROM 根目录")
	f.StringVar(&c.ignore, "ignore-ext", ".txt,.info,.xml,.fix,.cfg,.sh", "忽略的 ROM 扩展名，逗号分隔，例如 .txt,.nfo")
	f.BoolVar(&c.fix, "fix", false, "根据 meta.db 自动补充 gamelist.xml")
	f.BoolVar(&c.replace, "replace", false, "与 --fix 搭配使用，是否直接覆盖 gamelist.xml（默认输出 gamelist.xml.fix）")
}

func (c *ScanUnlinkCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.rootDir) == "" {
		return errors.New("scan-unlink requires --dir")
	}
	c.ignoreExt = parseIgnoreExt(c.ignore)
	logutil.GetLogger(ctx).Info("starting scan-unlink",
		zap.String("dir", c.rootDir),
		zap.Strings("ignore_ext", mapKeys(c.ignoreExt)),
		zap.Bool("fix", c.fix),
		zap.Bool("replace", c.replace),
	)
	return nil
}

func (c *ScanUnlinkCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)
	results := make([]model.UnlinkLocation, 0)
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
		if _, seen := processed[cleanDir]; seen {
			return nil
		}

		logger.Info("processing gamelist", zap.String("path", filepath.ToSlash(path)))
		res, err := c.collectUnlinked(ctx, cleanDir, path)
		if err != nil {
			return err
		}
		if len(res.Files) > 0 {
			results = append(results, res)
		}
		processed[cleanDir] = struct{}{}
		return nil
	})
	if err != nil {
		return err
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Count == results[j].Count {
			return results[i].Location < results[j].Location
		}
		return results[i].Count > results[j].Count
	})

	printUnlinkResults(results)

	if c.fix && len(results) > 0 {
		if err := c.fixGamelists(ctx, results); err != nil {
			return err
		}
	}

	logger.Info("scan-unlink completed",
		zap.Int("locations", len(results)),
		zap.Bool("fix_enabled", c.fix),
		zap.Bool("replace", c.replace),
	)
	return nil
}

func (c *ScanUnlinkCommand) PostRun(ctx context.Context) error { return nil }

func (c *ScanUnlinkCommand) collectUnlinked(ctx context.Context, dir, gamelistPath string) (model.UnlinkLocation, error) {
	result := model.UnlinkLocation{Location: filepath.ToSlash(dir)}

	doc, err := metadata.ParseGamelistFile(gamelistPath)
	if err != nil {
		return result, fmt.Errorf("parse gamelist %s: %w", gamelistPath, err)
	}

	linked := make(map[string]struct{})
	for _, game := range doc.Games {
		if resolved := resolveResourcePath(dir, game.Path); resolved != "" {
			linked[filepath.Clean(resolved)] = struct{}{}
		}
	}

	files, err := c.listManagedFiles(dir)
	if err != nil {
		return result, err
	}

	for _, file := range files {
		if _, ok := linked[filepath.Clean(file.absPath)]; ok {
			continue
		}
		info, err := os.Stat(file.absPath)
		if err != nil {
			return result, fmt.Errorf("stat file %s: %w", file.absPath, err)
		}
		hash, err := readFileMD5WithCache(ctx, file.absPath)
		if err != nil {
			return result, fmt.Errorf("hash file %s: %w", file.absPath, err)
		}

		result.Files = append(result.Files, model.UnlinkFile{
			Name: file.relPath,
			Size: info.Size(),
			Hash: hash,
		})
	}

	result.Count = len(result.Files)
	result.Total = len(files)

	return result, nil
}

type romCandidate struct {
	absPath string
	relPath string
}

func (c *ScanUnlinkCommand) listManagedFiles(dir string) ([]romCandidate, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	files := make([]romCandidate, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			if strings.EqualFold(name, constant.ModsDirName) {
				modsDir := filepath.Join(dir, name)
				if err := c.walkMods(dir, modsDir, &files); err != nil {
					return nil, fmt.Errorf("scan mods dir %s: %w", modsDir, err)
				}
			}
			continue
		}
		if strings.EqualFold(name, constant.DefaultGamelistFile) {
			continue
		}
		if c.shouldIgnore(name) {
			continue
		}
		files = append(files, romCandidate{
			absPath: filepath.Join(dir, name),
			relPath: filepath.ToSlash(name),
		})
	}

	sort.Slice(files, func(i, j int) bool { return files[i].relPath < files[j].relPath })
	return files, nil
}

func (c *ScanUnlinkCommand) walkMods(baseDir, modsDir string, files *[]romCandidate) error {
	return filepath.WalkDir(modsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), constant.DefaultGamelistFile) {
			return nil
		}
		if c.shouldIgnore(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(baseDir, path)
		if err != nil {
			return err
		}
		*files = append(*files, romCandidate{
			absPath: filepath.Clean(path),
			relPath: filepath.ToSlash(rel),
		})
		return nil
	})
}

func resolveResourcePath(baseDir, value string) string {
	val := strings.TrimSpace(value)
	if val == "" {
		return ""
	}
	if filepath.IsAbs(val) {
		return filepath.Clean(val)
	}
	val = strings.TrimPrefix(val, "./")
	val = strings.TrimPrefix(val, ".\\")
	val = filepath.Clean(filepath.FromSlash(val))
	return filepath.Join(baseDir, val)
}

func init() {
	RegisterRunner("scan-unlink", func() IRunner { return NewScanUnlinkCommand() })
}

func printUnlinkResults(results []model.UnlinkLocation) {
	if len(results) == 0 {
		fmt.Println("location: none")
		return
	}
	for _, res := range results {
		fmt.Printf("location: %s\n", res.Location)
		for _, file := range res.Files {
			fmt.Printf("- %s: not in gamelist\n", file.Name)
		}
		fmt.Println()
	}
}

func (c *ScanUnlinkCommand) fixGamelists(ctx context.Context, locations []model.UnlinkLocation) error {
	store := storage.DefaultClient()
	if store == nil {
		return errors.New("storage client not initialised")
	}

	hashSet := make(map[string]struct{})
	for _, loc := range locations {
		for _, file := range loc.Files {
			if hash := normalizeHash(file.Hash); hash != "" {
				hashSet[hash] = struct{}{}
			}
		}
	}
	if len(hashSet) == 0 {
		return nil
	}

	matchedEntries, err := fetchMatchedEntries(ctx, appdb.MetaDao, hashSet)
	if err != nil {
		return err
	}

	logger := logutil.GetLogger(ctx)
	for _, loc := range locations {
		dir := filepath.Clean(loc.Location)
		gamelistPath := filepath.Join(dir, constant.DefaultGamelistFile)
		if _, err := os.Stat(gamelistPath); err != nil {
			logger.Warn("gamelist missing, skip fix",
				zap.String("path", filepath.ToSlash(gamelistPath)),
				zap.Error(err),
			)
			continue
		}
		doc, err := metadata.ParseGamelistFile(gamelistPath)
		if err != nil {
			logger.Warn("parse gamelist failed, skip fix",
				zap.String("path", filepath.ToSlash(gamelistPath)),
				zap.Error(err),
			)
			continue
		}

		extraEntries := make([]metadata.GamelistEntry, 0, len(loc.Files))
		for _, file := range loc.Files {
			meta, ok := matchedEntries[normalizeHash(file.Hash)]
			if !ok {
				continue
			}
			entry, err := buildGamelistEntry(ctx, store, dir, file, meta)
			if err != nil {
				logger.Error("build gamelist entry failed",
					zap.String("location", dir),
					zap.String("rom", file.Name),
					zap.Error(err),
				)
				continue
			}
			extraEntries = append(extraEntries, *entry)
		}
		if len(extraEntries) == 0 {
			continue
		}

		destPath := gamelistPath
		if !c.replace {
			destPath = gamelistPath + ".fix"
		}

		updatedDoc := *doc
		updatedDoc.Games = append(updatedDoc.Games, extraEntries...)

		if err := metadata.WriteGamelistFile(destPath, &updatedDoc); err != nil {
			return err
		}
		logger.Info("gamelist updated",
			zap.String("path", filepath.ToSlash(destPath)),
			zap.Int("added", len(extraEntries)),
		)
	}
	return nil
}

func fetchMatchedEntries(ctx context.Context, dao *appdb.MetaDAO, hashSet map[string]struct{}) (map[string]model.MatchedMeta, error) {
	result := make(map[string]model.MatchedMeta, len(hashSet))
	if len(hashSet) == 0 {
		return result, nil
	}
	hashes := make([]string, 0, len(hashSet))
	for hash := range hashSet {
		hashes = append(hashes, hash)
	}

	const chunkSize = 200
	for start := 0; start < len(hashes); start += chunkSize {
		end := start + chunkSize
		if end > len(hashes) {
			end = len(hashes)
		}
		chunk := hashes[start:end]
		entries, _, err := dao.FetchStoredByHashes(ctx, chunk)
		if err != nil {
			return nil, err
		}
		for hash, stored := range entries {
			result[normalizeHash(hash)] = model.MatchedMeta{
				Entry: stored.Entry,
				ID:    stored.ID,
			}
		}
	}
	return result, nil
}

func buildGamelistEntry(ctx context.Context, store storage.Client, dir string, file model.UnlinkFile, meta model.MatchedMeta) (*metadata.GamelistEntry, error) {
	entry := meta.Entry
	name := entry.Name
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(file.Name)
	}

	pathValue := "./" + file.Name
	desc := strings.TrimSpace(entry.Desc)

	imagePath, err := downloadMedia(ctx, store, dir, constant.ImageDir, entry.Media, []string{"boxart", "boxfront", "screenshot"})
	if err != nil {
		return nil, err
	}
	logoPath, err := downloadMedia(ctx, store, dir, constant.ImageDir, entry.Media, []string{"logo"})
	if err != nil {
		return nil, err
	}
	videoPath, err := downloadMedia(ctx, store, dir, constant.VideoDir, entry.Media, []string{"video"})
	if err != nil {
		return nil, err
	}

	var release string
	if entry.ReleaseAt > 0 {
		release = time.Unix(entry.ReleaseAt, 0).UTC().Format("20060102T150405")
	}

	genres := make([]string, 0, len(entry.Genres))
	for _, g := range entry.Genres {
		if trimmed := strings.TrimSpace(g); trimmed != "" {
			genres = append(genres, trimmed)
		}
	}

	return &metadata.GamelistEntry{
		ID:          fmt.Sprintf("%d", meta.ID),
		Source:      "retrog",
		Path:        pathValue,
		Name:        name,
		Description: desc,
		Image:       imagePath,
		Marquee:     logoPath,
		Video:       videoPath,
		ReleaseDate: release,
		Developer:   entry.Developer,
		Publisher:   entry.Publisher,
		Genres:      genres,
		MD5:         normalizeHash(file.Hash),
	}, nil
}

func downloadMedia(ctx context.Context, store storage.Client, dir string, category string, mediaList []model.MediaEntry, preferred []string) (string, error) {
	target := pickMedia(mediaList, preferred)
	if target == nil {
		return "", nil
	}

	subdir := filepath.Join(category, constant.RetrogMediaSubdir)
	destDir := filepath.Join(dir, subdir)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create media dir %s: %w", destDir, err)
	}

	filename := target.Hash + target.Ext
	destPath := filepath.Join(destDir, filename)
	if _, err := os.Stat(destPath); errors.Is(err, os.ErrNotExist) {
		if err := store.DownloadToFile(ctx, target.Hash+target.Ext, destPath); err != nil {
			return "", fmt.Errorf("download media %s: %w", target.Hash+target.Ext, err)
		}
	} else if err != nil {
		return "", fmt.Errorf("stat media %s: %w", destPath, err)
	}

	rel := "./" + filepath.ToSlash(filepath.Join(subdir, filename))
	return rel, nil
}

func pickMedia(media []model.MediaEntry, preferred []string) *model.MediaEntry {
	for _, typ := range preferred {
		for _, item := range media {
			if strings.EqualFold(item.Type, typ) {
				copy := item
				return &copy
			}
		}
	}
	return nil
}

func normalizeHash(hash string) string {
	return strings.ToLower(strings.TrimSpace(hash))
}

func parseIgnoreExt(raw string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, item := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		trimmed = strings.ToLower(trimmed)
		if !strings.HasPrefix(trimmed, ".") {
			trimmed = "." + trimmed
		}
		result[trimmed] = struct{}{}
	}
	return result
}

func mapKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (c *ScanUnlinkCommand) shouldIgnore(name string) bool {
	if len(c.ignoreExt) == 0 {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return false
	}
	_, ok := c.ignoreExt[ext]
	return ok
}
