package ensure

import (
	"context"
	"errors"
	"time"

	"github.com/spf13/cobra"

	"retrog/internal/app"
	"retrog/internal/cli/common"
	"retrog/internal/storage"
)

// NewCommand constructs the cobra command responsible for downloading assets.
func NewCommand() *cobra.Command {
	var metaPath string
	var category string
	var targetDir string

	cmd := &cobra.Command{
		Use:   "ensure",
		Short: "Download ROMs and media for a category based on generated meta JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			if metaPath == "" || category == "" || targetDir == "" {
				return errors.New("ensure requires --meta, --cat, and --dir")
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

			ensurer := app.NewEnsurer(store, cfg)

			ctx := cmdContext(cmd)
			ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()

			if err := ensurer.Ensure(ctx, metaPath, category, targetDir); err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&metaPath, "meta", "", "Path to meta JSON produced by upload")
	cmd.Flags().StringVar(&category, "cat", "", "Category name to download")
	cmd.Flags().StringVar(&targetDir, "dir", "", "Destination directory to download into (must be empty or missing)")

	return cmd
}

func cmdContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}
