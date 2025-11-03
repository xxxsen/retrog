package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"retrog/internal/config"
	"retrog/internal/metadata"
	"retrog/internal/storage"

	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

const metadataFileName = "metadata.pegasus.txt"

var mediaCandidates = map[string]string{
	"boxart":     "boxart",
	"boxfront":   "boxfront",
	"screenshot": "screenshot",
	"video":      "video",
	"logo":       "logo",
}

// UploadCommand wraps the upload workflow and exposes a Run entrypoint.
type UploadCommand struct {
	cfg      *config.Config
	romDir   string
	metaPath string
}

// NewUploadCommand constructs an executable upload command.
func NewUploadCommand(cfg *config.Config, romDir, metaPath string) *UploadCommand {
	return &UploadCommand{cfg: cfg, romDir: romDir, metaPath: metaPath}
}

// Run executes the upload command logic.
func (c *UploadCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)

	store := storage.DefaultClient()
	if store == nil {
		var err error
		store, err = storage.NewS3Client(ctx, c.cfg.S3)
		if err != nil {
			return err
		}
		storage.SetDefaultClient(store)
	}

	meta, err := c.buildMeta(ctx, store)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta json: %w", err)
	}

	if err := os.WriteFile(c.metaPath, data, 0o644); err != nil {
		return fmt.Errorf("write meta file: %w", err)
	}

	logger.Info("upload run completed",
		zap.String("meta_path", c.metaPath),
		zap.Int("categories", len(meta.Category)),
	)

	return nil
}

func (c *UploadCommand) buildMeta(ctx context.Context, store storage.Client) (*Meta, error) {
	logger := logutil.GetLogger(ctx)
	logger.Info("scanning rom root", zap.String("root", c.romDir))

	rootEntries, err := os.ReadDir(c.romDir)
	if err != nil {
		return nil, fmt.Errorf("read rom root %s: %w", c.romDir, err)
	}

	result := &Meta{}

	for _, entry := range rootEntries {
		if !entry.IsDir() {
			continue
		}
		categoryPath := filepath.Join(c.romDir, entry.Name())
		cat, err := c.processCategory(ctx, store, categoryPath)
		if err != nil {
			return nil, err
		}
		if cat != nil {
			result.Category = append(result.Category, *cat)
			logger.Debug("category processed",
				zap.String("path", categoryPath),
				zap.String("name", cat.CatName),
				zap.Int("games", len(cat.GameList)),
			)
		}
	}

	return result, nil
}

func (c *UploadCommand) processCategory(ctx context.Context, store storage.Client, categoryPath string) (*Category, error) {
	metaPath := filepath.Join(categoryPath, metadataFileName)
	logger := logutil.GetLogger(ctx)
	logger.Debug("processing category metadata", zap.String("meta", metaPath))

	doc, err := metadata.Parse(metaPath)
	if err != nil {
		return nil, fmt.Errorf("parse metadata %s: %w", metaPath, err)
	}

	if len(doc.Games) == 0 {
		return nil, nil
	}

	cat := &Category{CatName: doc.Collection}
	if cat.CatName == "" {
		cat.CatName = filepath.Base(categoryPath)
	}

	for _, gameDef := range doc.Games {
		game, err := c.processGame(ctx, store, categoryPath, gameDef)
		if err != nil {
			return nil, err
		}
		cat.GameList = append(cat.GameList, *game)
	}

	logger.Debug("category games compiled",
		zap.String("category", cat.CatName),
		zap.Int("games", len(cat.GameList)),
	)

	return cat, nil
}

func (c *UploadCommand) processGame(ctx context.Context, store storage.Client, categoryPath string, gameDef metadata.Game) (*Game, error) {
	cleanedName := cleanGameName(gameDef.Name)
	cleanedDesc := cleanDescription(gameDef.Description)

	game := &Game{
		Name: cleanedName,
		Desc: cleanedDesc,
	}

	primaryMediaDir := c.findMediaDir(categoryPath, gameDef)

	for _, rel := range gameDef.Files {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		full := filepath.Join(categoryPath, rel)
		info, err := os.Stat(full)
		if err != nil {
			return nil, fmt.Errorf("stat rom file %s: %w", full, err)
		}
		if info.IsDir() {
			continue
		}
		md5sum, err := fileMD5(full)
		if err != nil {
			return nil, err
		}
		ext := strings.ToLower(filepath.Ext(full))
		key := fmt.Sprintf("%s%s", md5sum, ext)
		originalName := filepath.Base(rel)
		contentType := mime.TypeByExtension(ext)
		if err := store.UploadFile(ctx, c.cfg.S3.RomBucket, key, full, contentType); err != nil {
			return nil, err
		}

		game.Files = append(game.Files, File{
			Hash:     md5sum,
			Ext:      ext,
			Size:     info.Size(),
			FileName: originalName,
		})
	}

	media, err := c.uploadMedia(ctx, store, primaryMediaDir)
	if err != nil {
		return nil, err
	}

	game.Media = media
	return game, nil
}

func (c *UploadCommand) findMediaDir(categoryPath string, gameDef metadata.Game) string {
	if len(gameDef.Files) == 0 {
		return ""
	}
	first := gameDef.Files[0]
	base := strings.TrimSuffix(first, filepath.Ext(first))
	return filepath.Join(categoryPath, "media", base)
}

func (c *UploadCommand) uploadMedia(ctx context.Context, store storage.Client, dir string) (Media, error) {
	res := Media{}
	if dir == "" {
		return res, nil
	}
	for mediaType, baseName := range mediaCandidates {
		path, err := firstFileWithPrefix(dir, baseName)
		if err != nil {
			return res, err
		}
		if path == "" {
			continue
		}
		md5sum, err := fileMD5(path)
		if err != nil {
			return res, err
		}
		ext := strings.ToLower(filepath.Ext(path))
		key := fmt.Sprintf("%s%s", md5sum, ext)
		contentType := mime.TypeByExtension(ext)
		if err := store.UploadFile(ctx, c.cfg.S3.MediaBucket, key, path, contentType); err != nil {
			return res, err
		}
		assignMediaPath(&res, mediaType, s3Path(c.cfg.S3.MediaBucket, key))
	}
	return res, nil
}

func assignMediaPath(media *Media, mediaType, path string) {
	switch mediaType {
	case "boxart":
		media.Boxart = path
	case "boxfront":
		media.BoxFront = path
	case "screenshot":
		media.Screenshot = path
	case "video":
		media.Video = path
	case "logo":
		media.Logo = path
	}
}

func firstFileWithPrefix(dir, prefix string) (string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read media dir %s: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			return filepath.Join(dir, name), nil
		}
	}
	return "", nil
}
