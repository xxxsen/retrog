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

func newUploadCommand() *cobra.Command {
	var romDir string
	var metaPath string

	cmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload ROMs and media to object storage and emit metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			if romDir == "" || metaPath == "" {
				return errors.New("upload requires --dir and --meta")
			}

			ctx := commandContext(cmd)
			logutil.GetLogger(ctx).Info("starting upload",
				zap.String("dir", romDir),
				zap.String("meta", metaPath),
			)

			cfg, err := getConfig(cmd)
			if err != nil {
				return err
			}

			var runner app.IRunner = app.NewUploadCommand(cfg, romDir, metaPath)

			ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()

			if err := runner.Run(ctx); err != nil {
				return err
			}

			logutil.GetLogger(ctx).Info("upload completed",
				zap.String("meta", metaPath),
			)

			return nil
		},
	}

	cmd.Flags().StringVar(&romDir, "dir", "", "ROM root directory")
	cmd.Flags().StringVar(&metaPath, "meta", "", "Path to write generated meta JSON")

	return cmd
}
