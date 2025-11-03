package cli

import (
	"context"
	"errors"
	"time"

	"github.com/spf13/cobra"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"

	"retrog/internal/app"
)

func newCleanBucketCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "clean-bucket",
		Short: "Remove all objects from the configured ROM and media buckets",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				return errors.New("refusing to clean buckets without --force confirmation")
			}

			cfg, err := getConfig(cmd)
			if err != nil {
				return err
			}

			ctx := commandContext(cmd)
			logutil.GetLogger(ctx).Info("clean bucket begin",
				zap.String("rom_bucket", cfg.S3.RomBucket),
				zap.String("media_bucket", cfg.S3.MediaBucket),
			)

			var runner app.IRunner = app.NewCleanBucketCommand(cfg)

			ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()

			if err := runner.Run(ctx); err != nil {
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
