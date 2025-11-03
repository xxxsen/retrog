package app

import (
	"context"
	"errors"

	"retrog/internal/storage"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

// CleanBucketCommand empties configured buckets.
type CleanBucketCommand struct {
	force       bool
	romBucket   string
	mediaBucket string
}

// NewCleanBucketCommand builds the clean bucket command.
func NewCleanBucketCommand() *CleanBucketCommand {
	return &CleanBucketCommand{}
}

// Name returns the command identifier.
func (c *CleanBucketCommand) Name() string { return "clean-bucket" }

// Init registers CLI flags that affect the command.
func (c *CleanBucketCommand) Init(fst *pflag.FlagSet) {
	fst.BoolVar(&c.force, "force", false, "Confirm the cleanup operation")
}

// PreRun performs validation and initialisation.
func (c *CleanBucketCommand) PreRun(ctx context.Context) error {
	if !c.force {
		return errors.New("refusing to clean buckets without --force confirmation")
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

	if _, err := storage.EnsureDefaultClient(ctx); err != nil {
		return err
	}

	logutil.GetLogger(ctx).Info("clean bucket begin",
		zap.String("rom_bucket", c.romBucket),
		zap.String("media_bucket", c.mediaBucket),
	)
	return nil
}

// Run executes the cleanup.
func (c *CleanBucketCommand) Run(ctx context.Context) error {
	store := storage.DefaultClient()
	if store == nil {
		return errors.New("storage client not initialised")
	}

	if err := store.ClearBucket(ctx, c.romBucket); err != nil {
		return err
	}
	if err := store.ClearBucket(ctx, c.mediaBucket); err != nil {
		return err
	}
	return nil
}

// PostRun performs any cleanup after execution.
func (c *CleanBucketCommand) PostRun(ctx context.Context) error {
	logutil.GetLogger(ctx).Info("clean bucket finished",
		zap.String("rom_bucket", c.romBucket),
		zap.String("media_bucket", c.mediaBucket),
	)
	return nil
}

func init() {
	RegisterRunner("clean-bucket", func() IRunner { return NewCleanBucketCommand() })
}
