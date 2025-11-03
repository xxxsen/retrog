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

	"retrog/internal/config"
	"retrog/internal/storage"

	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

// Ensurer handles downloading data back from S3 using the meta file.
type Ensurer struct {
	store storage.Client
	cfg   *config.Config
}

// EnsureOptions control what the Ensurer downloads.
type EnsureOptions struct {
	Category     string
	TargetDir    string
	IncludeROM   bool
	IncludeMedia bool
	Unzip        bool
}

// NewEnsurer creates a new ensure handler.
func NewEnsurer(store storage.Client, cfg *config.Config) *Ensurer {
	return &Ensurer{store: store, cfg: cfg}
}

// Ensure downloads all games for the given category into targetDir.
func (e *Ensurer) Ensure(ctx context.Context, metaPath string, opts EnsureOptions) error {
	if err := ensureCleanDir(opts.TargetDir); err != nil {
		return err
	}

	if !opts.IncludeROM && !opts.IncludeMedia {
		opts.IncludeROM = true
		opts.IncludeMedia = true
	}

	meta, err := loadMeta(metaPath)
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
			logutil.GetLogger(ctx).Info("skip game", zap.String("game", game.Name))
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
			if err := e.downloadROMFiles(ctx, game, gameDir, opts.Unzip); err != nil {
				return err
			}
		}

		if opts.IncludeMedia {
			mediaDir := filepath.Join(gameDir, "media")
			if err := e.downloadMedia(ctx, mediaDir, game.Media); err != nil {
				return err
			}
		}
	}

	return nil
}

func (e *Ensurer) downloadROMFiles(ctx context.Context, game Game, gameDir string, unzip bool) error {
	for idx, file := range game.Files {
		key := fmt.Sprintf("%s%s", file.Hash, file.Ext)
		destName := buildFileNameFromMeta(game, file, idx)
		destPath := filepath.Join(gameDir, destName)
		if err := e.store.DownloadToFile(ctx, e.cfg.S3.RomBucket, key, destPath); err != nil {
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

		if unzip && strings.EqualFold(file.Ext, ".zip") {
			if err := unzipSingleFile(destPath); err != nil {
				return err
			}
		}
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

func (e *Ensurer) downloadMedia(ctx context.Context, mediaDir string, media Media) error {
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
		bucket, key, err := parseS3Path(item.src)
		if err != nil {
			return err
		}
		dest := filepath.Join(mediaDir, item.name+filepath.Ext(key))
		if err := e.store.DownloadToFile(ctx, bucket, key, dest); err != nil {
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
