package app

import (
	"context"

	"retrog/internal/config"
	"retrog/internal/storage"
)

// EnsureCommand encapsulates ensure execution state.
type EnsureCommand struct {
	cfg      *config.Config
	metaPath string
	opts     EnsureOptions
}

// NewEnsureCommand creates a new ensure command instance.
func NewEnsureCommand(cfg *config.Config, metaPath string, opts EnsureOptions) *EnsureCommand {
	return &EnsureCommand{cfg: cfg, metaPath: metaPath, opts: opts}
}

// Run executes the ensure logic.
func (c *EnsureCommand) Run(ctx context.Context) error {
	store, err := storage.NewS3Client(ctx, c.cfg.S3)
	if err != nil {
		return err
	}

	ensurer := NewEnsurer(store, c.cfg)
	return ensurer.Ensure(ctx, c.metaPath, c.opts)
}
