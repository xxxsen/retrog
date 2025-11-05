package app

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type ExportCommand struct {
	output string
}

func (c *ExportCommand) Name() string { return "export" }

func (c *ExportCommand) Desc() string {
	return "导出 meta.db 与 S3 媒体为 tar.gz 包"
}

func NewExportCommand() *ExportCommand { return &ExportCommand{} }

func (c *ExportCommand) Init(f *pflag.FlagSet) {
	f.StringVar(&c.output, "out", "", "导出的 tar.gz 文件路径")
}

func (c *ExportCommand) PreRun(ctx context.Context) error {
	if strings.TrimSpace(c.output) == "" {
		return errors.New("export requires --out")
	}
	logutil.GetLogger(ctx).Info("starting export",
		zap.String("out", c.output),
	)
	return nil
}

func (c *ExportCommand) Run(ctx context.Context) error {
	logger := logutil.GetLogger(ctx)
	store := storage.DefaultClient()
	dao := appdb.MetaDao

	tmpDir, err := os.MkdirTemp("", "retrog-export-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	mediaRoot := filepath.Join(tmpDir, "media")
	if err := os.MkdirAll(mediaRoot, 0o755); err != nil {
		return fmt.Errorf("create media dir: %w", err)
	}

	records, err := c.collectRecords(ctx, store, dao, tmpDir)
	if err != nil {
		return err
	}

	metadataPath := filepath.Join(tmpDir, "metadata.json")
	if err := c.writeMetadata(metadataPath, records); err != nil {
		return err
	}

	if err := c.archiveDirectory(tmpDir, c.output); err != nil {
		return err
	}

	logger.Info("export completed",
		zap.Int("records", len(records)),
		zap.String("output", c.output),
	)
	return nil
}

func (c *ExportCommand) PostRun(ctx context.Context) error { return nil }

type exportRecord struct {
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

func (c *ExportCommand) collectRecords(ctx context.Context, store storage.Client, dao *appdb.MetaDAO, baseDir string) ([]exportRecord, error) {
	const pageSize = 200
	lastID := int64(0)
	records := make([]exportRecord, 0)

	for {
		batch, err := dao.FetchPage(ctx, lastID, pageSize)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}

		for _, item := range batch {
			entry := item.Entry
			rec := exportRecord{
				Hash:      item.Hash,
				Name:      entry.Name,
				Desc:      entry.Desc,
				Size:      entry.Size,
				Developer: entry.Developer,
				Publisher: entry.Publisher,
				Genres:    strings.Join(entry.Genres, ","),
				ReleaseAt: entry.ReleaseAt,
				Media:     entry.Media,
			}
			records = append(records, rec)

			for _, media := range entry.Media {
				rel := mediaRelativePath(media.Hash, media.Ext)
				target := filepath.Join(baseDir, rel)
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					return nil, fmt.Errorf("create media dir %s: %w", filepath.Dir(target), err)
				}
				key := media.Hash + media.Ext
				if err := store.DownloadToFile(ctx, key, target); err != nil {
					return nil, fmt.Errorf("download media %s: %w", key, err)
				}
			}
		}

		lastID = batch[len(batch)-1].ID
	}

	return records, nil
}

func (c *ExportCommand) writeMetadata(path string, records []exportRecord) error {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

func (c *ExportCommand) archiveDirectory(srcDir, outPath string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create archive %s: %w", outPath, err)
	}
	defer f.Close()

	gzw := gzip.NewWriter(f)
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close()

	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == srcDir {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		info, err := d.Info()
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = rel
		if d.IsDir() && !strings.HasSuffix(header.Name, "/") {
			header.Name += "/"
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tw, file); err != nil {
			file.Close()
			return err
		}
		return file.Close()
	})
}

func init() {
	RegisterRunner("export", func() IRunner { return NewExportCommand() })
}
