package app

import (
	"context"

	"retrog/internal/config"
	"retrog/internal/storage"
)

// CleanBucketCommand empties configured buckets.
type CleanBucketCommand struct {
	cfg *config.Config
}

// NewCleanBucketCommand builds the clean bucket command.
func NewCleanBucketCommand(cfg *config.Config) *CleanBucketCommand {
	return &CleanBucketCommand{cfg: cfg}
}

// Run executes the cleanup.
func (c *CleanBucketCommand) Run(ctx context.Context) error {
	store := storage.DefaultClient()
	if store == nil {
		var err error
		store, err = storage.NewS3Client(ctx, c.cfg.S3)
		if err != nil {
			return err
		}
		storage.SetDefaultClient(store)
	}

	if err := store.ClearBucket(ctx, c.cfg.S3.RomBucket); err != nil {
		return err
	}
	if err := store.ClearBucket(ctx, c.cfg.S3.MediaBucket); err != nil {
		return err
	}
	return nil
}
