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

var mediaAssetKeys = map[string][]string{
	"boxart":     {"boxart", "box_front", "boxfront"},
	"boxfront":   {"box_front", "boxfront", "boxart"},
	"screenshot": {"screenshot"},
	"video":      {"video"},
	"logo":       {"logo"},
}

// UploadCommand wraps the upload workflow and exposes a Run entrypoint.
type UploadCommand struct {
	romDir   string
	metaPath string
	cats     []string
}

// Name returns the command identifier.
func (c *UploadCommand) Name() string { return "upload" }

func (c *UploadCommand) Desc() string {
	return "Upload ROMs and media to object storage and emit metadata"
}

// NewUploadCommand constructs an executable upload command.
func NewUploadCommand() *UploadCommand {
	return &UploadCommand{}
}

// Init registers CLI flags that affect the command.
func (c *UploadCommand) Init(fst *pflag.FlagSet) {
	fst.StringVar(&c.romDir, "dir", "", "ROM root directory")
	fst.StringVar(&c.metaPath, "meta", "", "Path to write generated meta JSON")
	fst.StringSliceVar(&c.cats, "cat", nil, "Comma separated list of categories to upload; empty means all")
}

// PreRun performs validation and object initialisation as needed.
func (c *UploadCommand) PreRun(ctx context.Context) error {
	if c.romDir == "" || c.metaPath == "" {
		return errors.New("upload requires --dir and --meta")
	}
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

	allowed := make(map[string]struct{})
	if len(c.cats) > 0 {
		for _, name := range c.cats {
			trimmed := strings.TrimSpace(name)
			if trimmed != "" {
				allowed[trimmed] = struct{}{}
			}
		}
	}

	for _, entry := range rootEntries {
		if !entry.IsDir() {
			continue
		}
		categoryPath := filepath.Join(c.romDir, entry.Name())
		if len(allowed) > 0 {
			if _, ok := allowed[entry.Name()]; !ok {
				continue
			}
		}
		cat, err := c.processCategory(ctx, store, categoryPath, entry.Name())
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

func (c *UploadCommand) processCategory(ctx context.Context, store storage.Client, categoryPath string, catName string) (*Category, error) {
	metaPath := filepath.Join(categoryPath, metadataFileName)
	logger := logutil.GetLogger(ctx)
	logger.Debug("processing category metadata", zap.String("meta", metaPath))

	doc, err := metadata.Parse(metaPath)
	if err != nil {
		return nil, fmt.Errorf("parse metadata %s: %w", metaPath, err)
	}
	doc.Cat = catName

	if len(doc.Games) == 0 {
		return nil, nil
	}

	cat := &Category{CatName: catName, Collection: doc.Collection}

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
		if err := store.UploadFile(ctx, key, full, contentType); err != nil {
			return nil, err
		}

		game.Files = append(game.Files, File{
			Hash:     md5sum,
			Ext:      ext,
			Size:     info.Size(),
			FileName: originalName,
		})
	}

	media, err := c.uploadMedia(ctx, store, categoryPath, primaryMediaDir, gameDef)
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

func (c *UploadCommand) uploadMedia(ctx context.Context, store storage.Client, categoryPath, defaultDir string, gameDef metadata.Game) (Media, error) {
	res := Media{}
	for mediaType, baseName := range mediaCandidates {
		path, err := c.pickMediaSource(categoryPath, defaultDir, gameDef, mediaType, baseName)
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
		if err := store.UploadFile(ctx, key, path, contentType); err != nil {
			return res, err
		}
		assignMediaPath(&res, mediaType, strings.TrimPrefix(key, "media/"))
	}
	return res, nil
}

func (c *UploadCommand) pickMediaSource(categoryPath, defaultDir string, gameDef metadata.Game, mediaType, baseName string) (string, error) {
	if candidate := c.assetPathFromMetadata(categoryPath, gameDef, mediaType); candidate != "" {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		logutil.GetLogger(context.Background()).Warn("asset path missing, fallback to prefix",
			zap.String("media_type", mediaType),
			zap.String("path", candidate),
		)
	}
	if defaultDir == "" {
		return "", nil
	}
	return c.firstFileWithPrefix(defaultDir, baseName)
}

func (c *UploadCommand) assetPathFromMetadata(categoryPath string, gameDef metadata.Game, mediaType string) string {
	if len(gameDef.Assets) == 0 {
		return ""
	}
	aliases := mediaAssetKeys[mediaType]
	for _, alias := range aliases {
		if val, ok := gameDef.Assets[alias]; ok {
			trimmed := strings.TrimSpace(val)
			if trimmed == "" {
				continue
			}
			candidate := filepath.FromSlash(trimmed)
			if filepath.IsAbs(candidate) {
				return candidate
			}
			return filepath.Join(categoryPath, candidate)
		}
	}
	return ""
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

func (c *UploadCommand) firstFileWithPrefix(dir, prefix string) (string, error) {
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
