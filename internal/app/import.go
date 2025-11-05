package app

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	appdb "github.com/xxxsen/retrog/internal/db"
	"github.com/xxxsen/retrog/internal/model"
	"github.com/xxxsen/retrog/internal/storage"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

type ImportCommand struct {
	input string
}

func (c *ImportCommand) Name() string { return "import" }

func (c *ImportCommand) Desc() string {
	return "从导出的压缩包重建 S3 媒体与 meta.db 元数据"
}

func NewImportCommand() *ImportCommand { return &ImportCommand{} }

func (c *ImportCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.input, "file", "", "导入的 tar.gz 文件路径")
}

func (c *ImportCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.input) == "" {
		return errors.New("import requires --file")
	}
	logutil.GetLogger(ctx).Info("starting import",
		zap.String("file", c.input),
	)
	return nil
}

func (c *ImportCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)
	store := storage.DefaultClient()
	dao := appdb.MetaDao

	tmpDir, err := os.MkdirTemp("", "retrog-import-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := c.extractTarGz(c.input, tmpDir); err != nil {
		return err
	}

	records, err := c.loadMetadata(filepath.Join(tmpDir, "metadata.json"))
	if err != nil {
		return err
	}

	if err := c.restoreMedia(ctx, store, tmpDir, records); err != nil {
		return err
	}

	entries := make(map[string]model.Entry, len(records))
	for _, rec := range records {
		entry := model.Entry{
			Name:      rec.Name,
			Desc:      rec.Desc,
			Size:      rec.Size,
			Developer: rec.Developer,
			Publisher: rec.Publisher,
			Genres:    c.splitGenres(rec.Genres),
			ReleaseAt: rec.ReleaseAt,
			Media:     rec.Media,
		}
		entries[rec.Hash] = entry
	}

	inserted, updated, err := dao.Upsert(ctx, entries)
	if err != nil {
		return err
	}

	logger.Info("import completed",
		zap.Int("entries", len(records)),
		zap.Int("inserted", inserted),
		zap.Int("updated", updated),
	)
	return nil
}

func (c *ImportCommand) PostRun(ctx context.Context) error { return nil }

type importRecord struct {
	Hash      string             `json:"hash"`
	Name      string             `json:"name"`
	Desc      string             `json:"desc"`
	Size      int64              `json:"size"`
	Developer string             `json:"developer,omitempty"`
	Publisher string             `json:"publisher,omitempty"`
	Genres    string             `json:"genres,omitempty"`
	ReleaseAt int64              `json:"release_at,omitempty"`
	Media     []model.MediaEntry `json:"media,omitempty"`
}

func (c *ImportCommand) loadMetadata(path string) ([]importRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read metadata.json: %w", err)
	}
	var records []importRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parse metadata.json: %w", err)
	}
	return records, nil
}

func (c *ImportCommand) restoreMedia(ctx context.Context, store storage.Client, baseDir string, records []importRecord) error {
	for _, rec := range records {
		for _, media := range rec.Media {
			rel := mediaRelativePath(media.Hash, media.Ext)
			src := filepath.Join(baseDir, rel)
			if _, err := os.Stat(src); err != nil {
				return fmt.Errorf("media file missing: %s", rel)
			}
			contentType := mime.TypeByExtension(media.Ext)
			if err := store.UploadFile(ctx, media.Hash+media.Ext, src, contentType); err != nil {
				return fmt.Errorf("upload media %s: %w", media.Hash+media.Ext, err)
			}
		}
	}
	return nil
}

func (c *ImportCommand) extractTarGz(archivePath, dstDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive %s: %w", archivePath, err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("create gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		name := filepath.Clean(header.Name)
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("invalid path in archive: %s", header.Name)
		}
		target := filepath.Join(dstDir, name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		default:
			continue
		}
	}
	return nil
}

func (c *ImportCommand) splitGenres(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func init() {
	RegisterRunner("import", func() IRunner { return NewImportCommand() })
}
