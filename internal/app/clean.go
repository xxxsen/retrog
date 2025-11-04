package app

import (
	"context"
	"errors"

	appdb "github.com/xxxsen/retrog/internal/db"
	"github.com/xxxsen/retrog/internal/storage"

	"github.com/spf13/pflag"
	"github.com/xxxsen/common/logutil"
)

// CleanCommand removes uploaded objects and clears local metadata.
type CleanCommand struct {
	force bool
}

// NewCleanCommand builds the clean command.
func NewCleanCommand() *CleanCommand {
	return &CleanCommand{}
}

// Name returns the command identifier.
func (c *CleanCommand) Name() string { return "clean" }

// Desc returns a short description.
func (c *CleanCommand) Desc() string {
	return "清空 S3 媒体并清理 meta.db 中的元数据"
}

// Init registers CLI flags that affect the command.
func (c *CleanCommand) Init(fst *pflag.FlagSet) {
	fst.BoolVar(&c.force, "force", false, "确认清理操作")
}

// PreRun performs validation and initialisation.
func (c *CleanCommand) PreRun(ctx context.Context) error {
	if !c.force {
		return errors.New("refusing to clean without --force confirmation")
	}

	logutil.GetLogger(ctx).Info("clean begin")
	return nil
}

// Run executes the cleanup.
func (c *CleanCommand) Run(ctx context.Context) error {
	store := storage.DefaultClient()
	if err := store.ClearBucket(ctx); err != nil {
		return err
	}

	dao := appdb.NewMetaDAO()
	if err := dao.ClearAll(ctx); err != nil {
		return err
	}
	return nil
}

// PostRun performs any cleanup after execution.
func (c *CleanCommand) PostRun(ctx context.Context) error {
	logutil.GetLogger(ctx).Info("clean finished")
	return nil
}

func init() {
	RegisterRunner("clean", func() IRunner { return NewCleanCommand() })
}
