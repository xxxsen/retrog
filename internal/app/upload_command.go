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

	"retrog/internal/metadata"
	"retrog/internal/storage"

	"github.com/spf13/pflag"
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
	romDir      string
	metaPath    string
	romBucket   string
	mediaBucket string
}

// Name returns the command identifier.
func (c *UploadCommand) Name() string { return "upload" }

// NewUploadCommand constructs an executable upload command.
func NewUploadCommand() *UploadCommand {
	return &UploadCommand{}
}

// Init registers CLI flags that affect the command.
func (c *UploadCommand) Init(fst *pflag.FlagSet) {
	fst.StringVar(&c.romDir, "dir", "", "ROM root directory")
	fst.StringVar(&c.metaPath, "meta", "", "Path to write generated meta JSON")
}

// PreRun performs validation and object initialisation as needed.
func (c *UploadCommand) PreRun(ctx context.Context) error {
	if c.romDir == "" || c.metaPath == "" {
		return errors.New("upload requires --dir and --meta")
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

	logutil.GetLogger(ctx).Info("starting upload",
		zap.String("dir", c.romDir),
		zap.String("meta", c.metaPath),
	)
	return nil
}

// Run executes the upload command logic.
func (c *UploadCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)

	store := storage.DefaultClient()
	if store == nil {
		return errors.New("storage client not initialised")
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
		key := fmt.Sprintf("rom/%s%s", md5sum, ext)
		originalName := filepath.Base(rel)
		contentType := mime.TypeByExtension(ext)
		if err := store.UploadFile(ctx, c.romBucket, key, full, contentType); err != nil {
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
		key := fmt.Sprintf("media/%s%s", md5sum, ext)
		contentType := mime.TypeByExtension(ext)
		if err := store.UploadFile(ctx, c.mediaBucket, key, path, contentType); err != nil {
			return res, err
		}
		assignMediaPath(&res, mediaType, key)
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

// PostRun performs any necessary cleanup after execution.
func (c *UploadCommand) PostRun(ctx context.Context) error {
	return nil
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

func init() {
	RegisterRunner("upload", func() IRunner { return NewUploadCommand() })
}
