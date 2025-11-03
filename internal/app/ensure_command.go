package app

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"retrog/internal/storage"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

// EnsureOptions control what the Ensurer downloads.
type EnsureOptions struct {
	Category     string
	TargetDir    string
	IncludeROM   bool
	IncludeMedia bool
	Unzip        bool
}

// EnsureCommand encapsulates ensure execution state.
type EnsureCommand struct {
	metaPath      string
	category      string
	targetDir     string
	dataSelection string
	unzip         bool
	opts          EnsureOptions
	romBucket     string
	mediaBucket   string
}

// Name returns the command identifier.
func (c *EnsureCommand) Name() string { return "ensure" }

// NewEnsureCommand creates a new ensure command instance.
func NewEnsureCommand() *EnsureCommand {
	return &EnsureCommand{}
}

// Init registers CLI flags that affect the command.
func (c *EnsureCommand) Init(fst *pflag.FlagSet) {
	fst.StringVar(&c.metaPath, "meta", "", "Path to meta JSON produced by upload")
	fst.StringVar(&c.category, "cat", "", "Category name to download")
	fst.StringVar(&c.targetDir, "dir", "", "Destination directory to download into (must be empty or missing)")
	fst.StringVar(&c.dataSelection, "data", "", "Comma-separated data types to download (rom, media)")
	fst.BoolVar(&c.unzip, "unzip", false, "Unzip ROM archives that contain a single file")
}

// PreRun performs validation and object initialisation as needed.
func (c *EnsureCommand) PreRun(ctx context.Context) error {
	if c.metaPath == "" || c.category == "" || c.targetDir == "" {
		return errors.New("ensure requires --meta, --cat, and --dir")
	}

	includeROM, includeMedia, err := parseEnsureDataSelection(c.dataSelection)
	if err != nil {
		return err
	}

	c.opts = EnsureOptions{
		Category:     c.category,
		TargetDir:    c.targetDir,
		IncludeROM:   includeROM,
		IncludeMedia: includeMedia,
		Unzip:        c.unzip,
	}

	if _, err := storage.EnsureDefaultClient(ctx); err != nil {
		return err
	}

	cfg, ok := storage.DefaultS3Config()
	if !ok {
		return errors.New("default s3 configuration not initialised")
	}
	bucket := cfg.RomBucket
	if bucket == "" {
		bucket = cfg.MediaBucket
	}
	if bucket == "" {
		return errors.New("s3 bucket not configured")
	}
	c.romBucket = bucket
	c.mediaBucket = bucket

	logutil.GetLogger(ctx).Info("starting ensure",
		zap.String("meta", c.metaPath),
		zap.String("category", c.category),
		zap.String("dir", c.targetDir),
		zap.Bool("rom", c.opts.IncludeROM),
		zap.Bool("media", c.opts.IncludeMedia),
		zap.Bool("unzip", c.unzip),
	)

	return nil
}

// Run executes the ensure logic.
func (c *EnsureCommand) Run(ctx context.Context) error {
	store := storage.DefaultClient()
	if store == nil {
		return errors.New("storage client not initialised")
	}

	return c.ensure(ctx, store)
}

func (c *EnsureCommand) ensure(ctx context.Context, store storage.Client) error {
	opts := c.opts

	if err := ensureCleanDir(opts.TargetDir); err != nil {
		return err
	}

	if !opts.IncludeROM && !opts.IncludeMedia {
		opts.IncludeROM = true
		opts.IncludeMedia = true
		c.opts = opts
	}

	meta, err := loadMeta(c.metaPath)
	if err != nil {
		return err
	}

	cat, err := findCategory(meta, opts.Category)
	if err != nil {
		return err
	}

	catDir := filepath.Join(opts.TargetDir, sanitizeName(cat.CatName))
	if err := os.MkdirAll(catDir, 0o755); err != nil {
		return fmt.Errorf("create category dir %s: %w", catDir, err)
	}

	for _, game := range cat.GameList {
		if len(game.Files) == 0 {
			logutil.GetLogger(ctx).Warn("skip game due to empty files", zap.String("game", game.Name))
			continue
		}

		baseName := deriveGameDirName(game)
		gameDir := filepath.Join(catDir, baseName)
		if opts.IncludeROM || opts.IncludeMedia {
			if err := os.MkdirAll(gameDir, 0o755); err != nil {
				return fmt.Errorf("create game dir %s: %w", gameDir, err)
			}
		}

		if opts.IncludeROM {
			if err := c.downloadROMFiles(ctx, store, game, gameDir, opts.Unzip); err != nil {
				return err
			}
		}

		if opts.IncludeMedia {
			mediaDir := filepath.Join(gameDir, "media")
			if err := c.downloadMedia(ctx, store, mediaDir, game.Media); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *EnsureCommand) downloadROMFiles(ctx context.Context, store storage.Client, game Game, gameDir string, unzip bool) error {
	logger := logutil.GetLogger(ctx)
	for idx, file := range game.Files {
		key := fmt.Sprintf("rom/%s%s", file.Hash, file.Ext)
		destName := buildFileNameFromMeta(game, file, idx)
		destPath := filepath.Join(gameDir, destName)
		if err := store.DownloadToFile(ctx, c.romBucket, key, destPath); err != nil {
			return err
		}

		if file.Hash != "" {
			sum, err := fileMD5(destPath)
			if err != nil {
				return err
			}
			if sum != file.Hash {
				return fmt.Errorf("hash mismatch for %s (expected %s got %s)", destPath, file.Hash, sum)
			}
		}

		isZip := strings.EqualFold(file.Ext, ".zip")
		if unzip && isZip {
			if err := unzipSingleFile(destPath); err != nil {
				return err
			}
		}

		logger.Debug("downloaded rom file",
			zap.String("game", game.Name),
			zap.String("dest", destPath),
			zap.Bool("unzipped", unzip && isZip),
		)
	}
	return nil
}

func deriveGameDirName(game Game) string {
	name := game.Files[0].FileName
	if name != "" {
		trimmedExt := strings.TrimSuffix(name, filepath.Ext(name))
		trimmedExt = removePartSuffix(trimmedExt)
		if trimmedExt != "" {
			return trimmedExt
		}
	}
	cleaned := cleanGameName(game.Name)
	if cleaned == "" {
		cleaned = "unknown"
	}
	return cleaned
}

func buildFileNameFromMeta(game Game, file File, idx int) string {
	base := file.FileName
	if base == "" {
		base = deriveGameDirName(game)
	}

	if idx > 0 {
		baseName := removePartSuffix(strings.TrimSuffix(base, filepath.Ext(base)))
		if baseName == "" {
			baseName = "unknown"
		}
		return fmt.Sprintf("%s_part_%d%s", baseName, idx+1, file.Ext)
	}

	if base == "" {
		return deriveGameDirName(game) + file.Ext
	}

	return base
}

func removePartSuffix(name string) string {
	if name == "" {
		return name
	}
	partSuffix := regexp.MustCompile(`(?i)_part_\d+$`)
	return partSuffix.ReplaceAllString(name, "")
}

func unzipSingleFile(zipPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip %s: %w", zipPath, err)
	}
	defer r.Close()

	var target *zip.File
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		target = f
		break
	}
	if target == nil {
		return fmt.Errorf("zip %s contains no files", zipPath)
	}

	rc, err := target.Open()
	if err != nil {
		return fmt.Errorf("open zip entry %s: %w", target.Name, err)
	}
	defer rc.Close()

	extractedExt := strings.ToLower(filepath.Ext(target.Name))
	if extractedExt == "" {
		extractedExt = ".bin"
	}

	basePath := strings.TrimSuffix(zipPath, filepath.Ext(zipPath))
	destPath := basePath + extractedExt

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("ensure unzip dir %s: %w", destPath, err)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create unzip target %s: %w", destPath, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, rc); err != nil {
		return fmt.Errorf("write unzip target %s: %w", destPath, err)
	}

	if err := os.Remove(zipPath); err != nil {
		return fmt.Errorf("remove zip %s: %w", zipPath, err)
	}

	return nil
}

func (c *EnsureCommand) downloadMedia(ctx context.Context, store storage.Client, mediaDir string, media Media) error {
	type mediaItem struct {
		src  string
		name string
	}

	items := []mediaItem{
		{media.Boxart, "boxart"},
		{media.Screenshot, "screenshot"},
		{media.Video, "video"},
		{media.Logo, "logo"},
	}

	needed := false
	for _, item := range items {
		if item.src != "" {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}

	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return fmt.Errorf("create media dir %s: %w", mediaDir, err)
	}

	for _, item := range items {
		if item.src == "" {
			continue
		}
		dest := filepath.Join(mediaDir, item.name+filepath.Ext(item.src))
		if err := store.DownloadToFile(ctx, c.mediaBucket, item.src, dest); err != nil {
			return err
		}
	}

	return nil
}

func loadMeta(path string) (*Meta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read meta file %s: %w", path, err)
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse meta json %s: %w", path, err)
	}
	return &meta, nil
}

func findCategory(meta *Meta, name string) (*Category, error) {
	for i := range meta.Category {
		if meta.Category[i].CatName == name {
			return &meta.Category[i], nil
		}
	}
	return nil, fmt.Errorf("category %s not found in meta", name)
}

func ensureCleanDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.MkdirAll(path, 0o755)
		}
		return fmt.Errorf("stat dir %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("ensure target dir: %s is not a directory", path)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("read target dir %s: %w", path, err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("target dir %s is not empty", path)
	}
	return nil
}

func parseEnsureDataSelection(input string) (rom bool, media bool, err error) {
	if strings.TrimSpace(input) == "" {
		return true, true, nil
	}

	rom = false
	media = false

	parts := strings.Split(input, ",")
	for _, part := range parts {
		trimmed := strings.TrimSpace(strings.ToLower(part))
		switch trimmed {
		case "rom":
			rom = true
		case "media":
			media = true
		case "":
			// ignore empty segments
		default:
			return false, false, fmt.Errorf("unknown data type %s", part)
		}
	}

	if !rom && !media {
		return true, true, nil
	}

	return rom, media, nil
}

// PostRun performs any necessary cleanup after execution.
func (c *EnsureCommand) PostRun(ctx context.Context) error {
	logutil.GetLogger(ctx).Info("ensure completed",
		zap.String("category", c.category),
		zap.String("dir", c.targetDir),
	)
	return nil
}

func init() {
	RegisterRunner("ensure", func() IRunner { return NewEnsureCommand() })
}
