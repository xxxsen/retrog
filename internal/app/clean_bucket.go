package app

import (
	"context"
	"errors"

	"github.com/xxxsen/retrog/internal/storage"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
)

// CleanBucketCommand empties configured buckets.
type CleanBucketCommand struct {
	force bool
}

// NewCleanBucketCommand builds the clean bucket command.
func NewCleanBucketCommand() *CleanBucketCommand {
	return &CleanBucketCommand{}
}

// Name returns the command identifier.
func (c *CleanBucketCommand) Name() string { return "clean-bucket" }

func (c *CleanBucketCommand) Desc() string {
	return "Remove all objects from the configured ROM and media buckets"
}

// Init registers CLI flags that affect the command.
func (c *CleanBucketCommand) Init(fst *pflag.FlagSet) {
	fst.BoolVar(&c.force, "force", false, "Confirm the cleanup operation")
}

// PreRun performs validation and initialisation.
func (c *CleanBucketCommand) PreRun(ctx context.Context) error {
	if !c.force {
		return errors.New("refusing to clean buckets without --force confirmation")
	}

	logutil.GetLogger(ctx).Info("clean bucket begin")
	return nil
}

// Run executes the cleanup.
func (c *CleanBucketCommand) Run(ctx context.Context) error {
	store := storage.DefaultClient()
	if err := store.ClearBucket(ctx); err != nil {
		return err
	}
	return nil
}

// PostRun performs any cleanup after execution.
func (c *CleanBucketCommand) PostRun(ctx context.Context) error {
	logutil.GetLogger(ctx).Info("clean bucket finished")
	return nil
}

func init() {
	RegisterRunner("clean-bucket", func() IRunner { return NewCleanBucketCommand() })
}
