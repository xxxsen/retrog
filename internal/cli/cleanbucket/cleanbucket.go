package cleanbucket

import (
	"context"
	"errors"
	"time"

	"github.com/spf13/cobra"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"

	"retrog/internal/cli/common"
	"retrog/internal/storage"
)

// NewCommand returns a cobra command that clears configured buckets.
func NewCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "clean-bucket",
		Short: "Remove all objects from the configured ROM and media buckets",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				return errors.New("refusing to clean buckets without --force confirmation")
			}

			cfgPath, _ := cmd.Root().PersistentFlags().GetString(common.ConfigFlag)
			cfg, err := common.LoadConfig(cfgPath)
			if err != nil {
				return err
			}

			store, err := storage.NewS3Client(cmd.Context(), cfg.S3)
			if err != nil {
				return err
			}

			ctx := cmdContext(cmd)
			ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()

			logutil.GetLogger(ctx).Info("clean bucket begin",
				zap.String("rom_bucket", cfg.S3.RomBucket),
				zap.String("media_bucket", cfg.S3.MediaBucket),
			)

			if err := store.ClearBucket(ctx, cfg.S3.RomBucket); err != nil {
				return err
			}
			if err := store.ClearBucket(ctx, cfg.S3.MediaBucket); err != nil {
				return err
			}

			logutil.GetLogger(ctx).Info("clean bucket finished",
				zap.String("rom_bucket", cfg.S3.RomBucket),
				zap.String("media_bucket", cfg.S3.MediaBucket),
			)

			cmd.Println("Buckets cleaned successfully")
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Confirm the cleanup operation")

	return cmd
}

func cmdContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}
