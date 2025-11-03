package ensure

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"

	"retrog/internal/app"
	"retrog/internal/cli/common"
	"retrog/internal/storage"
)

// NewCommand constructs the cobra command responsible for downloading assets.
func NewCommand() *cobra.Command {
	var metaPath string
	var category string
	var targetDir string
	var dataSelection string
	var unzip bool

	cmd := &cobra.Command{
		Use:   "ensure",
		Short: "Download ROMs and media for a category based on generated meta JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			if metaPath == "" || category == "" || targetDir == "" {
				return errors.New("ensure requires --meta, --cat, and --dir")
			}

			includeROM, includeMedia, err := parseDataSelection(dataSelection)
			if err != nil {
				return err
			}

			ctx := cmdContext(cmd)
			logutil.GetLogger(ctx).Info("starting ensure",
				zap.String("meta", metaPath),
				zap.String("category", category),
				zap.String("dir", targetDir),
				zap.Bool("rom", includeROM),
				zap.Bool("media", includeMedia),
				zap.Bool("unzip", unzip),
			)

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

			ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()

			opts := app.EnsureOptions{
				Category:     category,
				TargetDir:    targetDir,
				IncludeROM:   includeROM,
				IncludeMedia: includeMedia,
				Unzip:        unzip,
			}

			if err := ensurer.Ensure(ctx, metaPath, opts); err != nil {
				return err
			}

			logutil.GetLogger(ctx).Info("ensure completed",
				zap.String("category", category),
				zap.String("dir", targetDir),
			)

			return nil
		},
	}

	cmd.Flags().StringVar(&metaPath, "meta", "", "Path to meta JSON produced by upload")
	cmd.Flags().StringVar(&category, "cat", "", "Category name to download")
	cmd.Flags().StringVar(&targetDir, "dir", "", "Destination directory to download into (must be empty or missing)")
	cmd.Flags().StringVar(&dataSelection, "data", "", "Comma-separated data types to download (rom, media)")
	cmd.Flags().BoolVar(&unzip, "unzip", false, "Unzip ROM archives that contain a single file")

	return cmd
}

func cmdContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

func parseDataSelection(input string) (rom bool, media bool, err error) {
	if strings.TrimSpace(input) == "" {
		return true, true, nil
	}

	rom = false
	media = false

	parts := strings.Split(input, ",")
	for _, part := range parts {
		trimmed := strings.TrimSpace(strings.ToLower(part))
		switch trimmed {
		case "rom":
			rom = true
		case "media":
			media = true
		case "":
			// ignore empty segments
		default:
			return false, false, fmt.Errorf("unknown data type %s", part)
		}
	}

	if !rom && !media {
		return true, true, nil
	}

	return rom, media, nil
}
