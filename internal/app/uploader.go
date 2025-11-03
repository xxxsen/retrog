package app

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"retrog/internal/config"
	"retrog/internal/metadata"
	"retrog/internal/storage"
)

const metadataFileName = "metadata.pegasus.txt"

var mediaCandidates = map[string]string{
	"boxart":     "boxart",
	"boxfront":   "boxfront",
	"screenshot": "screenshot",
	"video":      "video",
	"logo":       "logo",
}

// Uploader orchestrates uploading ROMs and media.
type Uploader struct {
	store storage.Client
	cfg   *config.Config
}

// NewUploader builds a new uploader instance.
func NewUploader(store storage.Client, cfg *config.Config) *Uploader {
	return &Uploader{store: store, cfg: cfg}
}

// Upload traverses the given rom root directory and uploads all categories.
func (u *Uploader) Upload(ctx context.Context, romRoot string) (*Meta, error) {
	rootEntries, err := os.ReadDir(romRoot)
	if err != nil {
		return nil, fmt.Errorf("read rom root %s: %w", romRoot, err)
	}

	result := &Meta{}

	for _, entry := range rootEntries {
		if !entry.IsDir() {
			continue
		}
		categoryPath := filepath.Join(romRoot, entry.Name())
		cat, err := u.processCategory(ctx, categoryPath)
		if err != nil {
			return nil, err
		}
		if cat != nil {
			result.Category = append(result.Category, *cat)
		}
	}

	return result, nil
}

func (u *Uploader) processCategory(ctx context.Context, categoryPath string) (*Category, error) {
	metaPath := filepath.Join(categoryPath, metadataFileName)
	doc, err := metadata.Parse(metaPath)
	if err != nil {
		return nil, fmt.Errorf("parse metadata %s: %w", metaPath, err)
	}

	if len(doc.Games) == 0 {
		return nil, nil
	}

	cat := &Category{
		CatName: doc.Collection,
	}
	if cat.CatName == "" {
		cat.CatName = filepath.Base(categoryPath)
	}

	for _, gameDef := range doc.Games {
		game, err := u.processGame(ctx, categoryPath, gameDef)
		if err != nil {
			return nil, err
		}
		cat.GameList = append(cat.GameList, *game)
	}

	return cat, nil
}

func (u *Uploader) processGame(ctx context.Context, categoryPath string, gameDef metadata.Game) (*Game, error) {
	cleanedName := cleanGameName(gameDef.Name)
	cleanedDesc := cleanDescription(gameDef.Description)

	game := &Game{
		DisplayName: cleanedName,
		Desc:        cleanedDesc,
	}

	primaryMediaDir := u.findMediaDir(categoryPath, gameDef)

	var baseHash string

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
		if baseHash == "" {
			baseHash = md5sum
			game.Hash = baseHash
		}
		originalName := filepath.Base(rel)
		contentType := mime.TypeByExtension(ext)
		if err := u.store.UploadFile(ctx, u.cfg.S3.RomBucket, key, full, contentType); err != nil {
			return nil, err
		}

		game.Files = append(game.Files, File{
			Hash:        md5sum,
			Ext:         ext,
			Size:        info.Size(),
			DisplayName: cleanedName,
			FileName:    originalName,
		})
	}

	media, err := u.uploadMedia(ctx, primaryMediaDir)
	if err != nil {
		return nil, err
	}

	game.Media = media
	if game.Hash == "" {
		game.Hash = cleanedName
	}
	return game, nil
}

func (u *Uploader) findMediaDir(categoryPath string, gameDef metadata.Game) string {
	if len(gameDef.Files) == 0 {
		return ""
	}
	first := gameDef.Files[0]
	base := strings.TrimSuffix(first, filepath.Ext(first))
	return filepath.Join(categoryPath, "media", base)
}

func (u *Uploader) uploadMedia(ctx context.Context, dir string) (Media, error) {
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
		if err := u.store.UploadFile(ctx, u.cfg.S3.MediaBucket, key, path, contentType); err != nil {
			return res, err
		}
		setMediaPath(&res, mediaType, s3Path(u.cfg.S3.MediaBucket, key))
	}
	return res, nil
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

func setMediaPath(media *Media, mediaType string, path string) {
	switch mediaType {
	case "boxfront":
		media.BoxFront = path
	case "boxart":
		media.Boxart = path
	case "screenshot":
		media.Screenshot = path
	case "video":
		media.Video = path
	case "logo":
		media.Logo = path
	}
}
