package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"retrog/internal/config"
	"retrog/internal/storage"
)

// Ensurer handles downloading data back from S3 using the meta file.
type Ensurer struct {
	store storage.Client
}

// NewEnsurer creates a new ensure handler.
func NewEnsurer(store storage.Client, _ *config.Config) *Ensurer {
	return &Ensurer{store: store}
}

// Ensure downloads all games for the given category into targetDir.
func (e *Ensurer) Ensure(ctx context.Context, metaPath, categoryName, targetDir string) error {
	if err := ensureCleanDir(targetDir); err != nil {
		return err
	}

	meta, err := loadMeta(metaPath)
	if err != nil {
		return err
	}

	cat, err := findCategory(meta, categoryName)
	if err != nil {
		return err
	}

	for _, game := range cat.GameList {
		gameDir := filepath.Join(targetDir, sanitizeName(game.Name))
		if err := os.MkdirAll(gameDir, 0o755); err != nil {
			return fmt.Errorf("create game dir %s: %w", gameDir, err)
		}

		for _, file := range game.Files {
			if file.Path == "" {
				continue
			}
			bucket, key, err := parseS3Path(file.Path)
			if err != nil {
				return err
			}
			dest := filepath.Join(gameDir, filepath.Base(key))
			if err := e.store.DownloadToFile(ctx, bucket, key, dest); err != nil {
				return err
			}
			if file.Hash != "" {
				sum, err := fileMD5(dest)
				if err != nil {
					return err
				}
				if sum != file.Hash {
					return fmt.Errorf("hash mismatch for %s (expected %s got %s)", dest, file.Hash, sum)
				}
			}
		}

		mediaDir := filepath.Join(gameDir, "media")
		if err := e.downloadMedia(ctx, mediaDir, game.Media); err != nil {
			return err
		}
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
	for _, cat := range meta.Category {
		if cat.CatName == name {
			return &cat, nil
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
