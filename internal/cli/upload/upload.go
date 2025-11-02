package upload

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"retrog/internal/app"
	"retrog/internal/cli/common"
	"retrog/internal/storage"
)

// NewCommand constructs the cobra command for uploading ROMs and media.
func NewCommand() *cobra.Command {
	var romDir string
	var metaPath string

	cmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload ROMs and media to object storage and emit metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			if romDir == "" || metaPath == "" {
				return errors.New("upload requires --dir and --meta")
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

			uploader := app.NewUploader(store, cfg)

			ctx := cmdContext(cmd)
			ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()

			meta, err := uploader.Upload(ctx, romDir)
			if err != nil {
				return err
			}

			data, err := json.MarshalIndent(meta, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal meta json: %w", err)
			}

			if err := os.WriteFile(metaPath, data, 0o644); err != nil {
				return fmt.Errorf("write meta file: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&romDir, "dir", "", "ROM root directory")
	cmd.Flags().StringVar(&metaPath, "meta", "", "Path to write generated meta JSON")

	return cmd
}

func cmdContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}
