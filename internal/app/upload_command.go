package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"retrog/internal/config"
	"retrog/internal/storage"

	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

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

	store, err := storage.NewS3Client(ctx, c.cfg.S3)
	if err != nil {
		return err
	}

	uploader := NewUploader(store, c.cfg)
	meta, err := uploader.Upload(ctx, c.romDir)
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
