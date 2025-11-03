package app

import (
	"context"
	"errors"

	"retrog/internal/config"
	"retrog/internal/storage"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

// CleanBucketCommand empties configured buckets.
type CleanBucketCommand struct {
	cfg   *config.Config
	force bool
}

// NewCleanBucketCommand builds the clean bucket command.
func NewCleanBucketCommand(cfg *config.Config) *CleanBucketCommand {
	return &CleanBucketCommand{cfg: cfg}
}

// SetConfig injects the shared configuration for later use.
func (c *CleanBucketCommand) SetConfig(cfg *config.Config) {
	c.cfg = cfg
}

// Init registers CLI flags that affect the command.
func (c *CleanBucketCommand) Init(fst *pflag.FlagSet) {
	fst.BoolVar(&c.force, "force", false, "Confirm the cleanup operation")
}

// PreRun performs validation and initialisation.
func (c *CleanBucketCommand) PreRun(ctx context.Context) error {
	if c.cfg == nil {
		return errors.New("clean-bucket command missing configuration")
	}
	if !c.force {
		return errors.New("refusing to clean buckets without --force confirmation")
	}

	if storage.DefaultClient() == nil {
		client, err := storage.NewS3Client(ctx, c.cfg.S3)
		if err != nil {
			return err
		}
		storage.SetDefaultClient(client)
	}

	logutil.GetLogger(ctx).Info("clean bucket begin",
		zap.String("rom_bucket", c.cfg.S3.RomBucket),
		zap.String("media_bucket", c.cfg.S3.MediaBucket),
	)
	return nil
}

// Run executes the cleanup.
func (c *CleanBucketCommand) Run(ctx context.Context) error {
	store := storage.DefaultClient()
	if store == nil {
		return errors.New("storage client not initialised")
	}

	if err := store.ClearBucket(ctx, c.cfg.S3.RomBucket); err != nil {
		return err
	}
	if err := store.ClearBucket(ctx, c.cfg.S3.MediaBucket); err != nil {
		return err
	}
	return nil
}

// PostRun performs any cleanup after execution.
func (c *CleanBucketCommand) PostRun(ctx context.Context) error {
	logutil.GetLogger(ctx).Info("clean bucket finished",
		zap.String("rom_bucket", c.cfg.S3.RomBucket),
		zap.String("media_bucket", c.cfg.S3.MediaBucket),
	)
	return nil
}
